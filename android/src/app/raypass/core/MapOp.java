package app.raypass.core;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * One write to the LWW map, the SDK's crdt.MapOp: set {@code key} to {@code value}, or delete it.
 * JSON: {@code {"key", "value"?, "delete"?, "stamp"}} with Go's field order and omitempty rules,
 * so a batch serialized here matches the Go app byte for byte.
 */
public final class MapOp {
    /** The map key, e.g. {@code i.<id>.name}. */
    public final String key;
    /** The value (a Json value) when not a delete; may be JSON null. */
    public final Object value;
    /** True for a tombstone. */
    public final boolean delete;
    /** The op's stamp. */
    public final Stamp stamp;

    /** Makes an op. */
    public MapOp(String key, Object value, boolean delete, Stamp stamp) {
        this.key = key;
        this.value = delete ? null : value;
        this.delete = delete;
        this.stamp = stamp;
    }

    /** The JSON object form. */
    public Map<String, Object> toJson() {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("key", key);
        // Go: Value json.RawMessage `omitempty` (a JSON null is the 4 bytes "null", so it is kept).
        if (!delete) m.put("value", value);
        if (delete) m.put("delete", Boolean.TRUE);
        m.put("stamp", stamp.toJson());
        return m;
    }

    /** Parses one op, or returns null if it is not an op. */
    public static MapOp fromJson(Object v) {
        Map<String, Object> m = Json.obj(v);
        if (m == null || !(m.get("key") instanceof String)) return null;
        return new MapOp((String) m.get("key"), m.get("value"), Json.bool(m, "delete", false), Stamp.fromJson(m.get("stamp")));
    }

    /** The plaintext of an entry: {@code {"ops":[...]}}. */
    public static String batchJson(List<MapOp> ops) {
        List<Object> a = new ArrayList<>();
        for (MapOp o : ops) a.add(o.toJson());
        return Json.write(Json.map("ops", a));
    }

    /** Parses an entry plaintext; malformed ops are skipped. */
    public static List<MapOp> parseBatch(String json) {
        List<MapOp> out = new ArrayList<>();
        Map<String, Object> m = Json.obj(Json.parse(json));
        if (m == null) throw new IllegalArgumentException("batch: not an object");
        List<Object> a = Json.arr(m.get("ops"));
        if (a != null) for (Object o : a) {
            MapOp op = fromJson(o);
            if (op != null) out.add(op);
        }
        return out;
    }
}
