package app.raypass.core;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * The last-writer-wins map, the SDK's crdt.Map: applying ops is idempotent and commutative, so
 * every device folds the vault feed (in any order, any number of times) into the same state.
 * Deletes are tombstones and resolve against concurrent sets by stamp like any two writes.
 */
public final class LwwMap {
    private static final class Cell {
        Object value;
        Stamp stamp = Stamp.ZERO;
        boolean deleted;
    }

    private final Map<String, Cell> cells = new HashMap<>();

    /** Applies op; returns whether the state changed. A stamp equal to the current one is a no-op. */
    public synchronized boolean apply(MapOp op) {
        Cell c = cells.get(op.key);
        if (c == null) {
            c = new Cell();
            cells.put(op.key, c);
        }
        if (!c.stamp.less(op.stamp)) return false;
        c.stamp = op.stamp;
        c.value = op.delete ? null : op.value;
        c.deleted = op.delete;
        return true;
    }

    /** Reports whether key is live. */
    public synchronized boolean has(String key) {
        Cell c = cells.get(key);
        return c != null && !c.deleted;
    }

    /** The value of key, or null if absent or deleted. */
    public synchronized Object get(String key) {
        Cell c = cells.get(key);
        return c == null || c.deleted ? null : c.value;
    }

    /** The stamp of the winning write of key (zero if never written). */
    public synchronized Stamp stamp(String key) {
        Cell c = cells.get(key);
        return c == null ? Stamp.ZERO : c.stamp;
    }

    /** The live keys, sorted. */
    public synchronized List<String> keys() {
        List<String> ks = new ArrayList<>();
        for (Map.Entry<String, Cell> e : cells.entrySet()) if (!e.getValue().deleted) ks.add(e.getKey());
        Collections.sort(ks);
        return ks;
    }

    /** Drops everything (on lock: decrypted state must not outlive the key). */
    public synchronized void clear() { cells.clear(); }

    /** Reports whether two maps have the same live content (values compared as JSON). */
    public boolean sameContent(LwwMap o) {
        List<String> a = keys(), b = o.keys();
        if (!a.equals(b)) return false;
        for (String k : a) if (!Json.write(get(k)).equals(Json.write(o.get(k)))) return false;
        return true;
    }
}
