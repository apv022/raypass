package app.raypass.core;

import java.util.ArrayList;
import java.util.Iterator;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * A small JSON reader and writer, so the pure-Java core runs the same on the JDK (tests) and on
 * Android (where org.json exists but the JDK has nothing).
 *
 * Values are {@code null}, {@link Boolean}, {@link Long} (integers that fit), {@link Double},
 * {@link String}, {@link List} and {@link Map} (insertion ordered). The writer escapes strings
 * the way Go's encoding/json does ({@code <}, {@code >}, {@code &}, U+2028, U+2029 as
 * {@code \\uXXXX}), so a batch written here is byte-identical to the same batch written by the Go
 * app when the keys are in the same order. Parse errors are {@link IllegalArgumentException}
 * with a position only, never the input (it may be a decrypted secret).
 */
public final class Json {
    private Json() {}

    /** Parses one JSON value; trailing non-space input is an error. */
    public static Object parse(String s) {
        Parser p = new Parser(s);
        p.ws();
        Object v = p.value(0);
        p.ws();
        if (p.i != s.length()) throw p.err();
        return v;
    }

    /** Serializes v (see the class comment for the accepted types). */
    public static String write(Object v) {
        StringBuilder b = new StringBuilder();
        write(b, v);
        return b.toString();
    }

    /** Appends the JSON of v to b. */
    @SuppressWarnings("unchecked")
    public static void write(StringBuilder b, Object v) {
        if (v == null) {
            b.append("null");
        } else if (v instanceof String) {
            quote(b, (String) v);
        } else if (v instanceof Boolean) {
            b.append(((Boolean) v) ? "true" : "false");
        } else if (v instanceof Double || v instanceof Float) {
            double d = ((Number) v).doubleValue();
            if (Double.isNaN(d) || Double.isInfinite(d)) throw new IllegalArgumentException("json: bad number");
            if (d == Math.rint(d) && Math.abs(d) < 1e15) b.append((long) d);
            else b.append(d);
        } else if (v instanceof Number) {
            b.append(((Number) v).longValue());
        } else if (v instanceof Map) {
            b.append('{');
            boolean first = true;
            for (Map.Entry<String, Object> e : ((Map<String, Object>) v).entrySet()) {
                if (!first) b.append(',');
                first = false;
                quote(b, e.getKey());
                b.append(':');
                write(b, e.getValue());
            }
            b.append('}');
        } else if (v instanceof List) {
            b.append('[');
            boolean first = true;
            for (Object o : (List<Object>) v) {
                if (!first) b.append(',');
                first = false;
                write(b, o);
            }
            b.append(']');
        } else if (v instanceof Raw) {
            b.append(((Raw) v).json);
        } else {
            throw new IllegalArgumentException("json: cannot write " + v.getClass().getName());
        }
    }

    /** Already-encoded JSON, written verbatim (a MapOp value kept as it arrived). */
    public static final class Raw {
        /** The encoded JSON text. */
        public final String json;

        /** Wraps already-valid JSON text. */
        public Raw(String json) { this.json = json; }

        @Override public boolean equals(Object o) { return o instanceof Raw && ((Raw) o).json.equals(json); }
        @Override public int hashCode() { return json.hashCode(); }
        @Override public String toString() { return json; }
    }

    private static final char[] HEX = "0123456789abcdef".toCharArray();

    /** Appends s as a JSON string literal, escaped like Go's encoding/json. */
    public static void quote(StringBuilder b, String s) {
        b.append('"');
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            switch (c) {
                case '"': b.append("\\\""); break;
                case '\\': b.append("\\\\"); break;
                case '\n': b.append("\\n"); break;
                case '\r': b.append("\\r"); break;
                case '\t': b.append("\\t"); break;
                default:
                    if (c < 0x20 || c == '<' || c == '>' || c == '&' || c == ' ' || c == ' ') {
                        b.append("\\u00").append(HEX[(c >> 4) & 0xf]).append(HEX[c & 0xf]);
                        if (c >= 0x100) { // U+2028/9: redo with all four digits
                            b.setLength(b.length() - 6);
                            b.append("\\u").append(HEX[(c >> 12) & 0xf]).append(HEX[(c >> 8) & 0xf])
                                    .append(HEX[(c >> 4) & 0xf]).append(HEX[c & 0xf]);
                        }
                    } else if (Character.isSurrogate(c)) {
                        // Go replaces invalid UTF-16 (a lone surrogate) with U+FFFD.
                        if (Character.isHighSurrogate(c) && i + 1 < s.length() && Character.isLowSurrogate(s.charAt(i + 1))) {
                            b.append(c).append(s.charAt(++i));
                        } else {
                            b.append('�');
                        }
                    } else {
                        b.append(c);
                    }
            }
        }
        b.append('"');
    }

    // ---- typed helpers ----

    /** v as a map, or null. */
    @SuppressWarnings("unchecked")
    public static Map<String, Object> obj(Object v) { return v instanceof Map ? (Map<String, Object>) v : null; }

    /** v as a list, or null. */
    @SuppressWarnings("unchecked")
    public static List<Object> arr(Object v) { return v instanceof List ? (List<Object>) v : null; }

    /** m[k] as a string, or def. */
    public static String str(Map<String, Object> m, String k, String def) {
        Object v = m == null ? null : m.get(k);
        return v instanceof String ? (String) v : def;
    }

    /** m[k] as a long (integers only), or def. */
    public static long num(Map<String, Object> m, String k, long def) {
        Object v = m == null ? null : m.get(k);
        if (v instanceof Long) return (Long) v;
        if (v instanceof Double && ((Double) v) == Math.rint((Double) v)) return (long) (double) (Double) v;
        return def;
    }

    /** m[k] as a bool, or def. */
    public static boolean bool(Map<String, Object> m, String k, boolean def) {
        Object v = m == null ? null : m.get(k);
        return v instanceof Boolean ? (Boolean) v : def;
    }

    /** A new insertion-ordered map from alternating key, value arguments. */
    public static Map<String, Object> map(Object... kv) {
        Map<String, Object> m = new LinkedHashMap<>();
        for (int i = 0; i + 1 < kv.length; i += 2) m.put((String) kv[i], kv[i + 1]);
        return m;
    }

    /** A list of strings from a JSON array value (non-strings skipped); empty for anything else. */
    public static List<String> strings(Object v) {
        List<String> out = new ArrayList<>();
        List<Object> a = arr(v);
        if (a != null) for (Object o : a) if (o instanceof String) out.add((String) o);
        return out;
    }

    private static final class Parser {
        final String s;
        int i;

        Parser(String s) { this.s = s; }

        IllegalArgumentException err() { return new IllegalArgumentException("json: syntax error at offset " + i); }

        void ws() {
            while (i < s.length()) {
                char c = s.charAt(i);
                if (c == ' ' || c == '\n' || c == '\r' || c == '\t') i++;
                else break;
            }
        }

        Object value(int depth) {
            if (depth > 200) throw err();
            if (i >= s.length()) throw err();
            char c = s.charAt(i);
            switch (c) {
                case '{': return object(depth);
                case '[': return array(depth);
                case '"': return string();
                case 't': lit("true"); return Boolean.TRUE;
                case 'f': lit("false"); return Boolean.FALSE;
                case 'n': lit("null"); return null;
                default:
                    if (c == '-' || (c >= '0' && c <= '9')) return number();
                    throw err();
            }
        }

        void lit(String w) {
            if (!s.startsWith(w, i)) throw err();
            i += w.length();
        }

        Map<String, Object> object(int depth) {
            Map<String, Object> m = new LinkedHashMap<>();
            i++;
            ws();
            if (i < s.length() && s.charAt(i) == '}') { i++; return m; }
            while (true) {
                ws();
                if (i >= s.length() || s.charAt(i) != '"') throw err();
                String k = string();
                ws();
                if (i >= s.length() || s.charAt(i) != ':') throw err();
                i++;
                ws();
                m.put(k, value(depth + 1));
                ws();
                if (i >= s.length()) throw err();
                char c = s.charAt(i++);
                if (c == '}') return m;
                if (c != ',') { i--; throw err(); }
            }
        }

        List<Object> array(int depth) {
            List<Object> a = new ArrayList<>();
            i++;
            ws();
            if (i < s.length() && s.charAt(i) == ']') { i++; return a; }
            while (true) {
                ws();
                a.add(value(depth + 1));
                ws();
                if (i >= s.length()) throw err();
                char c = s.charAt(i++);
                if (c == ']') return a;
                if (c != ',') { i--; throw err(); }
            }
        }

        String string() {
            i++; // opening quote
            StringBuilder b = null;
            int start = i;
            while (true) {
                if (i >= s.length()) throw err();
                char c = s.charAt(i);
                if (c == '"') {
                    String out = b == null ? s.substring(start, i) : b.append(s, start, i).toString();
                    i++;
                    return out;
                }
                if (c < 0x20) throw err();
                if (c == '\\') {
                    if (b == null) b = new StringBuilder();
                    b.append(s, start, i);
                    i++;
                    if (i >= s.length()) throw err();
                    char e = s.charAt(i++);
                    switch (e) {
                        case '"': b.append('"'); break;
                        case '\\': b.append('\\'); break;
                        case '/': b.append('/'); break;
                        case 'b': b.append('\b'); break;
                        case 'f': b.append('\f'); break;
                        case 'n': b.append('\n'); break;
                        case 'r': b.append('\r'); break;
                        case 't': b.append('\t'); break;
                        case 'u':
                            if (i + 4 > s.length()) throw err();
                            try {
                                b.append((char) Integer.parseInt(s.substring(i, i + 4), 16));
                            } catch (NumberFormatException x) {
                                throw err();
                            }
                            i += 4;
                            break;
                        default: throw err();
                    }
                    start = i;
                } else {
                    i++;
                }
            }
        }

        Object number() {
            int start = i;
            if (s.charAt(i) == '-') i++;
            boolean frac = false;
            while (i < s.length()) {
                char c = s.charAt(i);
                if (c >= '0' && c <= '9') i++;
                else if (c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-') { frac = true; i++; }
                else break;
            }
            String t = s.substring(start, i);
            try {
                if (!frac) return Long.parseLong(t);
                return Double.parseDouble(t);
            } catch (NumberFormatException e) {
                try {
                    return Double.parseDouble(t);
                } catch (NumberFormatException e2) {
                    throw err();
                }
            }
        }
    }

    /** Iterates a JSON array of objects, skipping non-objects. */
    public static Iterable<Map<String, Object>> objects(Object v) {
        final List<Object> a = arr(v);
        final List<Map<String, Object>> out = new ArrayList<>();
        if (a != null) {
            Iterator<Object> it = a.iterator();
            while (it.hasNext()) {
                Map<String, Object> m = obj(it.next());
                if (m != null) out.add(m);
            }
        }
        return out;
    }
}
