package app.raypass.core;

import java.util.Map;

/**
 * A hybrid logical clock timestamp, the SDK's crdt.Stamp: {@code {"w": ms, "c": counter, "d":
 * device}}. Stamps order by wall, then counter, then device id (string compare; device ids are
 * ASCII, so Java's UTF-16 order equals Go's byte order). The zero stamp sorts before everything.
 */
public final class Stamp implements Comparable<Stamp> {
    /** The zero stamp. */
    public static final Stamp ZERO = new Stamp(0, 0, "");

    /** Wall clock milliseconds. */
    public final long w;
    /** Counter within the same millisecond (uint32 in Go). */
    public final long c;
    /** Device id, the final tie-break. */
    public final String d;

    /** Makes a stamp. */
    public Stamp(long w, long c, String d) {
        this.w = w;
        this.c = c;
        this.d = d == null ? "" : d;
    }

    @Override
    public int compareTo(Stamp o) {
        if (w != o.w) return w < o.w ? -1 : 1;
        if (c != o.c) return c < o.c ? -1 : 1;
        int x = d.compareTo(o.d);
        return x < 0 ? -1 : x > 0 ? 1 : 0;
    }

    /** Reports this &lt; o. */
    public boolean less(Stamp o) { return compareTo(o) < 0; }

    @Override public boolean equals(Object o) { return o instanceof Stamp && compareTo((Stamp) o) == 0; }
    @Override public int hashCode() { return (int) (w * 31 + c) * 31 + d.hashCode(); }
    @Override public String toString() { return w + "." + c + "." + d; }

    /** The JSON object form, keys in Go's order (w, c, d). */
    public Map<String, Object> toJson() { return Json.map("w", w, "c", c, "d", d); }

    /** Parses the JSON object form; missing fields are zero. */
    public static Stamp fromJson(Object v) {
        Map<String, Object> m = Json.obj(v);
        if (m == null) return ZERO;
        return new Stamp(Json.num(m, "w", 0), Json.num(m, "c", 0), Json.str(m, "d", ""));
    }
}
