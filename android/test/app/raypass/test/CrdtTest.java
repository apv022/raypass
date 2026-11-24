package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.Clock;
import app.raypass.core.LwwMap;
import app.raypass.core.MapOp;
import app.raypass.core.Stamp;

import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.Random;

/** LWW map fold and the HLC, against the SDK's crdt semantics. */
final class CrdtTest {
    static void register() {
        test("stamp order: wall, then counter, then device", new T.Case() { public void run() {
            check(new Stamp(1, 9, "z").less(new Stamp(2, 0, "a")), "wall");
            check(new Stamp(2, 0, "z").less(new Stamp(2, 1, "a")), "counter");
            check(new Stamp(2, 1, "a").less(new Stamp(2, 1, "b")), "device");
            check(Stamp.ZERO.less(new Stamp(0, 0, "a")), "zero first");
        }});
        test("fold is order independent and idempotent", new T.Case() { public void run() {
            List<MapOp> ops = new ArrayList<>();
            Random r = new Random(7);
            String[] devs = {"a", "b", "c"};
            for (int i = 0; i < 400; i++) {
                String k = "k" + r.nextInt(12);
                Stamp s = new Stamp(r.nextInt(50), r.nextInt(3), devs[r.nextInt(3)] + i); // stamps are unique per op, as in a real feed
                ops.add(r.nextInt(4) == 0 ? new MapOp(k, null, true, s) : new MapOp(k, (long) r.nextInt(1000), false, s));
            }
            LwwMap a = new LwwMap();
            for (MapOp o : ops) a.apply(o);
            for (int round = 0; round < 5; round++) {
                List<MapOp> sh = new ArrayList<>(ops);
                Collections.shuffle(sh, new Random(round));
                sh.addAll(ops.subList(0, 100)); // duplicates
                LwwMap b = new LwwMap();
                for (MapOp o : sh) b.apply(o);
                check(a.sameContent(b), "same state, round " + round);
            }
        }});
        test("delete and concurrent set resolve by stamp", new T.Case() { public void run() {
            LwwMap m = new LwwMap();
            m.apply(new MapOp("x", "v1", false, new Stamp(1, 0, "a")));
            m.apply(new MapOp("x", null, true, new Stamp(2, 0, "b")));
            check(!m.has("x"), "deleted");
            m.apply(new MapOp("x", "v0", false, new Stamp(1, 5, "c")));
            check(!m.has("x"), "older set loses to the tombstone");
            m.apply(new MapOp("x", "v3", false, new Stamp(3, 0, "a")));
            eq("v3", m.get("x"), "newer set wins");
            check(!m.apply(new MapOp("x", "dup", false, new Stamp(3, 0, "a"))), "equal stamp is a no-op");
        }});
        test("clock: now is after everything observed", new T.Case() { public void run() {
            final long[] wall = {100};
            Clock c = new Clock("me", new Clock.Wall() { public long now() { return wall[0]; } });
            Stamp s1 = c.now();
            c.observe(new Stamp(500, 7, "other"));
            Stamp s2 = c.now();
            check(new Stamp(500, 7, "other").less(s2), "after observed");
            check(s1.less(s2), "monotonic");
            eq(500L, s2.w, "wall from observed");
            eq(8L, s2.c, "counter bumps");
            wall[0] = 900;
            Stamp s3 = c.now();
            eq(900L, s3.w, "real time again");
            eq(0L, s3.c, "counter reset");
        }});
        test("map op JSON matches the SDK shape", new T.Case() { public void run() {
            eq("{\"key\":\"k\",\"value\":\"v\",\"stamp\":{\"w\":1,\"c\":2,\"d\":\"d\"}}",
                    app.raypass.core.Json.write(new MapOp("k", "v", false, new Stamp(1, 2, "d")).toJson()), "set");
            eq("{\"key\":\"k\",\"delete\":true,\"stamp\":{\"w\":1,\"c\":0,\"d\":\"d\"}}",
                    app.raypass.core.Json.write(new MapOp("k", "v", true, new Stamp(1, 0, "d")).toJson()), "delete");
        }});
    }
}
