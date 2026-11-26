package app.raypass;

import android.content.Context;
import android.content.SharedPreferences;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.util.Log;

import app.raypass.core.Clock;
import app.raypass.core.Crypto;
import app.raypass.core.Json;
import app.raypass.core.MapOp;
import app.raypass.core.RecentFills;
import app.raypass.core.Session;
import app.raypass.core.Vault;

import java.io.IOException;
import java.security.GeneralSecurityException;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/**
 * The one place that ties Raypass together in this process: the Sol connection (token and api in
 * private prefs), the ciphertext cache, the {@link Session} (lock state, vault key, folded vault),
 * the sync thread (events stream with Last-Event-ID, catch-up reads, the outbox) and auto-lock.
 *
 * Sync: the events stream is opened first, then both feeds are read from the cached pos, then
 * events are handled; entries are deduplicated by pos, so nothing is missed or applied twice. When
 * the stream ends it reconnects with a growing delay (the SDK's behaviour); there is no polling.
 * Writes are encrypted, put in the outbox (so they survive being offline and process death) and
 * appended in order. Logs carry no secrets: no bodies, passwords or tokens.
 */
final class Repo {
    private static final String TAG = "Raypass";
    private static volatile Repo instance;

    /** Something changed (vault, lock state, connection); called on the main thread. */
    interface Listener { void changed(); }

    /** Vault states the UI routes on. */
    enum State { NOT_CONNECTED, WAITING, SETUP, LOCKED, UNLOCKED }

    static Repo get(Context c) {
        Repo r = instance;
        if (r == null) {
            synchronized (Repo.class) {
                if (instance == null) instance = new Repo(c.getApplicationContext());
                r = instance;
            }
        }
        return r;
    }

    final Context app;
    private final SharedPreferences prefs;
    private final Store store;
    private final Object storeLock = new Object();
    private volatile Session session;
    private final Handler main = new Handler(Looper.getMainLooper());
    private final List<Listener> listeners = new CopyOnWriteArrayList<>();
    final ExecutorService bg = Executors.newCachedThreadPool();
    private final Object flushLock = new Object();

    private volatile SolApi api;
    private volatile Thread syncThread;
    private volatile boolean online;
    private volatile String connError = "";
    private volatile boolean syncedOnce;
    private final Object wake = new Object();
    private int failures;
    /** The current events request got its answer (so a later 400 is not about Last-Event-ID). */
    private volatile boolean eventsOpened;
    /** When the vault locks itself, in elapsedRealtime ms (counts deep sleep); 0 = never. */
    private volatile long lockAt;

    private Repo(Context app) {
        this.app = app;
        prefs = app.getSharedPreferences("sol", Context.MODE_PRIVATE);
        store = new Store(app.getFilesDir());
        syncedOnce = prefs.getBoolean("synced_once", false);
        failures = prefs.getInt("unlock_failures", 0);
        session = newSession();
        loadCache();
        String a = prefs.getString("api", null), t = prefs.getString("token", null);
        if (a != null && t != null) api = new SolApi(a, t);
    }

    private Session newSession() {
        String dev = prefs.getString("device_id", null);
        if (dev == null) {
            // Stamps need a writer id unique to this install; it is not a secret.
            dev = "android-" + Crypto.randomHex(8);
            prefs.edit().putString("device_id", dev).apply();
        }
        return new Session(new Clock(dev));
    }

    private void loadCache() {
        synchronized (storeLock) {
            for (Store.Entry e : store.load(Store.KEYS)) session.addKeysBody(e.body);
            for (Store.Entry e : store.load(Store.VAULT)) session.addVaultBody(e.body);
            for (Store.Pending p : store.loadOutbox()) {
                if (p.feed.equals(Store.KEYS)) session.addKeysBody(p.body);
                else session.addVaultBody(p.body);
            }
        }
    }

    // ---- listeners ----

    void addListener(Listener l) { listeners.add(l); }

    void removeListener(Listener l) { listeners.remove(l); }

    private final Runnable fire = new Runnable() {
        public void run() { for (Listener l : listeners) l.changed(); }
    };

    void changed() {
        main.removeCallbacks(fire);
        main.post(fire);
    }

    // ---- state ----

    boolean connected() { return api != null; }

    boolean online() { return online; }

    /** The last connection problem, for a banner ("" when fine). */
    String connError() { return connError; }

    Session session() { return session; }

    /** The unlocked vault, or null (locks first when the auto-lock time has passed). */
    Vault vault() {
        checkAutoLock();
        return session.vault();
    }

    State state() {
        checkAutoLock();
        Session s = session;
        if (s.unlocked()) return State.UNLOCKED;
        if (s.initialized()) return State.LOCKED;
        if (api == null) return State.NOT_CONNECTED;
        return syncedOnce ? State.SETUP : State.WAITING;
    }

    int pending() { synchronized (storeLock) { return store.outbox().size(); } }

    // ---- connection ----

    /** Stores the CONNECT result and starts syncing. A different Sol person resets the cache. */
    void onConnected(String apiUrl, String token) {
        prefs.edit().putString("api", apiUrl).putString("token", token).remove("last_event_id").apply();
        SolApi old = api;
        api = new SolApi(apiUrl, token);
        // The old stream blocks in a read on the old connection: close it, or that thread lingers.
        if (old != null) old.closeStream();
        connError = "";
        restartSync();
    }

    /** Forgets the token (the person revoked us, or chose to disconnect). The cache stays. */
    void disconnect(String why) {
        prefs.edit().remove("token").remove("api").remove("last_event_id").apply();
        SolApi a = api;
        api = null;
        online = false;
        connError = why;
        if (a != null) a.closeStream();
        Thread t = syncThread;
        if (t != null) t.interrupt();
        changed();
    }

    void startSync() {
        if (api == null || syncThread != null) return;
        Thread t = new Thread(new Runnable() { public void run() { syncLoop(); } }, "raypass-sync");
        t.setDaemon(true);
        syncThread = t;
        t.start();
    }

    private void restartSync() {
        Thread t = syncThread;
        syncThread = null;
        SolApi a = api;
        if (t != null) t.interrupt();
        if (a != null) a.closeStream();
        startSync();
    }

    /** Wakes a sync loop that is waiting to reconnect (the app came to the foreground). */
    void nudge() {
        synchronized (wake) { wake.notifyAll(); }
        startSync();
    }

    private void syncLoop() {
        long backoff = 500;
        Thread me = Thread.currentThread();
        while (syncThread == me) {
            final SolApi a = api;
            if (a == null) break;
            try {
                final String person = whoami(a);
                List<String> feeds = Arrays.asList(person + "/raypass/" + Store.KEYS, person + "/raypass/" + Store.VAULT);
                eventsOpened = false;
                a.events(feeds, prefs.getString("last_event_id", null), new SolApi.EventSink() {
                    public void opened() throws IOException {
                        eventsOpened = true; // past the request: a 400 after this is not about Last-Event-ID
                        catchUp(a, person, Store.KEYS);
                        catchUp(a, person, Store.VAULT);
                        if (!syncedOnce) {
                            syncedOnce = true;
                            prefs.edit().putBoolean("synced_once", true).apply();
                        }
                        online = true;
                        connError = "";
                        changed();
                        bg.execute(new Runnable() { public void run() { flush(); } });
                    }

                    public boolean event(SolApi.Event e) throws IOException {
                        if (syncThread != me) return false;
                        if (e.type.equals("entry")) {
                            Map<String, Object> en = Json.obj(Json.parse(e.data));
                            String feed = Json.str(en, "feed", "");
                            String name = feed.endsWith("/" + Store.KEYS) ? Store.KEYS : feed.endsWith("/" + Store.VAULT) ? Store.VAULT : null;
                            if (name != null && feed.startsWith(person + "/raypass/")) accept(name, Json.num(en, "pos", 0), en.get("body"));
                        }
                        if (!e.id.isEmpty()) prefs.edit().putString("last_event_id", e.id).apply();
                        return true;
                    }
                });
                backoff = 500; // the stream ran and ended: reconnect soon
            } catch (SolApi.SolException e) {
                online = false;
                if (e.unauthorized()) {
                    Log.w(TAG, "Sol refused the token (401): connect again");
                    disconnect("Sol no longer accepts Raypass. Connect again.");
                    break;
                }
                if (e.status == 400 && !eventsOpened && prefs.contains("last_event_id")) {
                    // Sol does not understand our resume id (a different Sol version): catch-up
                    // reads cover the gap, so start the stream afresh.
                    prefs.edit().remove("last_event_id").apply();
                }
                connError = e.status == 0 ? "Sol is not reachable" : "Sol: " + e.code;
                Log.i(TAG, "sync: " + e.code);
                changed();
            } catch (IllegalArgumentException e) {
                online = false;
                connError = "Sol sent something unreadable";
                changed();
            } catch (IOException e) {
                online = false;
                connError = "Sol is not reachable";
                changed();
            }
            if (syncThread != me) break;
            synchronized (wake) {
                try {
                    wake.wait(backoff);
                } catch (InterruptedException e) {
                    if (syncThread != me) break;
                }
            }
            backoff = Math.min(backoff * 2, 30_000);
        }
        if (syncThread == me) syncThread = null;
    }

    private String whoami(SolApi a) throws IOException {
        String p = prefs.getString("person", null), d = prefs.getString("sol_device", null);
        Map<String, Object> w = a.whoami();
        String person = Json.str(Json.obj(w.get("person")), "id", "");
        String device = Json.str(Json.obj(w.get("device")), "id", "");
        if (person.isEmpty()) throw new SolApi.SolException(0, "bad_response", "whoami without a person");
        if (p != null && !p.equals(person)) {
            // Connected to another person's Sol: their feeds, not ours. Start over.
            Log.i(TAG, "Sol person changed: clearing the cache");
            synchronized (storeLock) {
                session.lock();
                store.clear();
                session = newSession();
            }
            syncedOnce = false;
            prefs.edit().putBoolean("synced_once", false).remove("last_event_id").apply();
            changed();
        } else if (d != null && !device.isEmpty() && !d.equals(device)) {
            // Same person, another Sol install on this phone: its pos numbers are not ours, so
            // the cache is read again from 1. The outbox (our unsent writes) is kept.
            Log.i(TAG, "Sol device changed: reading the feeds again");
            synchronized (storeLock) {
                session.lock();
                store.clearFeeds();
                session = newSession();
                for (Store.Pending q : store.outbox()) {
                    if (q.feed.equals(Store.KEYS)) session.addKeysBody(q.body);
                    else session.addVaultBody(q.body);
                }
            }
            prefs.edit().remove("last_event_id").apply();
            changed();
        }
        prefs.edit().putString("person", person).putString("sol_device", device).apply();
        return person;
    }

    private void catchUp(SolApi a, String person, String feed) throws IOException {
        long from;
        synchronized (storeLock) { from = store.contiguous(feed) + 1; }
        while (true) {
            List<Map<String, Object>> es = a.read(person + "/raypass/" + feed, from, 1000);
            List<Store.Entry> batch = new ArrayList<>();
            for (Map<String, Object> e : es) {
                long pos = Json.num(e, "pos", 0);
                batch.add(new Store.Entry(pos, e.get("body")));
                from = Math.max(from, pos + 1);
            }
            acceptAll(feed, batch);
            if (es.size() < 1000) return;
        }
    }

    private void accept(String feed, long pos, Object body) throws IOException {
        acceptAll(feed, java.util.Collections.singletonList(new Store.Entry(pos, body)));
    }

    private void acceptAll(String feed, List<Store.Entry> es) throws IOException {
        if (es.isEmpty()) return;
        synchronized (storeLock) {
            List<Store.Entry> fresh = store.addAll(feed, es);
            if (fresh.isEmpty()) return;
            for (Store.Entry e : fresh) {
                if (feed.equals(Store.KEYS)) session.addKeysBody(e.body);
                else session.addVaultBody(e.body);
            }
        }
        changed();
    }

    // ---- writing ----

    /** Appends the outbox in order; stops at the first failure (retried on the next connect). */
    void flush() {
        synchronized (flushLock) {
            SolApi a = api;
            if (a == null) return;
            List<Store.Pending> ps;
            synchronized (storeLock) { ps = store.outbox(); }
            for (Store.Pending p : ps) {
                try {
                    Map<String, Object> e = a.append(p.feed, p.body);
                    synchronized (storeLock) {
                        store.add(p.feed, Json.num(e, "pos", 0), p.body);
                        store.dequeue(p);
                    }
                } catch (SolApi.SolException e) {
                    if (e.unauthorized()) disconnect("Sol no longer accepts Raypass. Connect again.");
                    else Log.i(TAG, "append: " + e.code);
                    break;
                } catch (IOException e) {
                    break;
                }
            }
            changed();
        }
    }

    private void enqueue(String feed, Map<String, Object> body) throws IOException {
        synchronized (storeLock) {
            store.enqueue(new Store.Pending(UUID.randomUUID().toString(), feed, body));
        }
    }

    /** Encrypts ops (already applied to the vault) and queues them for Sol. */
    void commit(List<MapOp> ops) throws GeneralSecurityException, IOException {
        if (ops.isEmpty()) return;
        List<Map<String, Object>> bodies = session.encrypt(ops);
        for (Map<String, Object> b : bodies) {
            session.addVaultBody(b); // kept as ciphertext too, so a lock/unlock refolds it
            enqueue(Store.VAULT, b);
        }
        changed();
        bg.execute(new Runnable() { public void run() { flush(); } });
        touch();
    }

    // ---- lock state ----

    /** Creates the vault (slow: PBKDF2). Refused unless the keys feed was read from Sol first. */
    void setup(String password) throws GeneralSecurityException, IOException {
        if (state() != State.SETUP) throw new GeneralSecurityException("not ready to set up");
        Map<String, Object> kr = session.setup(password, Crypto.DEFAULT_ITER);
        enqueue(Store.KEYS, kr);
        failures = 0;
        changed();
        bg.execute(new Runnable() { public void run() { flush(); } });
        touch();
    }

    /**
     * Unlocks (slow). After 5 failures in a row each try first waits 2^(n-5) s, capped at 60 s, as
     * the desktop app does; the count survives restarts.
     */
    void unlock(String password) throws GeneralSecurityException {
        int n;
        synchronized (this) { n = failures; }
        if (n >= 5) SystemClock.sleep(Math.min(60_000L, 1000L << Math.min(n - 5, 6)));
        try {
            session.unlock(password);
        } catch (Crypto.WrongPasswordException e) {
            synchronized (this) {
                failures++;
                prefs.edit().putInt("unlock_failures", failures).apply();
            }
            throw e;
        }
        synchronized (this) {
            failures = 0;
            prefs.edit().putInt("unlock_failures", 0).apply();
        }
        changed();
        touch();
    }

    /** Unlocks with a vault key unwrapped by the biometric Keystore key. */
    void unlockWithKey(byte[] vk) throws GeneralSecurityException {
        session.unlockWithKey(vk, null);
        changed();
        touch();
    }

    void lock() {
        lockAt = 0;
        session.lock();
        main.removeCallbacks(autoLock);
        expirePendingSaves(Long.MAX_VALUE);
        RecentFills.clear();
        changed();
    }

    /** Re-wraps the vault key; returns when queued (slow: two PBKDF2 runs). */
    void changePassword(String oldPw, String newPw) throws GeneralSecurityException, IOException {
        Map<String, Object> kr = session.changePassword(oldPw, newPw, Crypto.DEFAULT_ITER);
        enqueue(Store.KEYS, kr);
        changed();
        bg.execute(new Runnable() { public void run() { flush(); } });
    }

    // ---- auto-lock ----

    // Handler delays count uptime, which stops in deep sleep: a phone left in a pocket would stay
    // unlocked for hours. So the deadline is kept in elapsedRealtime (which counts sleep) and is
    // checked whenever the vault is asked for; the Handler only locks promptly while awake.
    private final Runnable autoLock = new Runnable() {
        public void run() {
            long left = lockAt - SystemClock.elapsedRealtime();
            if (lockAt != 0 && left > 0) main.postDelayed(this, left);
            else checkAutoLock();
        }
    };

    /** Locks when the auto-lock deadline has passed. */
    private void checkAutoLock() {
        long at = lockAt;
        if (at == 0 || SystemClock.elapsedRealtime() < at) return;
        if (session.unlocked()) {
            Log.i(TAG, "auto-lock");
            lock();
        } else {
            lockAt = 0;
        }
    }

    /** Records activity: the auto-lock timer restarts (s.auto_lock minutes, 0 = never). */
    void touch() {
        checkAutoLock(); // an expired deadline locks; activity after it must not extend it
        main.removeCallbacks(autoLock);
        Vault v = session.vault();
        if (v == null) {
            lockAt = 0;
            return;
        }
        int min = v.autoLock();
        if (min <= 0) {
            lockAt = 0;
            return;
        }
        lockAt = SystemClock.elapsedRealtime() + min * 60_000L;
        main.postDelayed(autoLock, min * 60_000L);
    }

    // ---- autofill save ----

    /** raypass.save_login; returns the item id. */
    String saveLogin(String url, String user, String pass) throws GeneralSecurityException, IOException, Vault.BadInput {
        Vault v = session.vault();
        if (v == null) throw new GeneralSecurityException("locked");
        String[] out = new String[2];
        List<MapOp> ops = v.saveLogin(url, user, pass, null, System.currentTimeMillis(), out);
        commit(ops);
        return out[0];
    }

    /**
     * Pending autofill saves waiting for an unlock, by token (memory only: never on disk):
     * {url, user, pass, created (elapsedRealtime)}. Dropped after 10 minutes or on lock, so a
     * save the person walked away from does not keep a password in memory.
     */
    private static final Map<String, String[]> pendingSaves = new java.util.concurrent.ConcurrentHashMap<>();
    private static final long PENDING_SAVE_MS = 10 * 60_000L;

    static String putPendingSave(String url, String user, String pass) {
        expirePendingSaves(PENDING_SAVE_MS);
        String token = UUID.randomUUID().toString();
        pendingSaves.put(token, new String[]{url, user, pass, String.valueOf(SystemClock.elapsedRealtime())});
        return token;
    }

    /** The pending save for token (removed), or null if none or expired. */
    static String[] takePendingSave(String token) {
        expirePendingSaves(PENDING_SAVE_MS);
        return token == null ? null : pendingSaves.remove(token);
    }

    private static void expirePendingSaves(long maxAge) {
        long now = SystemClock.elapsedRealtime();
        for (Map.Entry<String, String[]> e : pendingSaves.entrySet()) {
            long t;
            try { t = Long.parseLong(e.getValue()[3]); } catch (NumberFormatException x) { t = 0; }
            if (maxAge == Long.MAX_VALUE || now - t > maxAge) pendingSaves.remove(e.getKey());
        }
    }

    static List<String> list(String... s) { return new ArrayList<>(Arrays.asList(s)); }
}
