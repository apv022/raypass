package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.UrlMatch;

/** URL matching rules of DESIGN.md. */
final class UrlMatchTest {
    static void register(final String root) {
        test("url: the shared suffix list loads (../urlmatch/suffixes.txt)", new T.Case() { public void run() throws Exception {
            try (java.io.InputStream in = new java.io.FileInputStream(java.nio.file.Paths.get(root, "urlmatch", "suffixes.txt").toFile())) {
                UrlMatch.loadSuffixes(in);
            }
            eq("bucket.s3.amazonaws.com", UrlMatch.registrable("bucket.s3.amazonaws.com"), "s3 is a suffix in the shared list");
        }});
        test("url: the built-in suffix list equals ../urlmatch/suffixes.txt", new T.Case() { public void run() throws Exception {
            java.util.List<String> want = new java.util.ArrayList<>();
            for (String line : java.nio.file.Files.readAllLines(java.nio.file.Paths.get(root, "urlmatch", "suffixes.txt"), java.nio.charset.StandardCharsets.UTF_8)) {
                if (line.trim().startsWith("#")) continue;
                for (String w : line.trim().split("\\s+")) if (!w.isEmpty()) want.add(w);
            }
            eq(want.toString(), UrlMatch.BUILTIN.toString(), "UrlMatch.BUILTIN is a copy of urlmatch/suffixes.txt");
        }});
        test("url: the Go urlmatch test table", new T.Case() { public void run() {
            String[][] parse = {{"GitHub.com", "https://github.com"}, {"HTTP://Example.COM.:8080/x?y=1", "http://example.com:8080"},
                    {"https://[::1]:9000/", "https://[::1]:9000"}, {"androidapp://com.Example.App", "androidapp://com.example.app"},
                    {" https://a.b.co.uk/login ", "https://a.b.co.uk"}};
            for (String[] c : parse) eq(c[1], UrlMatch.canonical(c[0]), "parse " + c[0]);
            String[][] reg = {{"github.com", "github.com"}, {"gist.github.com", "github.com"}, {"a.b.example.co.uk", "example.co.uk"},
                    {"alice.github.io", "alice.github.io"}, {"x.alice.github.io", "alice.github.io"}, {"localhost", "localhost"},
                    {"192.168.1.1", "192.168.1.1"}, {"co.uk", "co.uk"}, {"nas", "nas"}};
            for (String[] c : reg) eq(c[1], UrlMatch.registrable(c[0]), "registrable " + c[0]);
            Object[][] m = {{"https://github.com", "https://github.com/login", 2}, {"github.com", "https://www.github.com/", 2},
                    {"https://github.com", "https://gist.github.com", 1}, {"https://github.com", "http://github.com", 2},
                    {"https://github.com", "https://github.com.evil.com", 0}, {"https://evilgithub.com", "https://github.com", 0},
                    {"https://alice.github.io", "https://bob.github.io", 0}, {"https://shop.example.co.uk", "https://login.example.co.uk", 1},
                    {"https://a.co.uk", "https://b.co.uk", 0}, {"http://192.168.1.1:8080", "http://192.168.1.1:8080/admin", 2},
                    {"http://192.168.1.1:8080", "http://192.168.1.1:8081", 0}, {"http://localhost:3000", "http://localhost:3000/x", 2},
                    {"http://localhost:3000", "http://localhost:4000", 0}, {"androidapp://com.example.app", "androidapp://com.example.app", 2},
                    {"androidapp://com.example.app", "androidapp://com.example.app2", 0}, {"androidapp://com.example", "https://example.com", 0},
                    {"", "https://x.com", 0},
                    // Not web pages: the host alone must not let a login fill.
                    {"https://bank.com", "javascript://bank.com/%0aalert(1)", 0}, {"https://bank.com", "ftp://bank.com", 0},
                    {"https://bank.com", "data://bank.com/x", 0}, {"https://bank.com", "javascript:alert(1)", 0},
                    {"https://bank.com", "data:text/html,https://bank.com", 0}, {"ftp://files.example.com", "ftp://files.example.com/pub", 2},
                    {"ftp://files.example.com", "https://files.example.com", 0},
                    // Userinfo is not the host; trailing dots and case do not matter.
                    {"https://bank.com", "https://bank.com@evil.com/", 0}, {"https://bank.com", "https://evil.com@bank.com/", 2},
                    {"https://bank.com", "HTTPS://BANK.COM./login", 2}, {"https://bank.com", "https://bank.com.evil.com.", 0},
                    // Private suffixes: every customer of a host is a different site.
                    {"https://alice.herokuapp.com", "https://bob.herokuapp.com", 0},
                    {"https://shop.myshopify.com", "https://admin.shop.myshopify.com", 1},
                    {"https://a.up.railway.app", "https://b.up.railway.app", 0}};
            for (Object[] c : m) eq(2 - (Integer) c[2], UrlMatch.rank((String) c[0], (String) c[1]) == -1 ? 2 : UrlMatch.rank((String) c[0], (String) c[1]),
                    "match " + c[0] + " vs " + c[1]);
        }});
        test("autofill target: web domain only from known browsers", new T.Case() { public void run() {
            eq("https://github.com", app.raypass.core.AutofillTarget.url("com.android.chrome", "GitHub.com", null), "browser");
            eq("http://intranet.local", app.raypass.core.AutofillTarget.url("org.mozilla.firefox", "intranet.local", "http"), "scheme");
            eq("androidapp://com.evil.app", app.raypass.core.AutofillTarget.url("com.evil.app", "github.com", "https"), "untrusted app claims a domain");
            eq("androidapp://com.example", app.raypass.core.AutofillTarget.url("com.example", null, null), "app");
            check(app.raypass.core.AutofillTarget.url("", "x.com", null) == null, "no package");
        }});
        test("url: registrable domains", new T.Case() { public void run() {
            eq("example.com", UrlMatch.registrable("a.b.example.com"), "two labels");
            eq("example.co.uk", UrlMatch.registrable("www.example.co.uk"), "co.uk");
            eq("me.github.io", UrlMatch.registrable("x.me.github.io"), "github.io");
            eq("example.com", UrlMatch.registrable("example.com"), "itself");
            eq("192.168.1.2", UrlMatch.registrable("192.168.1.2"), "ip");
        }});
        test("url: matching and ranks", new T.Case() { public void run() {
            eq(0, UrlMatch.rank("https://GitHub.com/login", "github.com"), "default scheme, case, path");
            eq(1, UrlMatch.rank("https://github.com", "https://gist.github.com/x"), "same registrable");
            eq(-1, UrlMatch.rank("https://github.com", "https://github.com.evil.io"), "suffix trick");
            eq(-1, UrlMatch.rank("https://evil-github.com", "https://github.com"), "lookalike");
            eq(-1, UrlMatch.rank("https://a.github.io", "https://b.github.io"), "github.io users differ");
            eq(-1, UrlMatch.rank("https://example.co.uk", "https://other.co.uk"), "co.uk is a suffix");
            eq(0, UrlMatch.rank("http://192.168.1.1:8080/x", "http://192.168.1.1:8080"), "ip host+port");
            eq(-1, UrlMatch.rank("http://192.168.1.1:8080", "http://192.168.1.1:9090"), "ip port differs");
            eq(-1, UrlMatch.rank("http://localhost:3000", "http://localhost:4000"), "localhost port");
            eq(0, UrlMatch.rank("https://user:pw@example.com:443/", "https://example.com"), "userinfo and default port");
            eq(0, UrlMatch.rank("androidapp://com.example.app", "androidapp://com.example.app"), "android exact");
            eq(-1, UrlMatch.rank("androidapp://com.example.app", "androidapp://com.example.app2"), "android differs");
            eq(-1, UrlMatch.rank("androidapp://com.example", "https://example.com"), "app vs web");
            eq(-1, UrlMatch.rank("", "https://example.com"), "empty");
        }});
        test("url: canonical form and display host", new T.Case() { public void run() {
            eq("https://www.example.com", UrlMatch.canonical("WWW.Example.com/path?q=1"), "canonical");
            eq("http://[::1]:8080", UrlMatch.canonical("http://[::1]:8080/x"), "ipv6");
            eq("androidapp://com.x.y", UrlMatch.canonical("androidapp://com.x.y"), "app");
            eq("example.com", UrlMatch.displayHost("https://www.example.com/"), "display");
        }});
    }
}
