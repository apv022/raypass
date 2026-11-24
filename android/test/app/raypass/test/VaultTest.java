package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.Clock;
import app.raypass.core.Item;
import app.raypass.core.Json;
import app.raypass.core.MapOp;
import app.raypass.core.Vault;
import app.raypass.core.VaultEntry;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;

/** The item model: add, update with history, trash, restore, purge, list, match, save_login. */
final class VaultTest {
    static List<Object> list(Object... a) { return new ArrayList<>(Arrays.asList(a)); }

    static void register() {
        test("vault: add normalizes fields and refuses bad input", new T.Case() { public void run() throws Exception {
            final Vault v = new Vault(new Clock("d"));
            String[] id = new String[1];
            v.add("login", Json.map("name", "  Git\nHub ", "folder", " Work // Servers/ ", "urls", list("https://github.com", " ", "https://github.com"),
                    "tags", list("a", "a", "b")), 5, id);
            Item it = v.get(id[0]);
            eq("Git Hub", it.name(), "name trimmed");
            eq("Work/Servers", it.str("folder"), "folder cleaned");
            eq(Arrays.asList("https://github.com"), it.urls(), "urls deduped");
            eq(Arrays.asList("a", "b"), it.strings("tags"), "tags");
            eq(0L, it.trashed(), "not trashed");
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.add("login", Json.map("name", ""), 1, null); }}, "no name");
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.add("note", Json.map("name", "x", "pass", "y"), 1, null); }}, "note has no pass");
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.add("bogus", Json.map("name", "x"), 1, null); }}, "type");
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.add("note", Json.map("name", "x", "created", 3L), 1, null); }}, "system field");
        }});

        test("vault: changing a login password keeps history (newest first, max 20)", new T.Case() { public void run() throws Exception {
            Vault v = new Vault(new Clock("d"));
            String[] id = new String[1];
            v.add("login", Json.map("name", "x", "pass", "p0"), 1, id);
            for (int i = 1; i <= 25; i++) v.update(id[0], Json.map("pass", "p" + i), 100 + i);
            Item it = v.get(id[0]);
            eq("p25", it.str("pass"), "pass");
            eq(20, it.history().size(), "capped");
            eq("p24", Json.str(it.history().get(0), "pass", ""), "newest first");
            v.update(id[0], Json.map("pass", "p25", "user", "u"), 200);
            eq(20, v.get(id[0]).history().size(), "same pass adds nothing");
            v.update(id[0], Json.map("user", null), 201);
            check(!v.map().has("i." + id[0] + ".user"), "null clears");
        }});

        test("vault: trash, restore, purge", new T.Case() { public void run() throws Exception {
            final Vault v = new Vault(new Clock("d"));
            final String[] id = new String[1];
            v.add("note", Json.map("name", "n", "notes", "body"), 1, id);
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.purge(Arrays.asList(id[0])); }}, "purge needs trash");
            eq(1, v.trash(Arrays.asList(id[0], id[0]), 5).size(), "trash once");
            eq(1, v.list("", "", true, false).size(), "in trash");
            eq(0, v.list("", "", false, false).size(), "not in list");
            v.restore(Arrays.asList(id[0]), 6);
            eq(0L, v.get(id[0]).trashed(), "restored");
            v.trash(Arrays.asList(id[0]), 7);
            List<MapOp> ops = v.purge(Arrays.asList(id[0]));
            check(ops.size() >= 6, "every key deleted");
            check(v.get(id[0]) == null, "gone");
            check(v.map().keys().isEmpty(), "no live keys");
        }});

        test("vault: list sorts favourites then name and searches", new T.Case() { public void run() throws Exception {
            Vault v = new Vault(new Clock("d"));
            v.add("login", Json.map("name", "beta", "user", "bob@x.org"), 1, null);
            v.add("login", Json.map("name", "Alpha"), 1, null);
            v.add("note", Json.map("name", "zed", "fav", true), 1, null);
            List<Item> all = v.list("", "", false, false);
            eq("zed", all.get(0).name(), "fav first");
            eq("Alpha", all.get(1).name(), "then by name, case-insensitive");
            eq(1, v.list("BOB", "", false, false).size(), "search user");
            eq(2, v.list("", "login", false, false).size(), "type filter");
            eq(1, v.list("", "", false, true).size(), "fav filter");
        }});

        test("vault: match and save_login dedupe by user and domain", new T.Case() { public void run() throws Exception {
            Vault v = new Vault(new Clock("d"));
            String[] out = new String[2];
            v.saveLogin("https://accounts.example.co.uk/login?x=1", "ann", "one", null, 10, out);
            eq("true", out[1], "created");
            String first = out[0];
            eq("accounts.example.co.uk", v.get(first).name(), "named after host");
            eq(Arrays.asList("https://accounts.example.co.uk"), v.get(first).urls(), "canonical url");
            check(v.saveLogin("https://www.example.co.uk", "ann", "one", null, 11, out).isEmpty(), "same pass: nothing");
            v.saveLogin("https://www.example.co.uk", "ann", "two", null, 12, out);
            eq(first, out[0], "updated the same item");
            eq("false", out[1], "not created");
            eq("two", v.get(first).str("pass"), "new pass");
            eq("one", Json.str(v.get(first).history().get(0), "pass", ""), "history");
            v.saveLogin("https://example.co.uk", "bob", "b", null, 13, out);
            check(!out[0].equals(first), "other user: new item");
            v.saveLogin("androidapp://com.example.app", "ann", "a", "Example app", 14, out);
            eq(2, v.match("https://login.example.co.uk").size(), "two web matches");
            eq(1, v.match("androidapp://com.example.app").size(), "app match");
            eq(0, v.match("https://example.com").size(), "other domain");
            eq(first, v.match("https://accounts.example.co.uk/").get(0).id, "exact host first");
        }});

        test("vault: ops split under the body limit and refold identically", new T.Case() { public void run() throws Exception {
            Vault v = new Vault(new Clock("d"));
            StringBuilder big = new StringBuilder();
            for (int i = 0; i < 30000; i++) big.append('x');
            List<MapOp> ops = new ArrayList<>();
            for (int i = 0; i < 6; i++) ops.addAll(v.add("note", Json.map("name", "n" + i, "notes", big.toString()), 1, null));
            byte[] vk = app.raypass.core.Crypto.random(32);
            List<VaultEntry> es = VaultEntry.encryptOps(vk, "0123456789abcdef", ops);
            check(es.size() >= 3, "split into several entries");
            Vault w = new Vault(new Clock("e"));
            for (VaultEntry e : es) {
                check(Json.write(e.toJson()).length() < VaultEntry.MAX_BODY, "under 64 KiB");
                w.apply(e.decrypt(vk));
            }
            check(w.map().sameContent(v.map()), "same state");
        }});

        test("vault: settings defaults and bounds", new T.Case() { public void run() throws Exception {
            final Vault v = new Vault(new Clock("d"));
            eq(15, v.autoLock(), "default auto lock");
            eq(30, v.clipClear(), "default clip clear");
            v.setSettings(0, 90);
            eq(0, v.autoLock(), "never");
            eq(90, v.clipClear(), "clip");
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.setSettings(-1, null); }}, "bounds");
        }});

        test("vault: ops with invalid keys or no device are ignored", new T.Case() { public void run() {
            Vault v = new Vault(new Clock("d"));
            v.apply(Arrays.asList(new MapOp("i.XYZ.type", "login", false, new app.raypass.core.Stamp(1, 0, "a")),
                    new MapOp("i.0123456789abcdef.type", "login", false, new app.raypass.core.Stamp(1, 0, "")),
                    new MapOp("i.0123456789abcdef.Type", "login", false, new app.raypass.core.Stamp(1, 0, "a"))));
            check(v.map().keys().isEmpty(), "nothing applied");
        }});
    }
}
