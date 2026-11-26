package app.raypass.core;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Locale;
import java.util.Set;

/**
 * URL matching of DESIGN.md "URL matching": two URLs match when their registrable domains
 * (eTLD+1 with a short embedded list of multi-part public suffixes, else the last two labels) are
 * equal; IP addresses, localhost and single-label hosts need the same host and port; Android apps
 * are {@code androidapp://<package>} and match exactly. Domain matching is for http and https only:
 * any other scheme needs the same scheme, host and port. A few groups of registrable domains are one
 * account (urlmatch/equivalent.txt: x.com and twitter.com, …) and match each other. Ranking: same
 * host (www. ignored) 0, same registrable domain 1, equivalent domains 2, no match -1. Mirrors the
 * Go app's urlmatch package rule for rule, with its suffix list and equivalent groups loaded from
 * urlmatch/suffixes.txt and urlmatch/equivalent.txt ({@link #loadSuffixes}, {@link
 * #loadEquivalent}). Pure Java so the autofill matching is tested on the JDK.
 */
public final class UrlMatch {
    private UrlMatch() {}

    /**
     * The built-in copy of urlmatch/suffixes.txt (android/test.sh checks they are equal), used
     * until {@link #loadSuffixes} replaces it with the copy packed as res/raw/suffixes.txt.
     */
    public static final java.util.List<String> BUILTIN = java.util.Collections.unmodifiableList(Arrays.asList(
            "co.uk", "org.uk", "ac.uk", "gov.uk", "me.uk", "net.uk", "ltd.uk", "plc.uk", "sch.uk", "nhs.uk",
            "police.uk", "com.au", "net.au", "org.au", "edu.au", "gov.au", "asn.au", "id.au", "co.nz", "org.nz",
            "net.nz", "govt.nz", "ac.nz", "co.jp", "ne.jp", "or.jp", "ac.jp", "go.jp", "ad.jp", "ed.jp", "gr.jp",
            "lg.jp", "co.kr", "or.kr", "ne.kr", "go.kr", "ac.kr", "co.in", "net.in", "org.in", "firm.in", "gen.in",
            "ind.in", "ac.in", "gov.in", "com.br", "net.br", "org.br", "gov.br", "edu.br", "com.cn", "net.cn", "org.cn",
            "gov.cn", "edu.cn", "com.tw", "org.tw", "net.tw", "edu.tw", "gov.tw", "com.hk", "org.hk", "net.hk",
            "edu.hk", "gov.hk", "com.sg", "org.sg", "net.sg", "edu.sg", "gov.sg", "com.my", "org.my", "net.my",
            "com.mx", "org.mx", "gob.mx", "edu.mx", "com.ar", "org.ar", "gob.ar", "com.tr", "org.tr", "gov.tr",
            "edu.tr", "co.za", "org.za", "gov.za", "co.il", "org.il", "ac.il", "gov.il", "com.ua", "org.ua", "net.ua",
            "co.id", "or.id", "ac.id", "go.id", "com.ph", "org.ph", "com.vn", "net.vn", "com.pk", "org.pk", "com.ng",
            "org.ng", "com.eg", "com.sa", "co.th", "in.th", "ac.th", "go.th", "com.co", "com.pe", "com.ve", "co.at",
            "or.at", "com.pl", "net.pl", "org.pl", "com.ru", "com.es", "org.es", "com.gr", "com.cy", "com.mt", "co.ke",
            "com.bd", "com.np", "com.lk", "github.io", "gitlab.io", "bitbucket.io", "codeberg.page", "readthedocs.io",
            "gitbook.io", "herokuapp.com", "appspot.com", "blogspot.com", "pythonanywhere.com", "netlify.app",
            "vercel.app", "now.sh", "pages.dev", "workers.dev", "trycloudflare.com", "web.app", "firebaseapp.com",
            "azurewebsites.net", "azurestaticapps.net", "cloudapp.net", "cloudfront.net", "s3.amazonaws.com",
            "cloudfunctions.net", "fly.dev", "onrender.com", "glitch.me", "repl.co", "surge.sh", "deno.dev",
            "up.railway.app", "hf.space", "streamlit.app", "ngrok.io", "ngrok.app", "ngrok-free.app", "loca.lt",
            "myshopify.com", "wordpress.com", "tumblr.com", "substack.com", "webflow.io", "neocities.org",
            "000webhostapp.com", "sharepoint.com", "ddns.net", "duckdns.org", "no-ip.org", "dyndns.org"));

    /**
     * The built-in copy of urlmatch/equivalent.txt, one group per string (android/test.sh checks
     * they are equal), used until {@link #loadEquivalent} reads res/raw/equivalent.txt.
     */
    public static final java.util.List<String> BUILTIN_EQUIVALENT = java.util.Collections.unmodifiableList(Arrays.asList(
            "x.com twitter.com",
            "google.com youtube.com gmail.com",
            "microsoft.com live.com outlook.com hotmail.com xbox.com office.com",
            "apple.com icloud.com",
            "facebook.com messenger.com"));

    /** Registrable domain → its group number in the equivalent groups in use. */
    static final java.util.Map<String, Integer> GROUPS = groups(BUILTIN_EQUIVALENT);

    private static java.util.Map<String, Integer> groups(java.util.List<String> lines) {
        java.util.Map<String, Integer> out = new java.util.HashMap<>();
        int n = 0;
        for (String line : lines) {
            n++;
            if (line.trim().startsWith("#")) continue;
            for (String d : line.trim().split("\\s+")) if (!d.isEmpty()) out.put(d.toLowerCase(Locale.ROOT), n);
        }
        return out;
    }

    /** Replaces the equivalent groups with urlmatch/equivalent.txt (one group per line, "#" comments). */
    public static synchronized void loadEquivalent(java.io.InputStream in) throws java.io.IOException {
        java.io.BufferedReader r = new java.io.BufferedReader(new java.io.InputStreamReader(in, java.nio.charset.StandardCharsets.UTF_8));
        java.util.List<String> lines = new java.util.ArrayList<>();
        for (String line; (line = r.readLine()) != null; ) lines.add(line);
        java.util.Map<String, Integer> g = groups(lines);
        if (g.isEmpty()) throw new java.io.IOException("empty equivalent list");
        GROUPS.clear();
        GROUPS.putAll(g);
    }

    /** Whether two registrable domains differ but are in one equivalent group (one account works on both). */
    public static boolean equivalent(String a, String b) {
        Integer ga = GROUPS.get(a.toLowerCase(Locale.ROOT)), gb = GROUPS.get(b.toLowerCase(Locale.ROOT));
        return ga != null && ga.equals(gb) && !a.equalsIgnoreCase(b);
    }

    /** Multi-part public suffixes in use: {@link #BUILTIN} until {@link #loadSuffixes}. */
    static final Set<String> SUFFIXES = new HashSet<>(BUILTIN);

    /** Replaces the suffix list with the Go app's urlmatch/suffixes.txt ("#" comments, whitespace separated). */
    public static synchronized void loadSuffixes(java.io.InputStream in) throws java.io.IOException {
        java.io.BufferedReader r = new java.io.BufferedReader(new java.io.InputStreamReader(in, java.nio.charset.StandardCharsets.UTF_8));
        Set<String> out = new HashSet<>();
        for (String line; (line = r.readLine()) != null; ) {
            if (line.trim().startsWith("#")) continue;
            for (String w : line.trim().split("\\s+")) if (!w.isEmpty()) out.add(w.toLowerCase(Locale.ROOT));
        }
        if (out.isEmpty()) throw new java.io.IOException("empty suffix list");
        SUFFIXES.clear();
        SUFFIXES.addAll(out);
    }

    /** A parsed URL: scheme, host (lowercase, no brackets; the package for androidapp), port (scheme default filled in). */
    public static final class Url {
        /** Lowercase scheme. */
        public final String scheme;
        /** Lowercase host, or the package for androidapp. */
        public final String host;
        /** Port as given, or 443 / 80 for https / http, else "". */
        public final String port;

        Url(String scheme, String host, String port) {
            this.scheme = scheme;
            this.host = host;
            this.port = port;
        }
    }

    /** Parses u like the Go app's urlmatch.Parse (default scheme https); null when there is no host. */
    public static Url parse(String u) {
        if (u == null) return null;
        String s = u.trim();
        if (s.isEmpty()) return null;
        if (!s.contains("://")) s = "https://" + s;
        int i = s.indexOf("://");
        if (i <= 0 || !s.substring(0, i).matches("[A-Za-z][A-Za-z0-9+.-]*")) return null;
        String scheme = s.substring(0, i).toLowerCase(Locale.ROOT);
        s = s.substring(i + 3);
        int end = s.length();
        for (char c : new char[]{'/', '?', '#'}) {
            int j = s.indexOf(c);
            if (j >= 0 && j < end) end = j;
        }
        String auth = s.substring(0, end);
        int at = auth.lastIndexOf('@');
        if (at >= 0) auth = auth.substring(at + 1);
        if (scheme.equals("androidapp")) {
            // Go: host + path without a trailing slash, lowercased; no query or fragment allowed.
            String rest = s.substring(end);
            if (rest.startsWith("/")) {
                String path = rest;
                if (path.indexOf('?') >= 0 || path.indexOf('#') >= 0) return null;
                if (path.endsWith("/")) path = path.substring(0, path.length() - 1);
                if (path.contains("/")) return null;
            } else if (!rest.isEmpty()) {
                return null;
            }
            String pkg = auth.toLowerCase(Locale.ROOT);
            return pkg.isEmpty() ? null : new Url(scheme, pkg, "");
        }
        String host = auth, port = "";
        if (auth.startsWith("[")) {
            int rb = auth.indexOf(']');
            if (rb < 0) return null;
            host = auth.substring(1, rb);
            if (auth.length() > rb + 1) {
                if (auth.charAt(rb + 1) != ':') return null;
                port = auth.substring(rb + 2);
            }
        } else {
            int c = auth.lastIndexOf(':');
            if (c >= 0) {
                host = auth.substring(0, c);
                port = auth.substring(c + 1);
            }
        }
        if (!port.matches("[0-9]*")) return null;
        host = host.toLowerCase(Locale.ROOT);
        if (host.endsWith(".")) host = host.substring(0, host.length() - 1);
        if (host.isEmpty()) return null;
        if (port.isEmpty()) port = scheme.equals("https") ? "443" : scheme.equals("http") ? "80" : "";
        return new Url(scheme, host, port);
    }

    /** IP literal, localhost, *.localhost, or a single-label host: matched by host and port only. */
    public static boolean isIpOrLocal(String host) {
        if (host.equals("localhost") || host.endsWith(".localhost")) return true;
        if (host.contains(":")) return true; // IPv6
        if (host.matches("\\d{1,3}(\\.\\d{1,3}){3}")) return true;
        return !host.contains(".");
    }

    /** The registrable domain: the longest known suffix plus one label, else the last two labels. */
    public static String registrable(String host) {
        host = host.toLowerCase(Locale.ROOT);
        if (host.endsWith(".")) host = host.substring(0, host.length() - 1);
        if (isIpOrLocal(host)) return host;
        String[] l = host.split("\\.");
        for (int i = 0; i < l.length - 1; i++) {
            StringBuilder suf = new StringBuilder();
            for (int k = i + 1; k < l.length; k++) suf.append(k > i + 1 ? "." : "").append(l[k]);
            if (SUFFIXES.contains(suf.toString())) {
                return l[i] + "." + suf;
            }
        }
        if (l.length <= 2) return host;
        return l[l.length - 2] + "." + l[l.length - 1];
    }

    private static boolean web(String scheme) { return scheme.equals("http") || scheme.equals("https"); }

    private static String noWww(String h) { return h.startsWith("www.") ? h.substring(4) : h; }

    /**
     * 0: same host (www. ignored) or app; 1: same registrable domain; 2: equivalent domains; -1: no
     * match (Go's Match 3/2/1/0).
     */
    public static int rank(String itemUrl, String target) {
        Url a = parse(itemUrl), b = parse(target);
        if (a == null || b == null) return -1;
        boolean aa = a.scheme.equals("androidapp"), ba = b.scheme.equals("androidapp");
        if (aa || ba) return aa && ba && a.host.equals(b.host) ? 0 : -1;
        // Registrable-domain matching is for web pages only: javascript://bank.com/ or ftp://bank.com
        // has bank.com as its host but is not the bank's page. Anything not http(s) on either side
        // matches only the same scheme, host and port.
        if (!web(a.scheme) || !web(b.scheme)) {
            return a.scheme.equals(b.scheme) && a.host.equals(b.host) && a.port.equals(b.port) ? 0 : -1;
        }
        if (isIpOrLocal(a.host) || isIpOrLocal(b.host)) return a.host.equals(b.host) && a.port.equals(b.port) ? 0 : -1;
        if (a.host.equals(b.host) || noWww(a.host).equals(noWww(b.host))) return 0;
        String ra = registrable(a.host), rb = registrable(b.host);
        if (ra.equals(rb)) return 1;
        return equivalent(ra, rb) ? 2 : -1;
    }

    /** Reports whether the two URLs match. */
    public static boolean matches(String itemUrl, String target) { return rank(itemUrl, target) >= 0; }

    /** The URL to store for a saved login: scheme://host[:port], or androidapp://package. */
    public static String canonical(String u) {
        Url p = parse(u);
        if (p == null) return u.trim();
        if (p.scheme.equals("androidapp")) return "androidapp://" + p.host;
        String h = p.host.contains(":") ? "[" + p.host + "]" : p.host;
        boolean dflt = p.port.isEmpty() || (p.scheme.equals("https") && p.port.equals("443")) || (p.scheme.equals("http") && p.port.equals("80"));
        return p.scheme + "://" + h + (dflt ? "" : ":" + p.port);
    }

    /** A name for a new login: the host without "www.", or the app's package. */
    public static String displayHost(String u) {
        Url p = parse(u);
        if (p == null) return "";
        return p.host.startsWith("www.") ? p.host.substring(4) : p.host;
    }
}
