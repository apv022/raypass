package app.raypass.core;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Locale;
import java.util.Set;

/**
 * URL matching of DESIGN.md "URL matching": two URLs match when their registrable domains
 * (eTLD+1 with a short embedded list of multi-part public suffixes, else the last two labels) are
 * equal; IP addresses and localhost need the same host and port; Android apps are
 * {@code androidapp://<package>} and match exactly. Ranking: exact host 0, same registrable
 * domain 1, no match -1. Pure Java so the autofill matching is tested on the JDK.
 */
public final class UrlMatch {
    private UrlMatch() {}

    /** Multi-part public suffixes we know (kept in step with the Go app's urlmatch list). */
    static final Set<String> SUFFIXES = new HashSet<>(Arrays.asList(
            "co.uk", "org.uk", "me.uk", "ac.uk", "gov.uk", "ltd.uk", "plc.uk", "net.uk", "nhs.uk", "sch.uk",
            "com.au", "net.au", "org.au", "edu.au", "gov.au", "asn.au", "id.au",
            "co.nz", "net.nz", "org.nz", "govt.nz", "ac.nz",
            "co.jp", "ne.jp", "or.jp", "ac.jp", "go.jp", "gr.jp",
            "co.kr", "or.kr", "ne.kr", "go.kr", "ac.kr",
            "co.in", "net.in", "org.in", "gov.in", "ac.in", "firm.in", "gen.in", "ind.in",
            "co.za", "org.za", "gov.za", "ac.za", "web.za",
            "co.il", "org.il", "ac.il", "gov.il",
            "co.id", "or.id", "ac.id", "go.id", "web.id",
            "co.th", "in.th", "ac.th", "go.th", "or.th",
            "com.br", "net.br", "org.br", "gov.br", "edu.br",
            "com.ar", "gob.ar", "org.ar", "net.ar",
            "com.mx", "gob.mx", "org.mx", "edu.mx", "net.mx",
            "com.cn", "net.cn", "org.cn", "gov.cn", "edu.cn",
            "com.hk", "org.hk", "net.hk", "edu.hk", "gov.hk",
            "com.tw", "org.tw", "net.tw", "edu.tw", "gov.tw",
            "com.sg", "org.sg", "net.sg", "edu.sg", "gov.sg",
            "com.my", "org.my", "net.my", "edu.my", "gov.my",
            "com.tr", "org.tr", "net.tr", "gov.tr", "edu.tr",
            "com.ua", "org.ua", "net.ua", "gov.ua", "in.ua",
            "com.pl", "net.pl", "org.pl",
            "com.es", "org.es", "nom.es",
            "com.pt", "org.pt",
            "com.ph", "org.ph", "net.ph",
            "com.vn", "net.vn", "org.vn",
            "com.pk", "org.pk", "net.pk",
            "com.eg", "com.sa", "com.ng", "com.co", "com.pe", "com.ve", "com.uy", "com.ec",
            "co.ke", "or.ke", "co.tz", "co.ug",
            "github.io", "gitlab.io", "herokuapp.com", "netlify.app", "vercel.app", "pages.dev", "workers.dev",
            "web.app", "firebaseapp.com", "appspot.com", "blogspot.com", "azurewebsites.net", "cloudfront.net",
            "s3.amazonaws.com", "glitch.me", "fly.dev", "onrender.com", "repl.co", "ngrok.io", "ngrok-free.app",
            "duckdns.org", "dyndns.org", "no-ip.org", "myshopify.com", "wordpress.com", "tumblr.com"));

    /** A parsed URL: scheme, host (lowercase, no brackets), port ("" for default). */
    public static final class Url {
        /** Lowercase scheme. */
        public final String scheme;
        /** Lowercase host, or the package for androidapp. */
        public final String host;
        /** Port or "". */
        public final String port;

        Url(String scheme, String host, String port) {
            this.scheme = scheme;
            this.host = host;
            this.port = port;
        }
    }

    /** Parses u with the default scheme https; null when there is no host. */
    public static Url parse(String u) {
        if (u == null) return null;
        String s = u.trim();
        if (s.isEmpty()) return null;
        String scheme = "https";
        int i = s.indexOf("://");
        if (i > 0 && s.substring(0, i).matches("[A-Za-z][A-Za-z0-9+.-]*")) {
            scheme = s.substring(0, i).toLowerCase(Locale.ROOT);
            s = s.substring(i + 3);
        }
        int end = s.length();
        for (char c : new char[]{'/', '?', '#'}) {
            int j = s.indexOf(c);
            if (j >= 0 && j < end) end = j;
        }
        String auth = s.substring(0, end);
        int at = auth.lastIndexOf('@');
        if (at >= 0) auth = auth.substring(at + 1);
        if (scheme.equals("androidapp")) return auth.isEmpty() ? null : new Url(scheme, auth, "");
        String host = auth, port = "";
        if (auth.startsWith("[")) {
            int rb = auth.indexOf(']');
            if (rb < 0) return null;
            host = auth.substring(1, rb);
            if (auth.length() > rb + 1 && auth.charAt(rb + 1) == ':') port = auth.substring(rb + 2);
        } else {
            int c = auth.lastIndexOf(':');
            if (c >= 0) {
                host = auth.substring(0, c);
                port = auth.substring(c + 1);
            }
        }
        host = host.toLowerCase(Locale.ROOT);
        while (host.endsWith(".")) host = host.substring(0, host.length() - 1);
        if (host.isEmpty()) return null;
        if ((scheme.equals("https") && port.equals("443")) || (scheme.equals("http") && port.equals("80"))) port = "";
        return new Url(scheme, host, port);
    }

    /** Reports whether host is an IPv4/IPv6 literal or localhost. */
    public static boolean isIpOrLocal(String host) {
        if (host.equals("localhost") || host.endsWith(".localhost")) return true;
        if (host.contains(":")) return true; // IPv6
        return host.matches("\\d{1,3}(\\.\\d{1,3}){3}");
    }

    /** The registrable domain (eTLD+1) of a hostname; the host itself if it has fewer labels. */
    public static String registrable(String host) {
        if (isIpOrLocal(host)) return host;
        String[] l = host.split("\\.");
        if (l.length <= 2) return host;
        String last2 = l[l.length - 2] + "." + l[l.length - 1];
        if (SUFFIXES.contains(last2)) return l[l.length - 3] + "." + last2;
        if (l.length >= 4) {
            String last3 = l[l.length - 3] + "." + last2;
            if (SUFFIXES.contains(last3)) return l[l.length - 4] + "." + last3;
        }
        return last2;
    }

    /** 0: same host; 1: same registrable domain; -1: no match. */
    public static int rank(String itemUrl, String target) {
        Url a = parse(itemUrl), b = parse(target);
        if (a == null || b == null) return -1;
        boolean aa = a.scheme.equals("androidapp"), ba = b.scheme.equals("androidapp");
        if (aa || ba) return aa && ba && a.host.equals(b.host) ? 0 : -1;
        if (isIpOrLocal(a.host) || isIpOrLocal(b.host)) return a.host.equals(b.host) && a.port.equals(b.port) ? 0 : -1;
        if (a.host.equals(b.host)) return 0;
        return registrable(a.host).equals(registrable(b.host)) ? 1 : -1;
    }

    /** Reports whether the two URLs match. */
    public static boolean matches(String itemUrl, String target) { return rank(itemUrl, target) >= 0; }

    /** The URL to store for a saved login: scheme://host[:port], or androidapp://package. */
    public static String canonical(String u) {
        Url p = parse(u);
        if (p == null) return u.trim();
        if (p.scheme.equals("androidapp")) return "androidapp://" + p.host;
        String h = p.host.contains(":") ? "[" + p.host + "]" : p.host;
        return p.scheme + "://" + h + (p.port.isEmpty() ? "" : ":" + p.port);
    }

    /** A name for a new login: the host without "www.", or the app's package. */
    public static String displayHost(String u) {
        Url p = parse(u);
        if (p == null) return "";
        return p.host.startsWith("www.") ? p.host.substring(4) : p.host;
    }
}
