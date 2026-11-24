package app.raypass.core;

import java.util.HashSet;
import java.util.Set;

/**
 * The positions of one feed held in the local cache. Sol numbers the entries it stores for a feed
 * 1, 2, 3, … with no gaps, but they reach the cache out of order (the events stream, our own
 * appends, catch-up reads), so besides the set it tracks {@link #contiguous}: the largest p with
 * every position 1..p held. Catch-up reads from there, so a gap left by a dropped stream is read
 * again instead of being hidden behind a larger position. Not thread-safe.
 */
public final class PosSet {
    private final Set<Long> seen = new HashSet<>();
    private long max, contig;

    /** Adds pos; false if it is not positive or already held. */
    public boolean add(long pos) {
        if (pos <= 0 || !seen.add(pos)) return false;
        max = Math.max(max, pos);
        while (seen.contains(contig + 1)) contig++;
        return true;
    }

    /** Reports whether pos is held. */
    public boolean has(long pos) { return seen.contains(pos); }

    /** The largest position held (0 when empty). */
    public long max() { return max; }

    /** The largest p such that every position 1..p is held. */
    public long contiguous() { return contig; }

    /** Forgets every position. */
    public void clear() {
        seen.clear();
        max = contig = 0;
    }
}
