package app.raypass.core;

import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Collections;
import java.util.Comparator;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.TreeMap;

/**
 * The decrypted vault: the LWW map folded from every op of every entry, the typed items built
 * from it, and the operations that change it. Every change returns the ops to encrypt and append;
 * they are already applied locally. Rules mirror the Go app's vault.State (key validation, field
 * normalization, password history, purge of every key of an item), so both clients write the same
 * shapes. Thread-safe; {@link #wipe} drops all decrypted state on lock.
 */
public final class Vault {
    /** Bad input from a person (safe to show; never contains a secret). */
    public static final class BadInput extends Exception {
        /** Makes one. */
        public BadInput(String m) { super(m); }
    }

    /** Default auto-lock minutes and clipboard-clear seconds; their maxima. */
    public static final int DEFAULT_AUTO_LOCK = 15, DEFAULT_CLIP_CLEAR = 30, MAX_AUTO_LOCK = 7 * 24 * 60, MAX_CLIP_CLEAR = 3600;
    /** Longest value in UTF-8 bytes (JSON-encoded). */
    public static final int MAX_VALUE = 32 << 10;

    private final LwwMap m = new LwwMap();
    private final Clock clk;
    private Map<String, Item> items;

    /** A vault whose writes are stamped by clk. */
    public Vault(Clock clk) { this.clk = clk; }

    /** The clock (its device id is the stamps' d). */
    public Clock clock() { return clk; }

    static boolean validKey(String k) {
        if (k.startsWith("s.")) return k.length() > 2 && k.length() - 2 < 64;
        if (!k.startsWith("i.")) return false;
        String rest = k.substring(2);
        if (rest.length() < 18 || rest.charAt(16) != '.' || !Keyring.validId(rest.substring(0, 16))) return false;
        String f = rest.substring(17);
        if (f.isEmpty() || f.length() > 32) return false;
        for (int i = 0; i < f.length(); i++) {
            char c = f.charAt(i);
            if (!((c >= 'a' && c <= 'z') || c == '_' || (c >= '0' && c <= '9'))) return false;
        }
        return true;
    }

    /** Applies ops from the feed (any order, idempotent); observes every stamp into the clock. */
    public synchronized void apply(List<MapOp> ops) {
        for (MapOp op : ops) {
            if (!validKey(op.key) || op.stamp.d.isEmpty()) continue;
            clk.observe(op.stamp);
            if (m.apply(op)) items = null;
        }
    }

    /** Drops every decrypted value. */
    public synchronized void wipe() {
        m.clear();
        items = null;
    }

    /** The folded map (tests compare it). */
    public LwwMap map() { return m; }

    private void rebuild() {
        if (items != null) return;
        Map<String, Map<String, Object>> vals = new HashMap<>();
        for (String k : m.keys()) {
            if (!k.startsWith("i.") || k.length() < 19) continue;
            String id = k.substring(2, 18), f = k.substring(19);
            Map<String, Object> v = vals.get(id);
            if (v == null) vals.put(id, v = new LinkedHashMap<>());
            v.put(f, m.get(k));
        }
        Map<String, Item> out = new HashMap<>();
        for (Map.Entry<String, Map<String, Object>> e : vals.entrySet()) {
            Object t = e.getValue().get("type");
            if (!(t instanceof String) || ((String) t).isEmpty()) continue;
            out.put(e.getKey(), new Item(e.getKey(), (String) t, e.getValue()));
        }
        items = out;
    }

    /** Every item (trashed included), by id. */
    public synchronized List<Item> items() {
        rebuild();
        List<Item> out = new ArrayList<>(items.values());
        Collections.sort(out, new Comparator<Item>() {
            public int compare(Item a, Item b) { return a.id.compareTo(b.id); }
        });
        return out;
    }

    /** The item with id, or null. */
    public synchronized Item get(String id) {
        rebuild();
        return items.get(id);
    }

    /** Number of items not in the trash / in the trash. */
    public synchronized int[] counts() {
        rebuild();
        int live = 0, trash = 0;
        for (Item it : items.values()) if (it.trashed() == 0) live++; else trash++;
        return new int[]{live, trash};
    }

    /** s.auto_lock in minutes (default 15, 0 = never). */
    public int autoLock() { return setting("s.auto_lock", DEFAULT_AUTO_LOCK, MAX_AUTO_LOCK); }

    /** s.clip_clear in seconds (default 30). */
    public int clipClear() { return setting("s.clip_clear", DEFAULT_CLIP_CLEAR, MAX_CLIP_CLEAR); }

    private int setting(String k, int def, int max) {
        Object v = m.get(k);
        if (v instanceof Long && (Long) v >= 0 && (Long) v <= max) return (int) (long) (Long) v;
        return def;
    }

    /** Ops setting the synced settings (null leaves one unchanged). */
    public synchronized List<MapOp> setSettings(Integer autoLock, Integer clipClear) throws BadInput {
        List<MapOp> ops = new ArrayList<>();
        if (autoLock != null) {
            if (autoLock < 0 || autoLock > MAX_AUTO_LOCK) throw new BadInput("auto_lock must be 0 to " + MAX_AUTO_LOCK + " minutes");
            ops.add(set("s.auto_lock", (long) autoLock));
        }
        if (clipClear != null) {
            if (clipClear < 0 || clipClear > MAX_CLIP_CLEAR) throw new BadInput("clip_clear must be 0 to " + MAX_CLIP_CLEAR + " seconds");
            ops.add(set("s.clip_clear", (long) clipClear));
        }
        return local(ops);
    }

    // ---- writing ----

    private MapOp set(String k, Object v) { return new MapOp(k, v, false, clk.now()); }

    private MapOp del(String k) { return new MapOp(k, null, true, clk.now()); }

    private List<MapOp> local(List<MapOp> ops) {
        for (MapOp op : ops) if (m.apply(op)) items = null;
        return ops;
    }

    private static String kindOf(String type, String f) {
        switch (f) {
            case "name": case "folder": case "notes": return "string";
            case "fav": return "bool";
            case "tags": return "strings";
            case "fields": return "fields";
        }
        if (!Item.typeFields(type).contains(f)) return null;
        if (f.equals("urls")) return "strings";
        if (f.equals("history")) return "history";
        return "string";
    }

    /** Normalizes one field value like the Go app; JSON null passes through as "clear". */
    static Object normalize(String type, String f, Object v) throws BadInput {
        String kind = kindOf(type, f);
        if (kind == null) {
            if (f.equals("created") || f.equals("updated") || f.equals("trashed")) throw new BadInput(f + " cannot be set");
            throw new BadInput("a " + type + " has no field \"" + f + "\"");
        }
        if (v == null) return null;
        if (Json.write(v).getBytes(StandardCharsets.UTF_8).length > MAX_VALUE) throw new BadInput(f + " is too long");
        switch (kind) {
            case "string": {
                if (!(v instanceof String)) throw new BadInput(f + " must be a string");
                String s = (String) v;
                if (f.equals("name") || f.equals("folder")) s = s.replace("\n", " ").replace("\r", "").trim();
                if (f.equals("folder")) {
                    StringBuilder b = new StringBuilder();
                    for (String p : s.split("/")) {
                        p = p.trim();
                        if (p.isEmpty()) continue;
                        if (b.length() > 0) b.append('/');
                        b.append(p);
                    }
                    s = b.toString();
                }
                return s;
            }
            case "bool":
                if (!(v instanceof Boolean)) throw new BadInput(f + " must be true or false");
                return v;
            case "strings": {
                List<Object> a = Json.arr(v);
                if (a == null) throw new BadInput(f + " must be a list of strings");
                LinkedHashSet<String> out = new LinkedHashSet<>();
                for (Object o : a) {
                    if (!(o instanceof String)) throw new BadInput(f + " must be a list of strings");
                    String s = ((String) o).trim();
                    if (!s.isEmpty()) out.add(s);
                }
                return new ArrayList<Object>(out);
            }
            case "fields": {
                List<Object> a = Json.arr(v);
                if (a == null) throw new BadInput(f + " must be a list of {name, value, hidden}");
                List<Object> out = new ArrayList<>();
                for (Object o : a) {
                    Map<String, Object> x = Json.obj(o);
                    if (x == null) throw new BadInput(f + " must be a list of {name, value, hidden}");
                    out.add(Json.map("name", Json.str(x, "name", ""), "value", Json.str(x, "value", ""), "hidden", Json.bool(x, "hidden", false)));
                }
                return out;
            }
            case "history": {
                List<Object> a = Json.arr(v);
                if (a == null) throw new BadInput(f + " must be a list of {pass, until}");
                List<Map<String, Object>> hs = new ArrayList<>();
                for (Object o : a) {
                    Map<String, Object> x = Json.obj(o);
                    if (x == null) throw new BadInput(f + " must be a list of {pass, until}");
                    hs.add(Json.map("pass", Json.str(x, "pass", ""), "until", Json.num(x, "until", 0)));
                }
                Collections.sort(hs, new Comparator<Map<String, Object>>() { // stable, newest first
                    public int compare(Map<String, Object> a, Map<String, Object> b) {
                        return Long.compare(Json.num(b, "until", 0), Json.num(a, "until", 0));
                    }
                });
                return new ArrayList<Object>(hs.subList(0, Math.min(hs.size(), Item.MAX_HISTORY)));
            }
        }
        throw new BadInput(f + " cannot be set");
    }

    private static Map<String, Object> checkFields(String type, Map<String, Object> in) throws BadInput {
        Map<String, Object> out = new TreeMap<>(); // sorted, like Go's sortedKeys
        for (Map.Entry<String, Object> e : in.entrySet()) {
            String f = e.getKey();
            if (f.equals("id") || f.equals("type")) continue;
            out.put(f, normalize(type, f, e.getValue()));
        }
        return out;
    }

    /** Adds an item of type with the given fields (Java values); returns the ops. id is out[0]. */
    public synchronized List<MapOp> add(String type, Map<String, Object> in, long now, String[] idOut) throws BadInput {
        return addWithId(Crypto.randomHex(8), type, in, now, idOut);
    }

    /** Like add with a chosen id (tests, imports). */
    public synchronized List<MapOp> addWithId(String id, String type, Map<String, Object> in, long now, String[] idOut) throws BadInput {
        if (!Item.validType(type)) throw new BadInput("type must be one of login, note, card, identity, wallet");
        if (!Keyring.validId(id)) throw new BadInput("bad id");
        Map<String, Object> vals = checkFields(type, in);
        Object name = vals.get("name");
        if (!(name instanceof String) || ((String) name).isEmpty()) throw new BadInput("an item needs a name");
        String p = "i." + id + ".";
        List<MapOp> ops = new ArrayList<>();
        ops.add(set(p + "type", type));
        ops.add(set(p + "created", now));
        ops.add(set(p + "updated", now));
        ops.add(set(p + "trashed", 0L));
        for (Map.Entry<String, Object> e : vals.entrySet()) if (e.getValue() != null) ops.add(set(p + e.getKey(), e.getValue()));
        if (idOut != null && idOut.length > 0) idOut[0] = id;
        return local(ops);
    }

    /** Sets fields of an item (a null value clears the field); a changed login pass goes to history. */
    public synchronized List<MapOp> update(String id, Map<String, Object> in, long now) throws BadInput {
        Item it = get(id);
        if (it == null) throw new BadInput("no such item");
        if (in.containsKey("type") && !it.type.equals(in.get("type"))) throw new BadInput("the type of an item cannot change");
        Map<String, Object> vals = checkFields(it.type, in);
        if (vals.containsKey("name")) {
            Object n = vals.get("name");
            if (!(n instanceof String) || ((String) n).isEmpty()) throw new BadInput("an item needs a name");
        }
        String p = "i." + id + ".";
        List<MapOp> ops = new ArrayList<>();
        if (vals.containsKey("pass") && it.type.equals(Item.LOGIN)) {
            Object np = vals.get("pass");
            String nps = np instanceof String ? (String) np : "";
            String old = it.str("pass");
            if (!vals.containsKey("history") && !old.isEmpty() && !nps.equals(old)) {
                List<Object> h = new ArrayList<>();
                h.add(Json.map("pass", old, "until", now));
                for (Map<String, Object> x : it.history()) h.add(Json.map("pass", Json.str(x, "pass", ""), "until", Json.num(x, "until", 0)));
                while (h.size() > Item.MAX_HISTORY) h.remove(h.size() - 1);
                ops.add(set(p + "history", h));
            }
        }
        for (Map.Entry<String, Object> e : vals.entrySet()) {
            if (e.getValue() == null) ops.add(del(p + e.getKey()));
            else ops.add(set(p + e.getKey(), e.getValue()));
        }
        ops.add(set(p + "updated", now));
        return local(ops);
    }

    /** Moves items to the trash. */
    public synchronized List<MapOp> trash(List<String> ids, long now) throws BadInput {
        List<MapOp> ops = new ArrayList<>();
        for (String id : new LinkedHashSet<>(ids)) {
            Item it = get(id);
            if (it == null) throw new BadInput("no such item");
            if (it.trashed() == 0) ops.add(set("i." + id + ".trashed", now));
        }
        return local(ops);
    }

    /** Takes items out of the trash. */
    public synchronized List<MapOp> restore(List<String> ids, long now) throws BadInput {
        List<MapOp> ops = new ArrayList<>();
        for (String id : new LinkedHashSet<>(ids)) {
            Item it = get(id);
            if (it == null) throw new BadInput("no such item");
            if (it.trashed() != 0) {
                ops.add(set("i." + id + ".trashed", 0L));
                ops.add(set("i." + id + ".updated", now));
            }
        }
        return local(ops);
    }

    /** Deletes every key of trashed items (tombstones). */
    public synchronized List<MapOp> purge(List<String> ids) throws BadInput {
        for (String id : ids) {
            Item it = get(id);
            if (it == null) throw new BadInput("no such item");
            if (it.trashed() == 0) throw new BadInput("only items in the trash can be purged");
        }
        List<MapOp> ops = new ArrayList<>();
        List<String> keys = m.keys();
        for (String id : new LinkedHashSet<>(ids)) {
            String p = "i." + id + ".";
            for (String k : keys) if (k.startsWith(p)) ops.add(del(k));
        }
        return local(ops);
    }

    // ---- reading ----

    /** List filter: which type ("" = all), trash or not, favourites only, a search query. */
    public synchronized List<Item> list(String query, String type, boolean trash, boolean favOnly) {
        String q = query == null ? "" : query.trim().toLowerCase(Locale.ROOT);
        List<Item> out = new ArrayList<>();
        for (Item it : items()) {
            if ((it.trashed() != 0) != trash) continue;
            if (type != null && !type.isEmpty() && !type.equals(it.type)) continue;
            if (favOnly && !it.fav()) continue;
            if (!q.isEmpty() && !matchesQuery(it, q)) continue;
            out.add(it);
        }
        Collections.sort(out, BY_FAV_NAME);
        return out;
    }

    /** Sorted: favourites, then name (case-insensitive), then id. */
    public static final Comparator<Item> BY_FAV_NAME = new Comparator<Item>() {
        public int compare(Item a, Item b) {
            if (a.fav() != b.fav()) return a.fav() ? -1 : 1;
            int c = a.name().toLowerCase(Locale.ROOT).compareTo(b.name().toLowerCase(Locale.ROOT));
            return c != 0 ? c : a.id.compareTo(b.id);
        }
    };

    private static boolean matchesQuery(Item it, String q) {
        StringBuilder hay = new StringBuilder(it.name()).append('\n').append(it.str("folder")).append('\n').append(it.str("user"))
                .append('\n').append(it.str("email")).append('\n').append(it.str("network")).append('\n');
        for (String u : it.urls()) hay.append(u).append('\n');
        for (String t : it.strings("tags")) hay.append(t).append('\n');
        String h = hay.toString().toLowerCase(Locale.ROOT);
        for (String w : q.split("\\s+")) if (!h.contains(w)) return false;
        return true;
    }

    /** Folder names in use (not trashed) with counts, sorted. */
    public synchronized Map<String, Integer> folders() {
        Map<String, Integer> out = new TreeMap<>();
        for (Item it : items()) {
            if (it.trashed() != 0 || it.str("folder").isEmpty()) continue;
            Integer n = out.get(it.str("folder"));
            out.put(it.str("folder"), n == null ? 1 : n + 1);
        }
        return out;
    }

    /** Logins (not trashed) with a URL matching url, best match first (UrlMatch rank, then name). */
    public synchronized List<Item> match(String url) {
        final Map<String, Integer> rank = new HashMap<>();
        List<Item> out = new ArrayList<>();
        for (Item it : items()) {
            if (!Item.LOGIN.equals(it.type) || it.trashed() != 0) continue;
            int best = -1;
            for (String u : it.urls()) {
                int r = UrlMatch.rank(u, url);
                if (r >= 0 && (best < 0 || r < best)) best = r;
            }
            if (best >= 0) {
                rank.put(it.id, best);
                out.add(it);
            }
        }
        Collections.sort(out, new Comparator<Item>() {
            public int compare(Item a, Item b) {
                int c = Integer.compare(rank.get(a.id), rank.get(b.id));
                return c != 0 ? c : BY_FAV_NAME.compare(a, b);
            }
        });
        return out;
    }

    /** Live (not trashed) logins, favourites first, then by name: what the autofill picker lists. */
    public synchronized List<Item> logins() { return list("", Item.LOGIN, false, false); }

    /**
     * Links a login to an app or site the person picked it for ("Remember for X" in the autofill
     * picker): adds the canonical url to the login's urls, so the next fill there is automatic.
     * Empty ops when one of its URLs already matches url exactly (same host or app).
     */
    public synchronized List<MapOp> rememberUrl(String id, String url, long now) throws BadInput {
        Item it = get(id);
        if (it == null || !Item.LOGIN.equals(it.type) || it.trashed() != 0) throw new BadInput("no such login");
        if (UrlMatch.parse(url) == null) throw new BadInput("url is not a URL");
        List<Object> urls = new ArrayList<>();
        for (String u : it.urls()) {
            if (UrlMatch.rank(u, url) == 0) return new ArrayList<>();
            urls.add(u);
        }
        urls.add(UrlMatch.canonical(url));
        Map<String, Object> in = new LinkedHashMap<>();
        in.put("urls", urls);
        return update(id, in, now);
    }

    /**
     * raypass.save_login: updates the pass of an existing login with the same user and a matching
     * URL (history kept), else adds a login named after the host. out[0] = id, out[1] = "true"
     * when created. Returns the ops (empty when nothing changed).
     */
    public synchronized List<MapOp> saveLogin(String url, String user, String pass, String name, long now, String[] out) throws BadInput {
        if (url == null || url.trim().isEmpty()) throw new BadInput("url is required");
        if (pass == null || pass.isEmpty()) throw new BadInput("pass is required");
        if (user == null) user = "";
        for (Item it : match(url)) {
            if (!it.str("user").equals(user)) continue;
            out[0] = it.id;
            out[1] = "false";
            if (it.str("pass").equals(pass)) return new ArrayList<>();
            Map<String, Object> in = new LinkedHashMap<>();
            in.put("pass", pass);
            return update(it.id, in, now);
        }
        String n = name == null || name.trim().isEmpty() ? UrlMatch.displayHost(url) : name;
        Map<String, Object> in = new LinkedHashMap<>();
        in.put("name", n.isEmpty() ? url : n);
        if (!user.isEmpty()) in.put("user", user);
        in.put("pass", pass);
        List<Object> urls = new ArrayList<>();
        urls.add(UrlMatch.canonical(url));
        in.put("urls", urls);
        String[] id = new String[1];
        List<MapOp> ops = add(Item.LOGIN, in, now, id);
        out[0] = id[0];
        out[1] = "true";
        return ops;
    }
}
