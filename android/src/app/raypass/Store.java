package app.raypass;

import app.raypass.core.Json;
import app.raypass.core.PosSet;

import java.io.BufferedReader;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * The app-private cache of both feeds, so Raypass opens (and unlocks) offline: one JSON line per
 * entry, {@code {"pos":N,"body":{...}}}, in {@code files/feeds/<feed>.jsonl}, plus an outbox of
 * bodies written while Sol was unreachable. Only ciphertext and keyring entries are ever written
 * here (the bodies Sol stores), never a decrypted value. Lines are appended and flushed with
 * fsync; a torn last line (crash mid-write) is skipped on load. The outbox is rewritten atomically
 * (temp file + rename). Not thread-safe on its own: {@link Repo} serializes access.
 */
final class Store {
    static final String KEYS = "keys", VAULT = "vault";

    /** A cached entry. */
    static final class Entry {
        final long pos;
        final Object body;

        Entry(long pos, Object body) {
            this.pos = pos;
            this.body = body;
        }
    }

    /** A body waiting to be appended. */
    static final class Pending {
        final String id, feed;
        final Map<String, Object> body;

        Pending(String id, String feed, Map<String, Object> body) {
            this.id = id;
            this.feed = feed;
            this.body = body;
        }
    }

    private final File dir;
    private final PosSet keys = new PosSet(), vault = new PosSet();
    private final List<Pending> outbox = new ArrayList<>();

    Store(File filesDir) {
        dir = new File(filesDir, "feeds");
        if (!dir.isDirectory() && !dir.mkdirs()) throw new IllegalStateException("cannot create the cache directory");
        dir.setReadable(false, false);
        dir.setReadable(true, true);
        dir.setExecutable(false, false);
        dir.setExecutable(true, true);
    }

    private File file(String feed) { return new File(dir, feed + ".jsonl"); }

    /** Loads a feed's cached entries in pos order of arrival. */
    List<Entry> load(String feed) {
        List<Entry> out = new ArrayList<>();
        File f = file(feed);
        if (!f.exists()) return out;
        try (BufferedReader r = new BufferedReader(new InputStreamReader(new FileInputStream(f), StandardCharsets.UTF_8))) {
            for (String line; (line = r.readLine()) != null; ) {
                try {
                    Map<String, Object> m = Json.obj(Json.parse(line));
                    long pos = Json.num(m, "pos", 0);
                    if (!seen(feed).add(pos)) continue;
                    out.add(new Entry(pos, m.get("body")));
                } catch (IllegalArgumentException torn) {
                    // A line cut short by a crash: the entry is read again from Sol.
                }
            }
        } catch (IOException e) {
            // Unreadable cache: start from Sol.
        }
        return out;
    }

    private PosSet seen(String feed) { return feed.equals(KEYS) ? keys : vault; }

    /**
     * The largest pos p such that every pos 1..p is cached: catch-up reads from p+1 (see
     * {@link PosSet}; the largest pos alone could hide a gap that would never be read).
     */
    long contiguous(String feed) { return seen(feed).contiguous(); }

    /** Appends an entry to the cache; false if it was already there. */
    boolean add(String feed, long pos, Object body) throws IOException {
        return !addAll(feed, java.util.Collections.singletonList(new Entry(pos, body))).isEmpty();
    }

    /**
     * Appends the entries not cached yet with one fsync (a first sync can bring thousands; one
     * fsync each would take seconds); returns those that were new.
     */
    List<Entry> addAll(String feed, List<Entry> es) throws IOException {
        List<Entry> fresh = new ArrayList<>();
        Set<Long> batch = new HashSet<>();
        StringBuilder b = new StringBuilder();
        for (Entry e : es) {
            if (e.pos <= 0 || seen(feed).has(e.pos) || !batch.add(e.pos)) continue;
            b.append(Json.write(Json.map("pos", e.pos, "body", e.body))).append('\n');
            fresh.add(e);
        }
        if (fresh.isEmpty()) return fresh;
        try (FileOutputStream o = new FileOutputStream(file(feed), true)) {
            o.write(b.toString().getBytes(StandardCharsets.UTF_8));
            o.getFD().sync();
        }
        for (Entry e : fresh) seen(feed).add(e.pos);
        return fresh;
    }

    // ---- outbox ----

    private File outboxFile() { return new File(dir, "outbox.jsonl"); }

    /** Loads the outbox. */
    List<Pending> loadOutbox() {
        outbox.clear();
        File f = outboxFile();
        if (f.exists()) {
            try (BufferedReader r = new BufferedReader(new InputStreamReader(new FileInputStream(f), StandardCharsets.UTF_8))) {
                for (String line; (line = r.readLine()) != null; ) {
                    try {
                        Map<String, Object> m = Json.obj(Json.parse(line));
                        Map<String, Object> body = Json.obj(m.get("body"));
                        if (body != null) outbox.add(new Pending(Json.str(m, "id", ""), Json.str(m, "feed", VAULT), body));
                    } catch (IllegalArgumentException torn) {
                        // skipped
                    }
                }
            } catch (IOException e) {
                // nothing pending we can read
            }
        }
        return new ArrayList<>(outbox);
    }

    List<Pending> outbox() { return new ArrayList<>(outbox); }

    void enqueue(Pending p) throws IOException {
        outbox.add(p);
        writeOutbox();
    }

    void dequeue(Pending p) throws IOException {
        outbox.remove(p);
        writeOutbox();
    }

    private void writeOutbox() throws IOException {
        File tmp = new File(dir, "outbox.jsonl.tmp");
        StringBuilder b = new StringBuilder();
        for (Pending p : outbox) b.append(Json.write(Json.map("id", p.id, "feed", p.feed, "body", p.body))).append('\n');
        try (FileOutputStream o = new FileOutputStream(tmp)) {
            o.write(b.toString().getBytes(StandardCharsets.UTF_8));
            o.getFD().sync();
        }
        if (!tmp.renameTo(outboxFile())) throw new IOException("cannot replace the outbox");
    }

    /** Deletes the cache and the outbox (disconnecting from a different Sol person). */
    void clear() {
        clearFeeds();
        for (String f : new String[]{"outbox.jsonl", "outbox.jsonl.tmp"}) new File(dir, f).delete();
        outbox.clear();
    }

    /**
     * Deletes the cached feeds but keeps the outbox: Sol's pos numbers belong to one Sol install,
     * so when Sol's device changes (Sol reinstalled or reset) the cached ones mean nothing.
     */
    void clearFeeds() {
        for (String f : new String[]{KEYS + ".jsonl", VAULT + ".jsonl"}) new File(dir, f).delete();
        keys.clear();
        vault.clear();
    }
}
