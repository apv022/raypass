package app.raypass.core;

import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;

/**
 * The order of the autofill picker ("Search Raypass"): which logins it shows first for an app or
 * page, and which it shows before "Show all logins". Best first: the login filled into this app
 * or page a moment ago (a multi-step sign-in), logins that match its URL, logins for the domain
 * the curated table guesses for the app ({@link AppDomains}, equivalent domains included), logins
 * whose name or URL holds a word of the package name, then everything else. Ties: favourites,
 * then name.
 *
 * Invariant: a guess only orders the list. Filling always needs the person's tap on a login.
 */
public final class AutofillPick {
    private AutofillPick() {}

    /** Scores, best first. */
    public static final int RECENT = 0, MATCH = 1, APP_DOMAIN = 2, WORD = 3, OTHER = 9;

    /**
     * How well it fits a request for url. pkg is the app's package (null in a browser, whose
     * page URL is url); recentId the login filled here a moment ago, or null.
     */
    public static int score(Item it, String url, String pkg, String recentId) {
        if (it.id.equals(recentId)) return RECENT;
        List<String> urls = it.urls();
        for (String u : urls) if (UrlMatch.matches(u, url)) return MATCH;
        String d = AppDomains.domain(pkg);
        if (d != null) {
            for (String u : urls) if (UrlMatch.matches(u, "https://" + d)) return APP_DOMAIN;
        }
        StringBuilder hay = new StringBuilder(it.name().toLowerCase(Locale.ROOT));
        for (String u : urls) hay.append('\n').append(UrlMatch.displayHost(u));
        String h = hay.toString();
        for (String w : AppDomains.words(pkg)) if (h.contains(w)) return WORD;
        return OTHER;
    }

    /** Whether a score puts a login among the suggestions shown before "Show all logins". */
    public static boolean suggested(int score) { return score < OTHER; }

    /** The logins sorted for the picker (see {@link #score}). */
    public static List<Item> rank(List<Item> logins, String url, String pkg, String recentId) {
        final Map<String, Integer> s = new HashMap<>();
        for (Item it : logins) s.put(it.id, score(it, url, pkg, recentId));
        List<Item> out = new ArrayList<>(logins);
        Collections.sort(out, new Comparator<Item>() {
            public int compare(Item a, Item b) {
                int c = Integer.compare(s.get(a.id), s.get(b.id));
                return c != 0 ? c : Vault.BY_FAV_NAME.compare(a, b);
            }
        });
        return out;
    }
}
