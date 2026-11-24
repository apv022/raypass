package app.raypass.core;

import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

/**
 * The lock state of one Raypass process over the two feeds (DESIGN.md "Lock state"): it keeps
 * every keyring and every vault entry as ciphertext, whether locked or not, and while unlocked
 * holds the vault key and the folded {@link Vault}. Locking wipes the key and the folded state.
 * Feed I/O is the caller's: methods return the bodies to append. Thread-safe.
 */
public final class Session {
    private final List<Keyring> keyrings = new ArrayList<>();
    private final List<VaultEntry> entries = new ArrayList<>();
    private final Clock clock;
    private byte[] vk;
    private Keyring current;
    private final Vault vault;
    private int corrupt;

    /** A locked session whose writes are stamped by clock. */
    public Session(Clock clock) {
        this.clock = clock;
        this.vault = new Vault(clock);
    }

    /** The clock. */
    public Clock clock() { return clock; }

    /** Adds a {@code keys} feed body (ignored if not a keyring). */
    public synchronized void addKeysBody(Object body) {
        Keyring k = Keyring.parse(body);
        if (k == null) return;
        keyrings.add(k);
        clock.observe(k.stamp);
        Keyring.Choice c = Keyring.choose(keyrings, Crypto.MIN_ITER);
        if (vk != null && c.current != null && !c.current.id.equals(current == null ? "" : current.id)) {
            // Another vault won (it was set up earlier elsewhere): our key cannot read it.
            lock();
        }
        current = c.current;
    }

    /** Adds a {@code vault} feed body; decrypted and folded at once when unlocked. */
    public synchronized void addVaultBody(Object body) {
        VaultEntry e = VaultEntry.parse(body);
        if (e == null) return;
        entries.add(e);
        if (vk != null) fold(e);
    }

    private void fold(VaultEntry e) {
        if (current == null || !e.k.equals(current.id)) return; // orphaned: another vault's entry
        try {
            vault.apply(e.decrypt(vk));
        } catch (GeneralSecurityException x) {
            corrupt++;
        }
    }

    /** Reports whether a vault exists (a valid keyring). */
    public synchronized boolean initialized() { return Keyring.choose(keyrings, Crypto.MIN_ITER).current != null; }

    /** Reports whether the vault key is held. */
    public synchronized boolean unlocked() { return vk != null; }

    /** The current keyring, or null. */
    public synchronized Keyring keyring() { return current; }

    /** The unlocked vault, or null while locked. */
    public synchronized Vault vault() { return vk == null ? null : vault; }

    /** Entries of other vaults (concurrent setups) that we cannot read. */
    public synchronized int orphaned() {
        if (current == null) return 0;
        int n = 0;
        for (VaultEntry e : entries) if (!e.k.equals(current.id)) n++;
        return n;
    }

    /** Entries of our vault that failed to decrypt since unlock. */
    public synchronized int corrupt() { return corrupt; }

    /** Number of ciphertext entries held. */
    public synchronized int entryCount() { return entries.size(); }

    /** Creates the vault; returns the keyring body to append to {@code keys}. Unlocked after. */
    public synchronized Map<String, Object> setup(String password, int iter) throws GeneralSecurityException {
        if (initialized()) throw new GeneralSecurityException("a vault already exists");
        byte[] key = Crypto.random(Crypto.KEY_SIZE);
        Keyring k = Keyring.create(password, key, iter, clock.now());
        keyrings.add(k);
        current = k;
        vk = key;
        corrupt = 0;
        vault.wipe();
        for (VaultEntry e : entries) fold(e);
        return k.toJson();
    }

    /** Derives, unwraps and folds; WrongPasswordException on a wrong password. Slow (PBKDF2). */
    public void unlock(String password) throws GeneralSecurityException {
        Keyring k;
        synchronized (this) {
            k = current;
            if (k == null) throw new GeneralSecurityException("no vault");
        }
        byte[] key = k.unwrap(password); // outside the lock: 600k rounds
        unlockWithKey(key, k);
    }

    /** Unlocks with a vault key from elsewhere (biometric unwrap); checked against the keyring. */
    public synchronized void unlockWithKey(byte[] key, Keyring expect) throws GeneralSecurityException {
        if (current == null || (expect != null && !current.id.equals(expect.id)) || !current.checks(key)) {
            Crypto.wipe(key);
            throw new Crypto.CorruptException();
        }
        if (vk != null) Crypto.wipe(vk);
        vk = key;
        corrupt = 0;
        vault.wipe();
        for (VaultEntry e : entries) fold(e);
    }

    /** A copy of the vault key (to wrap for biometrics; caller wipes), or null while locked. */
    public synchronized byte[] keyCopy() { return vk == null ? null : vk.clone(); }

    /** Wipes the key and every decrypted value. */
    public synchronized void lock() {
        Crypto.wipe(vk);
        vk = null;
        vault.wipe();
    }

    /** Re-wraps the vault key under a new password; returns the keyring body to append. */
    public Map<String, Object> changePassword(String oldPw, String newPw, int iter) throws GeneralSecurityException {
        Keyring k;
        synchronized (this) {
            k = current;
            if (k == null) throw new GeneralSecurityException("no vault");
        }
        byte[] key = k.unwrap(oldPw);
        try {
            Keyring n = k.rewrap(newPw, key, iter <= 0 ? k.iter : iter, clock.now());
            synchronized (this) {
                keyrings.add(n);
                current = Keyring.choose(keyrings, Crypto.MIN_ITER).current;
            }
            return n.toJson();
        } finally {
            Crypto.wipe(key);
        }
    }

    /** Encrypts ops (already applied locally) into vault feed bodies. */
    public synchronized List<Map<String, Object>> encrypt(List<MapOp> ops) throws GeneralSecurityException {
        if (vk == null) throw new GeneralSecurityException("locked");
        List<Map<String, Object>> out = new ArrayList<>();
        if (ops.isEmpty()) return out;
        for (VaultEntry e : VaultEntry.encryptOps(vk, current.id, ops)) out.add(e.toJson());
        return out;
    }
}
