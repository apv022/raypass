package app.raypass.core;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.util.ArrayList;
import java.util.List;

/**
 * Password and passphrase generator (raypass.generate): SecureRandom.nextInt(bound), which
 * rejects to avoid modulo bias, over a character alphabet or the EFF large wordlist. Every chosen
 * character class is guaranteed to appear (resampling the whole password, so the result is still
 * uniform over the passwords that satisfy it).
 */
public final class Generator {
    /** Character classes. */
    public static final String UPPER = "ABCDEFGHIJKLMNOPQRSTUVWXYZ", LOWER = "abcdefghijklmnopqrstuvwxyz",
            DIGITS = "0123456789", SYMBOLS = "!#$%&*+-=?@^_~.,:;/()[]{}";
    /** Characters dropped by avoid_ambiguous. */
    public static final String AMBIGUOUS = "Il1O0o|`'\"5S2Z";

    private static final SecureRandom RNG = new SecureRandom();

    /** A generated secret and its entropy. */
    public static final class Result {
        /** The password or passphrase. */
        public final String password;
        /** log2 of the number of equally likely outputs (before the class guarantee). */
        public final double entropyBits;

        Result(String password, double entropyBits) {
            this.password = password;
            this.entropyBits = entropyBits;
        }
    }

    private final List<String> words;

    /** A generator using wordlist for passphrases (may be empty: words mode then fails). */
    public Generator(List<String> words) { this.words = words; }

    /** Reads a wordlist: one word per line, optionally after the EFF dice number and a tab. */
    public static List<String> readWords(InputStream in) throws IOException {
        List<String> out = new ArrayList<>();
        BufferedReader r = new BufferedReader(new InputStreamReader(in, StandardCharsets.UTF_8));
        java.util.Set<String> seen = new java.util.HashSet<>();
        for (String line; (line = r.readLine()) != null; ) {
            line = line.trim();
            if (line.startsWith("#")) continue; // the Go app's header comments
            int t = line.indexOf('\t'); // EFF format: "11111\tabacus"
            if (t >= 0) line = line.substring(t + 1).trim();
            if (!line.isEmpty() && seen.add(line)) out.add(line);
        }
        return out;
    }

    private static String strip(String set, boolean avoid) {
        if (!avoid) return set;
        StringBuilder b = new StringBuilder();
        for (char c : set.toCharArray()) if (AMBIGUOUS.indexOf(c) < 0) b.append(c);
        return b.toString();
    }

    /** chars mode: length 4–128. At least one class must be on. */
    public Result chars(int length, boolean upper, boolean lower, boolean digits, boolean symbols, boolean avoidAmbiguous) {
        if (length < 4 || length > 128) throw new IllegalArgumentException("length must be 4 to 128");
        List<String> sets = new ArrayList<>();
        if (upper) sets.add(strip(UPPER, avoidAmbiguous));
        if (lower) sets.add(strip(LOWER, avoidAmbiguous));
        if (digits) sets.add(strip(DIGITS, avoidAmbiguous));
        if (symbols) sets.add(strip(SYMBOLS, avoidAmbiguous));
        if (sets.isEmpty()) throw new IllegalArgumentException("choose at least one kind of character");
        StringBuilder all = new StringBuilder();
        for (String s : sets) all.append(s);
        String alphabet = all.toString();
        char[] out = new char[length];
        while (true) {
            for (int i = 0; i < length; i++) out[i] = alphabet.charAt(RNG.nextInt(alphabet.length()));
            boolean ok = true;
            if (sets.size() <= length) {
                for (String s : sets) {
                    boolean has = false;
                    for (char c : out) if (s.indexOf(c) >= 0) { has = true; break; }
                    if (!has) { ok = false; break; }
                }
            }
            if (ok) break;
        }
        return new Result(new String(out), charsEntropy(sets, length));
    }

    /** words mode: 3–20 words joined by separator; capitalize each; number appends a digit to one word. */
    public Result words(int count, String separator, boolean capitalize, boolean number) {
        if (count < 3 || count > 20) throw new IllegalArgumentException("words must be 3 to 20");
        if (words.isEmpty()) throw new IllegalStateException("no wordlist");
        if (separator == null) separator = "-";
        if (separator.getBytes(StandardCharsets.UTF_8).length > 8) throw new IllegalArgumentException("separator is too long");
        int numAt = number ? RNG.nextInt(count) : -1;
        StringBuilder b = new StringBuilder();
        for (int i = 0; i < count; i++) {
            String w = words.get(RNG.nextInt(words.size()));
            if (capitalize && !w.isEmpty()) w = new StringBuilder().appendCodePoint(Character.toUpperCase(w.codePointAt(0)))
                    .append(w.substring(Character.charCount(w.codePointAt(0)))).toString();
            if (i == numAt) w = w + RNG.nextInt(10);
            if (i > 0) b.append(separator);
            b.append(w);
        }
        double bits = count * log2(words.size());
        if (number) bits += log2(10) + log2(count);
        return new Result(b.toString(), bits);
    }

    /**
     * log2 of the number of length-l strings over the union of sets that contain every set at
     * least once (inclusion-exclusion), exactly like the Go app.
     */
    static double charsEntropy(List<String> sets, int l) {
        java.math.BigInteger total = java.math.BigInteger.ZERO;
        int n = sets.size();
        for (int mask = 0; mask < (1 << n); mask++) {
            int size = 0, bits = 0;
            for (int i = 0; i < n; i++) {
                if ((mask & (1 << i)) == 0) size += sets.get(i).length();
                else bits++;
            }
            java.math.BigInteger term = java.math.BigInteger.valueOf(size).pow(l);
            total = bits % 2 == 1 ? total.subtract(term) : total.add(term);
        }
        if (total.signum() <= 0) return 0;
        int shift = Math.max(0, total.bitLength() - 64);
        return log2(total.shiftRight(shift).doubleValue()) + shift;
    }

    private static double log2(double x) { return Math.log(x) / Math.log(2); }
}
