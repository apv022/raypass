package app.raypass.core;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * One vault item as folded from the map (DESIGN.md "Item model"): its id, type and the decoded
 * value of every field it has. Immutable snapshot; changes go through {@link Vault} as ops.
 */
public final class Item {
    /** Item types. */
    public static final String LOGIN = "login", NOTE = "note", CARD = "card", IDENTITY = "identity", WALLET = "wallet";
    /** All types, in UI order. */
    public static final List<String> TYPES = Collections.unmodifiableList(Arrays.asList(LOGIN, NOTE, CARD, IDENTITY, WALLET));
    /** Fields every type has. */
    public static final List<String> COMMON = Collections.unmodifiableList(Arrays.asList("name", "folder", "fav", "tags", "notes", "fields"));
    /** Most passwords kept in a login's history. */
    public static final int MAX_HISTORY = 20;

    /** The fields of type t beyond the common ones (empty for unknown types). */
    public static List<String> typeFields(String t) {
        if (LOGIN.equals(t)) return Arrays.asList("user", "pass", "urls", "totp", "history");
        if (CARD.equals(t)) return Arrays.asList("holder", "number", "exp", "cvv", "brand");
        if (IDENTITY.equals(t)) return Arrays.asList("first", "last", "email", "phone", "address", "company");
        if (WALLET.equals(t)) return Arrays.asList("seed", "privkey", "address", "network", "passphrase", "derivation");
        return Collections.emptyList();
    }

    /** Reports whether t is a known type. */
    public static boolean validType(String t) { return TYPES.contains(t); }

    /** Fields whose values are secrets (hidden until revealed, copied as sensitive). */
    public static boolean secretField(String f) {
        return f.equals("pass") || f.equals("cvv") || f.equals("number") || f.equals("seed") || f.equals("privkey")
                || f.equals("passphrase") || f.equals("totp");
    }

    /** The 16-hex id. */
    public final String id;
    /** The type. */
    public final String type;
    final Map<String, Object> values;

    Item(String id, String type, Map<String, Object> values) {
        this.id = id;
        this.type = type;
        this.values = values;
    }

    /** A string field, "" if unset. */
    public String str(String f) {
        Object v = values.get(f);
        return v instanceof String ? (String) v : "";
    }

    /** The raw decoded value of a field (null if unset). */
    public Object raw(String f) { return values.get(f); }

    /** The display name ("(no name)" when empty, like the Go app). */
    public String name() {
        String n = str("name");
        return n.isEmpty() ? "(no name)" : n;
    }

    /** Favourite flag. */
    public boolean fav() { return Boolean.TRUE.equals(values.get("fav")); }

    /** Trash time in ms, 0 when not trashed. */
    public long trashed() { return num("trashed"); }

    /** Created / updated time in ms. */
    public long created() { return num("created"); }

    /** Updated time in ms. */
    public long updated() { return num("updated"); }

    private long num(String f) {
        Object v = values.get(f);
        if (v instanceof Long) return (Long) v;
        if (v instanceof Double) return (long) (double) (Double) v;
        return 0;
    }

    /** A string-list field (urls, tags). */
    public List<String> strings(String f) { return Json.strings(values.get(f)); }

    /** The login's URLs. */
    public List<String> urls() { return strings("urls"); }

    /** The custom fields as {name, value, hidden} maps. */
    public List<Map<String, Object>> fields() {
        List<Map<String, Object>> out = new ArrayList<>();
        for (Map<String, Object> m : Json.objects(values.get("fields"))) out.add(m);
        return out;
    }

    /** The password history, newest first, as {pass, until} maps. */
    public List<Map<String, Object>> history() {
        List<Map<String, Object>> out = new ArrayList<>();
        for (Map<String, Object> m : Json.objects(values.get("history"))) out.add(m);
        return out;
    }

    /** A short secondary line for lists (user, card tail, email; never a secret). */
    public String subtitle() {
        switch (type) {
            case LOGIN: return str("user");
            case CARD: {
                String n = str("number").replaceAll("[^0-9]", "");
                String b = str("brand");
                return (b.isEmpty() ? "" : b + " ") + (n.length() >= 4 ? "•••• " + n.substring(n.length() - 4) : "");
            }
            case IDENTITY: {
                String s = (str("first") + " " + str("last")).trim();
                return s.isEmpty() ? str("email") : s;
            }
            case WALLET: return str("network");
            default: return str("folder");
        }
    }

    /** Every field's value as an editable copy (for edit forms and exports). */
    public Map<String, Object> copyValues() { return new LinkedHashMap<>(values); }
}
