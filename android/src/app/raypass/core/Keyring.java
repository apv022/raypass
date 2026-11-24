package app.raypass.core;

import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A keyring entry of the {@code keys} feed (DESIGN.md "Vault key and the keyring"): the vault
 * key wrapped under the master key. Only the KDF parameters, nonce, wrap, check and stamp are in
 * it; the wrap and check reveal nothing without the password.
 */
public final class Keyring {
    /** Keyring id: 16 lowercase hex chars, random per vault. */
    public final String id;
    /** PBKDF2 rounds. */
    public final int iter;
    /** Base64 fields as stored. */
    public final String salt, nonce, wrapped, check;
    /** Which keyring entry is current (the greatest). */
    public final Stamp stamp;

    /** Makes a keyring from its stored fields. */
    public Keyring(String id, int iter, String salt, String nonce, String wrapped, String check, Stamp stamp) {
        this.id = id;
        this.iter = iter;
        this.salt = salt;
        this.nonce = nonce;
        this.wrapped = wrapped;
        this.check = check;
        this.stamp = stamp;
    }

    /** The feed body, in the Go struct's field order. */
    public Map<String, Object> toJson() {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("v", 1L);
        m.put("type", "keyring");
        m.put("id", id);
        m.put("kdf", Json.map("alg", Crypto.KDF_ALG, "iter", (long) iter, "salt", salt));
        m.put("nonce", nonce);
        m.put("wrapped", wrapped);
        m.put("check", check);
        m.put("stamp", stamp.toJson());
        return m;
    }

    /** Parses a feed body; null if it is not a v1 keyring. */
    public static Keyring parse(Object body) {
        Map<String, Object> m = Json.obj(body);
        if (m == null || Json.num(m, "v", 0) != 1 || !"keyring".equals(m.get("type"))) return null;
        Map<String, Object> kdf = Json.obj(m.get("kdf"));
        String alg = Json.str(kdf, "alg", "");
        long iter = Json.num(kdf, "iter", 0);
        if (!Crypto.KDF_ALG.equals(alg)) iter = -1; // fails valid()
        return new Keyring(Json.str(m, "id", ""), (int) Math.max(-1, Math.min(iter, Integer.MAX_VALUE)),
                Json.str(kdf, "salt", ""), Json.str(m, "nonce", ""), Json.str(m, "wrapped", ""),
                Json.str(m, "check", ""), Stamp.fromJson(m.get("stamp")));
    }

    /** Reports whether every field is well formed and iter is in [minIter, MAX_ITER]. */
    public boolean valid(int minIter) {
        try {
            return validId(id) && iter >= minIter && iter <= Crypto.MAX_ITER
                    && Crypto.unb64(salt).length == Crypto.SALT_SIZE && Crypto.unb64(nonce).length == Crypto.NONCE_SIZE
                    && Crypto.unb64(wrapped).length == Crypto.KEY_SIZE + 16 && Crypto.unb64(check).length == 32;
        } catch (Crypto.CorruptException e) {
            return false;
        }
    }

    /** A new vault's keyring: random id, salt, nonce. */
    public static Keyring create(String password, byte[] vk, int iter, Stamp stamp) throws GeneralSecurityException {
        if (iter < Crypto.MIN_ITER) throw new GeneralSecurityException("kdf iteration count below the minimum");
        return wrap(password, vk, Crypto.randomHex(8), Crypto.random(Crypto.SALT_SIZE), Crypto.random(Crypto.NONCE_SIZE), iter, stamp);
    }

    /** The same vault key under a new password (same id, new salt and nonce). */
    public Keyring rewrap(String password, byte[] vk, int iter, Stamp stamp) throws GeneralSecurityException {
        if (iter < Crypto.MIN_ITER) throw new GeneralSecurityException("kdf iteration count below the minimum");
        return wrap(password, vk, id, Crypto.random(Crypto.SALT_SIZE), Crypto.random(Crypto.NONCE_SIZE), iter, stamp);
    }

    /** Wraps vk with explicit parameters (vectors use this). */
    public static Keyring wrap(String password, byte[] vk, String id, byte[] salt, byte[] nonce, int iter, Stamp stamp)
            throws GeneralSecurityException {
        if (vk.length != Crypto.KEY_SIZE || salt.length != Crypto.SALT_SIZE) throw new GeneralSecurityException("bad key or salt size");
        byte[] mk = Crypto.deriveKey(password, salt, iter);
        try {
            byte[] w = Crypto.seal(mk, nonce, vk, Crypto.AAD_KEYRING);
            return new Keyring(id, iter, Crypto.b64(salt), Crypto.b64(nonce), Crypto.b64(w), Crypto.b64(Crypto.check(vk)), stamp);
        } finally {
            Crypto.wipe(mk);
        }
    }

    /** Derives the master key and unwraps the vault key. Wrong password: WrongPasswordException. */
    public byte[] unwrap(String password) throws GeneralSecurityException {
        byte[] s = Crypto.unb64(salt), n = Crypto.unb64(nonce), w = Crypto.unb64(wrapped), c = Crypto.unb64(check);
        if (iter < 1 || iter > Crypto.MAX_ITER) throw new Crypto.CorruptException();
        byte[] mk = Crypto.deriveKey(password, s, iter);
        byte[] vk;
        try {
            vk = Crypto.open(mk, n, w, Crypto.AAD_KEYRING);
        } catch (Crypto.CorruptException e) {
            throw new Crypto.WrongPasswordException();
        } finally {
            Crypto.wipe(mk);
        }
        if (!checks(vk)) {
            Crypto.wipe(vk);
            throw new Crypto.CorruptException();
        }
        return vk;
    }

    /** Reports whether vk is this keyring's vault key (HMAC check). */
    public boolean checks(byte[] vk) throws GeneralSecurityException {
        return vk != null && vk.length == Crypto.KEY_SIZE && Crypto.equal(Crypto.check(vk), Crypto.unb64(check));
    }

    /** Reports whether s is 16 lowercase hex chars. */
    public static boolean validId(String s) {
        if (s == null || s.length() != 16) return false;
        for (int i = 0; i < 16; i++) {
            char c = s.charAt(i);
            if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'))) return false;
        }
        return true;
    }

    /** The result of choosing among keyring entries. */
    public static final class Choice {
        /** The current keyring, or null if there is no valid one. */
        public final Keyring current;
        /** Ids of other vaults (set up concurrently; their items are orphaned). */
        public final List<String> others;

        Choice(Keyring current, List<String> others) {
            this.current = current;
            this.others = others;
        }
    }

    /**
     * Picks the vault (the id of the valid entry with the smallest stamp) and its current keyring
     * (the greatest stamp with that id whose check equals the first one's), as the Go app's
     * ChooseKeyring does.
     */
    public static Choice choose(List<Keyring> all, int minIter) {
        List<Keyring> valid = new ArrayList<>();
        for (Keyring k : all) if (k != null && k.valid(minIter)) valid.add(k);
        if (valid.isEmpty()) return new Choice(null, new ArrayList<String>());
        Collections.sort(valid, new Comparator<Keyring>() {
            public int compare(Keyring a, Keyring b) { return a.stamp.compareTo(b.stamp); }
        });
        String id = valid.get(0).id, check = valid.get(0).check;
        Keyring cur = null;
        List<String> others = new ArrayList<>();
        for (Keyring k : valid) {
            if (k.id.equals(id)) {
                // The vault key never changes, so neither does its check: a later keyring of this
                // vault that wraps another key (a broken client, or a stray write) is ignored, or
                // this device would write entries the others cannot read under the same vault id.
                if (k.check.equals(check)) cur = k; // ascending: the last one wins
            } else if (!others.contains(k.id)) others.add(k.id);
        }
        return new Choice(cur, others);
    }
}
