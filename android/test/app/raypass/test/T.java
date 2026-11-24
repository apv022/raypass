package app.raypass.test;

import java.util.ArrayList;
import java.util.List;

/**
 * A tiny test framework (JUnit is a third-party library): register named cases, run them, count
 * failures. Assertion messages never include secrets; test names are descriptive, not data.
 */
public final class T {
    /** A test body. */
    public interface Case { void run() throws Exception; }

    private static final List<String> names = new ArrayList<>();
    private static final List<Case> cases = new ArrayList<>();

    /** Registers a case. */
    public static void test(String name, Case c) {
        names.add(name);
        cases.add(c);
    }

    /** Fails the current case. */
    public static void fail(String msg) { throw new AssertionError(msg); }

    /** Asserts cond. */
    public static void check(boolean cond, String msg) { if (!cond) fail(msg); }

    /** Asserts equality (values shown: only use with non-secret data). */
    public static void eq(Object want, Object got, String what) {
        if (want == null ? got != null : !want.equals(got)) fail(what + ": want " + want + ", got " + got);
    }

    /** Asserts that c throws an instance of type. */
    public static void throwsA(Class<? extends Throwable> type, Case c, String what) {
        try {
            c.run();
        } catch (Throwable t) {
            if (type.isInstance(t)) return;
            fail(what + ": threw " + t.getClass().getSimpleName() + ", want " + type.getSimpleName());
        }
        fail(what + ": did not throw");
    }

    /** Runs every case; returns the number of failures. */
    public static int runAll() {
        int failed = 0;
        for (int i = 0; i < cases.size(); i++) {
            long t0 = System.nanoTime();
            try {
                cases.get(i).run();
                System.out.printf("ok   %-60s %6.0f ms%n", names.get(i), (System.nanoTime() - t0) / 1e6);
            } catch (Throwable t) {
                failed++;
                System.out.printf("FAIL %s%n     %s%n", names.get(i), t);
                StackTraceElement[] st = t.getStackTrace();
                for (int k = 0; k < Math.min(4, st.length); k++) System.out.println("       at " + st[k]);
            }
        }
        System.out.printf("%d tests, %d failed%n", cases.size(), failed);
        return failed;
    }
}
