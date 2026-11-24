package app.raypass.core;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.GeneralSecurityException;
import java.util.HashMap;
import java.util.Locale;
import java.util.Map;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;

/**
 * TOTP codes (RFC 4226 / RFC 6238): {@code otpauth://totp/...} URIs, bare base32 secrets and Steam
 * Guard ({@code steam://SECRET}, 5 characters from Steam's alphabet). Parsing follows the Go app's
 * totp package so both accept the same strings. The secret never appears in an exception.
 */
public final class Totp {
    /** Not a valid secret or URI. */
    public static final class Invalid extends Exception {
        Invalid() { super("not a valid TOTP secret or otpauth URI"); }
    }

    private static final String STEAM_CHARS = "23456789BCDFGHJKMNPQRTVWXY";

    /** Decoded secret. */
    public final byte[] secret;
    /** SHA1, SHA256 or SHA512. */
    public final String algorithm;
    /** 6–8, 5 for Steam. */
    public final int digits;
    /** Seconds. */
    public final int period;
    /** Steam Guard code. */
    public final boolean steam;
    /** Label parts from an otpauth URI ("" otherwise). */
    public final String issuer, account;

    private Totp(byte[] secret, String algorithm, int digits, int period, boolean steam, String issuer, String account) {
        this.secret = secret;
        this.algorithm = algorithm;
        this.digits = digits;
        this.period = period;
        this.steam = steam;
        this.issuer = issuer;
        this.account = account;
    }

    /** Parses an otpauth URI, steam://SECRET or a bare base32 secret. */
    public static Totp parse(String s) throws Invalid {
        if (s == null) throw new Invalid();
        s = s.trim();
        String low = s.toLowerCase(Locale.ROOT);
        if (low.startsWith("steam://")) return new Totp(decodeSecret(s.substring(8)), "SHA1", 5, 30, true, "", "");
        if (low.startsWith("otpauth://")) return parseUri(s);
        return new Totp(decodeSecret(s), "SHA1", 6, 30, false, "", "");
    }

    private static Totp parseUri(String s) throws Invalid {
        String rest = s.substring("otpauth://".length());
        int hash = rest.indexOf('#');
        if (hash >= 0) rest = rest.substring(0, hash);
        int q = rest.indexOf('?');
        String query = q >= 0 ? rest.substring(q + 1) : "";
        String hostPath = q >= 0 ? rest.substring(0, q) : rest;
        int slash = hostPath.indexOf('/');
        String kind = (slash >= 0 ? hostPath.substring(0, slash) : hostPath).toLowerCase(Locale.ROOT);
        String path = slash >= 0 ? pct(hostPath.substring(slash + 1), false) : "";
        if (!kind.equals("totp") && !kind.equals("steam")) throw new Invalid();
        Map<String, String> qs = new HashMap<>();
        for (String kv : query.split("[&;]")) {
            if (kv.isEmpty()) continue;
            int e = kv.indexOf('=');
            String k = pct(e >= 0 ? kv.substring(0, e) : kv, true), v = e >= 0 ? pct(kv.substring(e + 1), true) : "";
            if (!qs.containsKey(k)) qs.put(k, v); // url.Values.Get returns the first
        }
        byte[] sec = decodeSecret(get(qs, "secret"));
        String issuer, account;
        int c = path.indexOf(':');
        if (c >= 0) {
            issuer = path.substring(0, c).trim();
            account = path.substring(c + 1).trim();
        } else {
            issuer = "";
            account = path.trim();
        }
        if (!get(qs, "issuer").isEmpty()) issuer = get(qs, "issuer");
        if (kind.equals("steam") || get(qs, "encoder").equalsIgnoreCase("steam")
                || (issuer.equalsIgnoreCase("steam") && get(qs, "digits").equals("5"))) {
            return new Totp(sec, "SHA1", 5, 30, true, issuer, account);
        }
        String alg = "SHA1";
        String a = get(qs, "algorithm");
        if (!a.isEmpty()) {
            switch (a.replace("-", "").toUpperCase(Locale.ROOT)) {
                case "SHA1": alg = "SHA1"; break;
                case "SHA256": alg = "SHA256"; break;
                case "SHA512": alg = "SHA512"; break;
                default: throw new Invalid();
            }
        }
        int digits = 6, period = 30;
        String d = get(qs, "digits");
        if (!d.isEmpty()) {
            digits = atoi(d);
            if (digits < 6 || digits > 8) throw new Invalid();
        }
        String p = get(qs, "period");
        if (!p.isEmpty()) {
            period = atoi(p);
            if (period < 1 || period > 3600) throw new Invalid();
        }
        return new Totp(sec, alg, digits, period, false, issuer, account);
    }

    private static String get(Map<String, String> m, String k) {
        String v = m.get(k);
        return v == null ? "" : v;
    }

    private static int atoi(String s) throws Invalid {
        try {
            return Integer.parseInt(s);
        } catch (NumberFormatException e) {
            throw new Invalid();
        }
    }

    /** Percent-decodes (and '+' to space in queries); a bad escape is Invalid like Go's url.Parse. */
    private static String pct(String s, boolean query) throws Invalid {
        ByteArrayOutputStream b = new ByteArrayOutputStream();
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c == '%') {
                if (i + 2 >= s.length()) throw new Invalid();
                int h = Character.digit(s.charAt(i + 1), 16), l = Character.digit(s.charAt(i + 2), 16);
                if (h < 0 || l < 0) throw new Invalid();
                b.write(h * 16 + l);
                i += 2;
            } else if (c == '+' && query) {
                b.write(' ');
            } else {
                byte[] u = String.valueOf(c).getBytes(StandardCharsets.UTF_8);
                if (Character.isHighSurrogate(c) && i + 1 < s.length()) u = s.substring(i, ++i + 1).getBytes(StandardCharsets.UTF_8);
                b.write(u, 0, u.length);
            }
        }
        return new String(b.toByteArray(), StandardCharsets.UTF_8);
    }

    /** Decodes base32 (RFC 4648, padding and spaces, dashes, tabs ignored, case-insensitive). */
    public static byte[] decodeSecret(String s) throws Invalid {
        StringBuilder t = new StringBuilder();
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            if (c == ' ' || c == '-' || c == '\t' || c == '=') continue;
            t.append(Character.toUpperCase(c));
        }
        int n = t.length();
        if (n == 0) throw new Invalid();
        int r = n % 8;
        if (r == 1 || r == 3 || r == 6) throw new Invalid();
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        int buf = 0, bits = 0;
        for (int i = 0; i < n; i++) {
            char c = t.charAt(i);
            int v;
            if (c >= 'A' && c <= 'Z') v = c - 'A';
            else if (c >= '2' && c <= '7') v = c - '2' + 26;
            else throw new Invalid();
            buf = (buf << 5) | v;
            bits += 5;
            if (bits >= 8) {
                bits -= 8;
                out.write((buf >> bits) & 0xff);
            }
        }
        if (out.size() == 0) throw new Invalid();
        return out.toByteArray();
    }

    private static int truncated(byte[] secret, long counter, String alg) {
        try {
            String name = "Hmac" + alg;
            Mac m = Mac.getInstance(name);
            m.init(new SecretKeySpec(secret, name));
            byte[] msg = new byte[8];
            for (int i = 7; i >= 0; i--) {
                msg[i] = (byte) counter;
                counter >>>= 8;
            }
            byte[] sum = m.doFinal(msg);
            int off = sum[sum.length - 1] & 0x0f;
            return ((sum[off] & 0x7f) << 24) | ((sum[off + 1] & 0xff) << 16) | ((sum[off + 2] & 0xff) << 8) | (sum[off + 3] & 0xff);
        } catch (GeneralSecurityException e) {
            throw new IllegalStateException("hmac unavailable");
        }
    }

    /** RFC 4226 HOTP with the given digits and algorithm. */
    public static String hotp(byte[] secret, long counter, int digits, String alg) {
        long v = truncated(secret, counter, alg) & 0xffffffffL;
        long mod = 1;
        for (int i = 0; i < digits; i++) mod *= 10;
        StringBuilder s = new StringBuilder(Long.toString(v % mod));
        while (s.length() < digits) s.insert(0, '0');
        return s.toString();
    }

    /** Steam Guard's 5-character code. */
    public static String steamCode(byte[] secret, long counter) {
        long v = truncated(secret, counter, "SHA1") & 0xffffffffL;
        char[] b = new char[5];
        for (int i = 0; i < 5; i++) {
            b[i] = STEAM_CHARS.charAt((int) (v % STEAM_CHARS.length()));
            v /= STEAM_CHARS.length();
        }
        return new String(b);
    }

    /** The code at unix time ms. */
    public String code(long unixMs) {
        long sec = Math.floorDiv(unixMs, 1000L);
        long counter = sec / (period <= 0 ? 30 : period);
        return steam ? steamCode(secret, counter) : hotp(secret, counter, digits, algorithm);
    }

    /** Seconds until the code at unixMs changes (1..period). */
    public int remaining(long unixMs) {
        long sec = Math.floorDiv(unixMs, 1000L);
        int p = period <= 0 ? 30 : period;
        return (int) (p - sec % p);
    }
}
