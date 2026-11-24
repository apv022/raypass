package app.raypass.core;

/**
 * The SDK's crdt.Clock: issues stamps for one device, each greater than every stamp issued or
 * observed. Every stamp applied from the feed must be {@link #observe observed}, so this device's
 * next write wins over everything it has seen (DESIGN.md "Vault entries"). Thread-safe.
 */
public final class Clock {
    /** The wall clock in ms; tests replace it. */
    public interface Wall { long now(); }

    private final String device;
    private final Wall wall;
    private long w;
    private long c;

    /** A clock for device using System.currentTimeMillis. */
    public Clock(String device) {
        this(device, new Wall() { public long now() { return System.currentTimeMillis(); } });
    }

    /** A clock for device with the given wall clock. */
    public Clock(String device, Wall wall) {
        this.device = device;
        this.wall = wall;
    }

    /** The device id stamps carry. */
    public String device() { return device; }

    /** A stamp greater than every stamp issued or observed so far. */
    public synchronized Stamp now() {
        long t = wall.now();
        if (t > w) {
            w = t;
            c = 0;
        } else {
            c++;
        }
        return new Stamp(w, c, device);
    }

    /** Folds a stamp seen elsewhere into the clock. */
    public synchronized void observe(Stamp s) {
        if (s.w > w || (s.w == w && s.c > c)) {
            w = s.w;
            c = s.c;
        }
    }
}
