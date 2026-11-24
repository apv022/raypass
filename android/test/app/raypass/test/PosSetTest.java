package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.PosSet;

/** The cache's feed positions: catch-up must restart below any gap. */
final class PosSetTest {
    static void register() {
        test("posset: contiguous stops at a gap and moves when it fills", new T.Case() { public void run() {
            PosSet s = new PosSet();
            check(s.add(1) && s.add(2), "adds");
            check(s.add(5), "an entry from the events stream, past a gap");
            eq(5L, s.max(), "max");
            eq(2L, s.contiguous(), "catch-up restarts at 3, not 6");
            check(!s.add(5) && !s.add(0) && !s.add(-1), "duplicates and non-positive refused");
            s.add(4);
            s.add(3);
            eq(5L, s.contiguous(), "gap filled");
            s.clear();
            eq(0L, s.contiguous(), "cleared");
            check(!s.has(1), "forgotten");
        }});
    }
}
