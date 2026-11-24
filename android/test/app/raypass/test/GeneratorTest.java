package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.Generator;

import java.io.FileInputStream;
import java.io.InputStream;
import java.nio.file.Paths;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

/** Generator: classes present, lengths, entropy like the Go app, wordlist parsing. */
final class GeneratorTest {
    static void register(final String root) {
        final List<String>[] words = new List[1];
        test("generator: the shared wordlist loads (7776 unique words)", new T.Case() { public void run() throws Exception {
            try (InputStream in = new FileInputStream(Paths.get(root, "gen", "wordlist.txt").toFile())) {
                words[0] = Generator.readWords(in);
            }
            eq(7776, words[0].size(), "size");
            eq(7776, new HashSet<>(words[0]).size(), "unique");
        }});
        test("generator: chars mode has every class and the right entropy", new T.Case() { public void run() {
            Generator g = new Generator(words[0]);
            Set<String> seen = new HashSet<>();
            for (int i = 0; i < 300; i++) {
                Generator.Result r = g.chars(4, true, true, true, true, false);
                eq(4, r.password.length(), "length");
                check(r.password.matches(".*[A-Z].*") && r.password.matches(".*[a-z].*") && r.password.matches(".*[0-9].*")
                        && r.password.matches(".*[^A-Za-z0-9].*"), "all classes");
                seen.add(r.password);
            }
            check(seen.size() > 290, "random");
            Generator.Result r = g.chars(20, true, true, true, true, false);
            // 87^20 minus strings missing a class: a little under 20*log2(87) = 128.9 bits.
            check(r.entropyBits > 128 && r.entropyBits < 128.9, "entropy " + r.entropyBits);
            Generator.Result a = g.chars(64, true, true, true, false, true);
            for (char c : "Il1O0o5S2Z".toCharArray()) check(a.password.indexOf(c) < 0, "no ambiguous chars");
            near(Math.log(10) / Math.log(2) * 8, g.chars(8, false, false, true, false, false).entropyBits, 1e-9);
        }});
        test("generator: words mode", new T.Case() { public void run() {
            Generator g = new Generator(words[0]);
            Generator.Result r = g.words(5, "-", true, true);
            String[] parts = r.password.split("-");
            eq(5, parts.length, "words");
            for (String p : parts) check(Character.isUpperCase(p.charAt(0)), "capitalized");
            check(r.password.matches(".*[0-9].*"), "number");
            double want = 5 * Math.log(7776) / Math.log(2) + Math.log(10) / Math.log(2) + Math.log(5) / Math.log(2);
            near(want, r.entropyBits, 1e-9);
        }});
    }

    static void near(double want, double got, double tol) {
        if (Math.abs(want - got) > tol) fail("entropy: want " + want + ", got " + got);
    }
}
