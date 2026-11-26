package app.raypass.core;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Set;

/**
 * A curated table of well-known Android apps and the web domain of their accounts
 * (com.twitter.android → x.com), plus words guessed from any other package name.
 *
 * Invariant: this is a hint for the autofill picker only (which logins it lists first and which
 * it shows before "Show all logins"), never a reason to fill on its own. A sideloaded app can
 * take any package name, and the official signing certificates are not pinned here, so trusting
 * the table for automatic fills would let such an app receive the real site's password. A login
 * fills an app automatically only when the person linked it ("Remember for <app>", which adds
 * androidapp://<package> to the login).
 */
public final class AppDomains {
    private AppDomains() {}

    /** Package (lowercase) → registrable domain. */
    static final Map<String, String> TABLE = new HashMap<>();

    static {
        String[] t = {
            "com.twitter.android", "x.com",
            "com.facebook.katana", "facebook.com",
            "com.facebook.lite", "facebook.com",
            "com.facebook.orca", "facebook.com",
            "com.instagram.android", "instagram.com",
            "com.instagram.barcelona", "instagram.com", // Threads signs in with Instagram
            "com.reddit.frontpage", "reddit.com",
            "com.discord", "discord.com",
            "com.github.android", "github.com",
            "com.spotify.music", "spotify.com",
            "com.netflix.mediaclient", "netflix.com",
            "com.amazon.mshop.android.shopping", "amazon.com",
            "com.amazon.avod.thirdpartyclient", "amazon.com",
            "com.linkedin.android", "linkedin.com",
            "com.snapchat.android", "snapchat.com",
            "com.pinterest", "pinterest.com",
            "org.telegram.messenger", "telegram.org",
            "com.paypal.android.p2pmobile", "paypal.com",
            "com.microsoft.office.outlook", "microsoft.com",
            "com.microsoft.teams", "microsoft.com",
            "com.microsoft.skydrive", "microsoft.com",
            "com.skype.raider", "microsoft.com",
            "com.google.android.gm", "google.com",
            "com.google.android.youtube", "google.com",
            "com.zhiliaoapp.musically", "tiktok.com",
            "com.ss.android.ugc.trill", "tiktok.com",
            "tv.twitch.android.app", "twitch.tv",
            "com.dropbox.android", "dropbox.com",
            "com.slack", "slack.com",
            "com.ebay.mobile", "ebay.com",
            "com.airbnb.android", "airbnb.com",
            "com.ubercab", "uber.com",
            "com.tumblr", "tumblr.com",
            "com.duolingo", "duolingo.com",
            "com.yahoo.mobile.client.android.mail", "yahoo.com",
            "com.valvesoftware.android.steam.community", "steampowered.com",
            "com.adobe.reader", "adobe.com",
            "com.evernote", "evernote.com",
            "notion.id", "notion.so",
            "com.trello", "trello.com",
            "com.booking", "booking.com",
            "com.etsy.android", "etsy.com",
            "com.coinbase.android", "coinbase.com",
            "com.binance.dev", "binance.com",
            "com.robinhood.android", "robinhood.com",
            "com.venmo", "venmo.com",
            "com.squareup.cash", "cash.app",
            "us.zoom.videomeetings", "zoom.us",
            "com.quora.android", "quora.com",
            "com.medium.reader", "medium.com",
            "com.tinder", "tinder.com",
            "com.vkontakte.android", "vk.com",
            "com.yelp.android", "yelp.com",
            "ch.protonmail.android", "proton.me",
            "com.disney.disneyplus", "disneyplus.com",
            "com.hulu.plus", "hulu.com",
            "com.scee.psxandroid", "playstation.com",
            "com.nintendo.znca", "nintendo.com",
        };
        for (int i = 0; i < t.length; i += 2) TABLE.put(t[i], t[i + 1]);
    }

    /** Parts of package names that say nothing about the service. */
    private static final Set<String> GENERIC = new HashSet<>(Arrays.asList(
            "com", "org", "net", "io", "me", "co", "tv", "app", "apps", "android", "mobile", "client", "phone",
            "main", "lite", "free", "pro", "beta", "official", "www", "the", "mobileapp", "droid", "release"));

    /** The domain of a well-known app (a hint for the picker, never trusted to fill), or null. */
    public static String domain(String pkg) {
        return pkg == null ? null : TABLE.get(pkg.toLowerCase(Locale.ROOT));
    }

    /**
     * Words a login for pkg probably has in its name or URL: the specific parts of the package
     * name (com.twitter.android → "twitter"; com.example.shop → "example", "shop").
     */
    public static List<String> words(String pkg) {
        if (pkg == null) return Collections.emptyList();
        List<String> out = new ArrayList<>();
        for (String w : pkg.toLowerCase(Locale.ROOT).split("[._]")) {
            if (w.length() >= 3 && !GENERIC.contains(w) && !out.contains(w)) out.add(w);
        }
        return out;
    }
}
