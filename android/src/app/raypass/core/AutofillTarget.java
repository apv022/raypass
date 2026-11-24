package app.raypass.core;

import java.util.Arrays;
import java.util.HashSet;
import java.util.Locale;
import java.util.Set;

/**
 * Decides which URL an autofill request is for. Any app can put any web domain into its own
 * view structure, so a web domain is trusted only from a known browser; every other app is
 * {@code androidapp://<package>} and matches only logins saved for that exact package.
 */
public final class AutofillTarget {
    private AutofillTarget() {}

    /** Browsers whose reported web domain we believe (they render pages, not their own UI). */
    public static final Set<String> BROWSERS = new HashSet<>(Arrays.asList(
            "com.android.chrome", "com.chrome.beta", "com.chrome.dev", "com.chrome.canary", "org.chromium.chrome",
            "com.google.android.apps.chrome", "org.mozilla.firefox", "org.mozilla.firefox_beta", "org.mozilla.fenix",
            "org.mozilla.fennec_fdroid", "org.mozilla.focus", "org.mozilla.klar", "us.spotco.fennec_dos", "io.github.forkmaintainers.iceraven",
            "org.torproject.torbrowser", "com.brave.browser", "com.brave.browser_beta", "com.brave.browser_nightly",
            "com.microsoft.emmx", "com.microsoft.emmx.beta", "com.sec.android.app.sbrowser", "com.sec.android.app.sbrowser.beta",
            "com.vivaldi.browser", "com.vivaldi.browser.snapshot", "com.opera.browser", "com.opera.browser.beta", "com.opera.mini.native",
            "com.opera.gx", "com.duckduckgo.mobile.android", "com.kiwibrowser.browser", "org.bromite.bromite", "org.cromite.cromite",
            "com.ecosia.android", "com.yandex.browser", "com.mi.globalbrowser", "com.UCMobile.intl", "mark.via.gp",
            "org.ungoogled.chromium.stable", "app.vanadium.browser", "com.qwant.liberty", "com.stoutner.privacybrowser.standard",
            "com.cookiegames.smartcookie", "acr.browser.lightning", "com.naver.whale", "com.huawei.browser"));

    /**
     * The URL to match: {@code <scheme>://<webDomain>} when a known browser reports a web domain
     * (scheme default https), else {@code androidapp://<package>}. Null when there is no package.
     */
    public static String url(String pkg, String webDomain, String webScheme) {
        if (pkg == null || pkg.isEmpty()) return null;
        if (webDomain != null && !webDomain.trim().isEmpty() && BROWSERS.contains(pkg)) {
            String scheme = webScheme == null || webScheme.isEmpty() ? "https" : webScheme.toLowerCase(Locale.ROOT);
            if (!scheme.equals("http") && !scheme.equals("https")) scheme = "https";
            return scheme + "://" + webDomain.trim().toLowerCase(Locale.ROOT);
        }
        return "androidapp://" + pkg;
    }
}
