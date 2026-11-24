package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.Json;

import java.util.List;
import java.util.Map;

/** The JSON reader/writer. */
final class JsonTest {
    static void register() {
        test("json: round trip of nested values", new T.Case() { public void run() {
            String s = "{\"a\":[1,2.5,true,false,null,\"x\"],\"b\":{\"c\":\"\\u00e9\\n\\\"\"},\"n\":-12}";
            Object v = Json.parse(s);
            Map<String, Object> m = Json.obj(v);
            eq(-12L, m.get("n"), "long");
            List<Object> a = Json.arr(m.get("a"));
            eq(2.5, a.get(1), "double");
            eq("é\n\"", Json.str(Json.obj(m.get("b")), "c", null), "escapes");
            eq("{\"a\":[1,2.5,true,false,null,\"x\"],\"b\":{\"c\":\"é\\n\\\"\"},\"n\":-12}", Json.write(v), "write");
        }});
        test("json: writer escapes like Go (<, >, &, U+2028, control chars)", new T.Case() { public void run() {
            eq("\"\\u003ca\\u0026b\\u003e\\u2028\\u0001\"", Json.write("<a&b> \u0001"), "escape");
            eq("\"🔑\"", Json.write("🔑"), "astral kept raw");
            eq("\"�\"", Json.write("\ud800"), "lone surrogate");
        }});
        test("json: rejects malformed input without echoing it", new T.Case() { public void run() {
            for (final String bad : new String[]{"", "{", "[1,]", "{\"a\" 1}", "tru", "\"abc", "1 2", "{\"a\":1,}"}) {
                try {
                    Json.parse(bad);
                    fail("accepted malformed input #" + bad.length());
                } catch (IllegalArgumentException e) {
                    check(e.getMessage().startsWith("json: syntax error at offset"), "message");
                }
            }
        }});
    }
}
