package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.Sse;

import java.util.ArrayList;
import java.util.List;

/** The events-stream parser against what Sol's daemon writes (and the SSE rules around it). */
final class SseTest {
    static List<Sse.Event> feed(Sse p, String... lines) {
        List<Sse.Event> out = new ArrayList<>();
        for (String l : lines) {
            Sse.Event e = p.line(l);
            if (e != null) out.add(e);
        }
        return out;
    }

    static void register() {
        test("sse: Sol's events, heartbeats and ids", new T.Case() { public void run() {
            Sse p = new Sse();
            List<Sse.Event> es = feed(p, ": ok", "", "id: e1.4", "event: entry", "data: {\"pos\":3}", "", ": beat", "",
                    "id: e1.5", "event: theme", "data: {}", "");
            eq(2, es.size(), "two events, comments ignored");
            eq("e1.4", es.get(0).id, "first id");
            eq("entry", es.get(0).type, "first type");
            eq("{\"pos\":3}", es.get(0).data, "first data");
            eq("theme", es.get(1).type, "second type");
            eq("e1.5", p.lastId(), "last id for Last-Event-ID");
        }});
        test("sse: multi-line data, no space, default type, carried id", new T.Case() { public void run() {
            Sse p = new Sse();
            List<Sse.Event> es = feed(p, "id:7", "data:a", "data: b", "data", "", "data: c", "");
            eq(2, es.size(), "two events");
            eq("a\nb\n", es.get(0).data, "lines joined, one leading space dropped, bare data is empty");
            eq("message", es.get(0).type, "default type");
            eq("7", es.get(1).id, "id carries over");
            eq("c", es.get(1).data, "second data");
        }});
        test("sse: an event with no data is not dispatched; type resets", new T.Case() { public void run() {
            Sse p = new Sse();
            List<Sse.Event> es = feed(p, "event: entry", "", "data: x", "");
            eq(1, es.size(), "only the event with data");
            eq("message", es.get(0).type, "type does not leak into the next event");
            feed(p, "id: a\0b", "data: y", "");
            eq("", p.lastId(), "an id with NUL is ignored");
        }});
    }
}
