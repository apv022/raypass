package app.raypass.core;

import java.util.HashMap;
import java.util.Iterator;
import java.util.Map;

/**
 * Which login was filled into an app or page a moment ago, for sign-ins spread over several
 * screens (X asks for the username, then the password on a later screen): when the password
 * screen of the same app comes within {@link #WINDOW_MS}, that login is offered first.
 *
 * Holds item ids only (no secret), in memory, and is cleared when the vault locks.
 */
public final class RecentFills {
    private RecentFills() {}

    /** How long a fill counts as part of the same sign-in. */
    public static final long WINDOW_MS = 5 * 60_000L;

    private static final int MAX = 32;
    private static final Map<String, String> ids = new HashMap<>();
    private static final Map<String, Long> times = new HashMap<>();

    /** Records that itemId was filled for url at nowMs (a monotonic clock). */
    public static synchronized void record(String url, String itemId, long nowMs) {
        if (url == null || itemId == null) return;
        Long old = times.get(url);
        if (old != null && old > nowMs) return; // an older event seen late does not replace a newer one
        expire(nowMs);
        if (ids.size() >= MAX && !ids.containsKey(url)) return;
        ids.put(url, itemId);
        times.put(url, nowMs);
    }

    /** The login filled for url within the window before nowMs, or null. */
    public static synchronized String recent(String url, long nowMs) {
        if (url == null) return null;
        Long t = times.get(url);
        if (t == null || nowMs - t > WINDOW_MS || nowMs < t) return null;
        return ids.get(url);
    }

    /** Forgets everything (on lock). */
    public static synchronized void clear() {
        ids.clear();
        times.clear();
    }

    private static void expire(long nowMs) {
        for (Iterator<Map.Entry<String, Long>> i = times.entrySet().iterator(); i.hasNext(); ) {
            Map.Entry<String, Long> e = i.next();
            if (nowMs - e.getValue() > WINDOW_MS) {
                ids.remove(e.getKey());
                i.remove();
            }
        }
    }
}
