package app.raypass.core;

import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * An entry of the {@code vault} feed: {@code {"v":1,"k":keyring id,"n":nonce,"c":ciphertext}},
 * where c is AES-256-GCM(vk, n, {"ops":[MapOp...]}, "raypass/vault/v1"). Batches are split so
 * each body stays under Sol's 64 KiB limit, with the Go app's exact budget.
 */
public final class VaultEntry {
    /** Sol's body limit. */
    public static final int MAX_BODY = 64 << 10;
    /** Largest plaintext so the base64 body plus envelope stays under MAX_BODY. */
    public static final int MAX_PLAIN = (MAX_BODY - 512) / 4 * 3 - 16;
    /** Largest single op. */
    public static final int MAX_OP_SIZE = MAX_PLAIN - 16;

    /** Keyring id, nonce and ciphertext, base64 as stored. */
    public final String k, n, c;

    /** Makes an entry from its fields. */
    public VaultEntry(String k, String n, String c) {
        this.k = k;
        this.n = n;
        this.c = c;
    }

    /** The feed body. */
    public Map<String, Object> toJson() {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("v", 1L);
        m.put("k", k);
        m.put("n", n);
        m.put("c", c);
        return m;
    }

    /** Parses a feed body; null if it is not a v1 entry. */
    public static VaultEntry parse(Object body) {
        Map<String, Object> m = Json.obj(body);
        if (m == null || Json.num(m, "v", 0) != 1) return null;
        String n = Json.str(m, "n", ""), c = Json.str(m, "c", "");
        if (n.isEmpty() || c.isEmpty()) return null;
        return new VaultEntry(Json.str(m, "k", ""), n, c);
    }

    /** Encrypts a plaintext batch with a given nonce (vectors) . */
    public static VaultEntry encrypt(byte[] vk, String keyringId, byte[] plaintext, byte[] nonce) throws GeneralSecurityException {
        return new VaultEntry(keyringId, Crypto.b64(nonce), Crypto.b64(Crypto.seal(vk, nonce, plaintext, Crypto.AAD_VAULT)));
    }

    /** The decrypted plaintext bytes (caller wipes). */
    public byte[] decryptRaw(byte[] vk) throws GeneralSecurityException {
        return Crypto.open(vk, Crypto.unb64(n), Crypto.unb64(c), Crypto.AAD_VAULT);
    }

    /** Decrypts and parses the ops. */
    public List<MapOp> decrypt(byte[] vk) throws GeneralSecurityException {
        byte[] p = decryptRaw(vk);
        try {
            return MapOp.parseBatch(new String(p, StandardCharsets.UTF_8));
        } catch (IllegalArgumentException e) {
            throw new Crypto.CorruptException();
        } finally {
            Crypto.wipe(p);
        }
    }

    /** Splits ops into batches whose plaintext fits MAX_PLAIN; an op too large on its own is an error. */
    public static List<List<MapOp>> split(List<MapOp> ops) throws GeneralSecurityException {
        List<List<MapOp>> out = new ArrayList<>();
        List<MapOp> cur = new ArrayList<>();
        int empty = "{\"ops\":[]}".length();
        int size = empty;
        for (MapOp op : ops) {
            int len = Json.write(op.toJson()).getBytes(StandardCharsets.UTF_8).length;
            if (len > MAX_OP_SIZE) throw new GeneralSecurityException("a value is too large to store");
            if (!cur.isEmpty() && size + 1 + len > MAX_PLAIN) {
                out.add(cur);
                cur = new ArrayList<>();
                size = empty;
            }
            if (!cur.isEmpty()) size++;
            cur.add(op);
            size += len;
        }
        if (!cur.isEmpty()) out.add(cur);
        return out;
    }

    /** Splits and encrypts ops with random nonces. */
    public static List<VaultEntry> encryptOps(byte[] vk, String keyringId, List<MapOp> ops) throws GeneralSecurityException {
        List<VaultEntry> out = new ArrayList<>();
        for (List<MapOp> b : split(ops)) {
            byte[] p = MapOp.batchJson(b).getBytes(StandardCharsets.UTF_8);
            try {
                out.add(encrypt(vk, keyringId, p, Crypto.random(Crypto.NONCE_SIZE)));
            } finally {
                Crypto.wipe(p);
            }
        }
        return out;
    }
}
