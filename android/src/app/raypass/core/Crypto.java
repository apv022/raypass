package app.raypass.core;

import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.Arrays;
import java.util.Base64;

import javax.crypto.Cipher;
import javax.crypto.Mac;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

/**
 * The crypto primitives of DESIGN.md "Crypto", byte for byte the same as the Go app's vault
 * package: PBKDF2-HMAC-SHA256 over the password's UTF-8 bytes, AES-256-GCM with 12-byte nonces,
 * a 128-bit tag appended and a fixed AAD string, HMAC-SHA256 key check, standard padded base64.
 *
 * PBKDF2 is computed here over {@code Mac("HmacSHA256")} instead of SecretKeyFactory because
 * SecretKeyFactory takes a {@code char[]} and leaves the char-to-byte encoding to the provider
 * (UTF-8 on the JDK and Android's BouncyCastle today, but not a documented promise); the Go side
 * uses the raw UTF-8 bytes, so we do too. Errors never carry key material or plaintext.
 */
public final class Crypto {
    private Crypto() {}

    /** AAD of the keyring wrap. */
    public static final String AAD_KEYRING = "raypass/keyring/v1";
    /** AAD of vault entries. */
    public static final String AAD_VAULT = "raypass/vault/v1";
    /** AAD of the encrypted export. */
    public static final String AAD_EXPORT = "raypass/export/v1";
    /** HMAC label of the keyring check. */
    public static final String CHECK_LABEL = "raypass/check/v1";
    /** KDF algorithm name in the keyring. */
    public static final String KDF_ALG = "pbkdf2-sha256";
    /** Default PBKDF2 rounds for new keyrings. */
    public static final int DEFAULT_ITER = 600_000;
    /** Smallest round count a keyring may declare. */
    public static final int MIN_ITER = 100_000;
    /** Largest round count we will compute (a crafted keyring must not hang us). */
    public static final int MAX_ITER = 10_000_000;
    /** Key, salt and nonce sizes in bytes. */
    public static final int KEY_SIZE = 32, SALT_SIZE = 16, NONCE_SIZE = 12;

    /** A wrong master password (GCM of the wrap failed). */
    public static final class WrongPasswordException extends GeneralSecurityException {
        /** Makes one. */
        public WrongPasswordException() { super("wrong password"); }
    }

    /** Data that does not decrypt or parse. */
    public static final class CorruptException extends GeneralSecurityException {
        /** Makes one. */
        public CorruptException() { super("corrupt data"); }
    }

    private static final SecureRandom RNG = new SecureRandom();

    /** n random bytes from SecureRandom. */
    public static byte[] random(int n) {
        byte[] b = new byte[n];
        RNG.nextBytes(b);
        return b;
    }

    /** 2n lowercase hex chars from n random bytes (ids). */
    public static String randomHex(int n) { return hex(random(n)); }

    /** Lowercase hex of b. */
    public static String hex(byte[] b) {
        StringBuilder s = new StringBuilder(b.length * 2);
        for (byte x : b) s.append(Character.forDigit((x >> 4) & 0xf, 16)).append(Character.forDigit(x & 0xf, 16));
        return s.toString();
    }

    /** Decodes hex (tests). */
    public static byte[] unhex(String s) {
        byte[] b = new byte[s.length() / 2];
        for (int i = 0; i < b.length; i++) b[i] = (byte) Integer.parseInt(s.substring(2 * i, 2 * i + 2), 16);
        return b;
    }

    /** Standard base64 with padding. */
    public static String b64(byte[] b) { return Base64.getEncoder().encodeToString(b); }

    /** Decodes standard base64 with padding; malformed input is CorruptException. */
    public static byte[] unb64(String s) throws CorruptException {
        if (s == null) throw new CorruptException();
        try {
            return Base64.getDecoder().decode(s);
        } catch (IllegalArgumentException e) {
            throw new CorruptException();
        }
    }

    /** PBKDF2-HMAC-SHA256(UTF-8(password), salt, iter) to 32 bytes. */
    public static byte[] deriveKey(String password, byte[] salt, int iter) throws GeneralSecurityException {
        byte[] pw = password.getBytes(StandardCharsets.UTF_8);
        try {
            return pbkdf2(pw, salt, iter, KEY_SIZE);
        } finally {
            Arrays.fill(pw, (byte) 0);
        }
    }

    /** PBKDF2 with HMAC-SHA256 (RFC 8018 §5.2) over raw password bytes. */
    public static byte[] pbkdf2(byte[] password, byte[] salt, int iter, int len) throws GeneralSecurityException {
        if (iter < 1 || iter > MAX_ITER) throw new GeneralSecurityException("bad kdf iteration count");
        Mac mac = Mac.getInstance("HmacSHA256");
        // HMAC keys may be empty in the RFC; SecretKeySpec refuses empty, so pad that case with
        // one zero byte, which HMAC treats identically (keys are zero-padded to the block size).
        mac.init(new SecretKeySpec(password.length == 0 ? new byte[1] : password, "HmacSHA256"));
        int hl = mac.getMacLength();
        int blocks = (len + hl - 1) / hl;
        byte[] out = new byte[len];
        byte[] u = new byte[hl], t = new byte[hl];
        for (int i = 1; i <= blocks; i++) {
            mac.update(salt);
            mac.update(new byte[]{(byte) (i >>> 24), (byte) (i >>> 16), (byte) (i >>> 8), (byte) i});
            mac.doFinal(u, 0);
            System.arraycopy(u, 0, t, 0, hl);
            for (int r = 1; r < iter; r++) {
                mac.update(u);
                mac.doFinal(u, 0);
                for (int k = 0; k < hl; k++) t[k] ^= u[k];
            }
            System.arraycopy(t, 0, out, (i - 1) * hl, Math.min(hl, len - (i - 1) * hl));
        }
        Arrays.fill(u, (byte) 0);
        Arrays.fill(t, (byte) 0);
        return out;
    }

    /** AES-256-GCM: ciphertext with the 16-byte tag appended. */
    public static byte[] seal(byte[] key, byte[] nonce, byte[] plaintext, String aad) throws GeneralSecurityException {
        if (key.length != KEY_SIZE) throw new GeneralSecurityException("bad key size");
        if (nonce.length != NONCE_SIZE) throw new GeneralSecurityException("bad nonce size");
        Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
        c.init(Cipher.ENCRYPT_MODE, new SecretKeySpec(key, "AES"), new GCMParameterSpec(128, nonce));
        c.updateAAD(aad.getBytes(StandardCharsets.UTF_8));
        return c.doFinal(plaintext);
    }

    /** The inverse of seal; a wrong key or tampered data is CorruptException. */
    public static byte[] open(byte[] key, byte[] nonce, byte[] ciphertext, String aad) throws GeneralSecurityException {
        if (key.length != KEY_SIZE) throw new GeneralSecurityException("bad key size");
        if (nonce.length != NONCE_SIZE || ciphertext.length < 16) throw new CorruptException();
        try {
            Cipher c = Cipher.getInstance("AES/GCM/NoPadding");
            c.init(Cipher.DECRYPT_MODE, new SecretKeySpec(key, "AES"), new GCMParameterSpec(128, nonce));
            c.updateAAD(aad.getBytes(StandardCharsets.UTF_8));
            return c.doFinal(ciphertext);
        } catch (javax.crypto.AEADBadTagException e) {
            throw new CorruptException();
        }
    }

    /** HMAC-SHA256(vk, "raypass/check/v1"). */
    public static byte[] check(byte[] vk) throws GeneralSecurityException {
        Mac m = Mac.getInstance("HmacSHA256");
        m.init(new SecretKeySpec(vk, "HmacSHA256"));
        return m.doFinal(CHECK_LABEL.getBytes(StandardCharsets.UTF_8));
    }

    /** Constant-time equality. */
    public static boolean equal(byte[] a, byte[] b) { return MessageDigest.isEqual(a, b); }

    /** Zeroes b (null is fine). */
    public static void wipe(byte[] b) { if (b != null) Arrays.fill(b, (byte) 0); }
}
