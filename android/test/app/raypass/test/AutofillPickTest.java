package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.AppDomains;
import app.raypass.core.AutofillPick;
import app.raypass.core.Clock;
import app.raypass.core.FormFields;
import app.raypass.core.Item;
import app.raypass.core.Json;
import app.raypass.core.MapOp;
import app.raypass.core.RecentFills;
import app.raypass.core.Vault;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/** The autofill picker for apps with no linked login: ranking, "Remember for", multi-step sign-ins. */
final class AutofillPickTest {
    static String add(Vault v, String name, String... urls) throws Exception {
        String[] id = new String[1];
        v.add("login", Json.map("name", name, "user", "u-" + name, "pass", "p", "urls", new ArrayList<Object>(Arrays.asList((Object[]) urls))), 1, id);
        return id[0];
    }

    static List<String> names(List<Item> items) {
        List<String> out = new ArrayList<>();
        for (Item it : items) out.add(it.name());
        return out;
    }

    static void register() {
        test("autofill: a login saved for twitter.com is offered on x.com, after x.com's own", new T.Case() { public void run() throws Exception {
            Vault v = new Vault(new Clock("d"));
            add(v, "Twitter", "https://twitter.com");
            add(v, "X", "https://x.com");
            add(v, "Bank", "https://bank.com");
            eq(Arrays.asList("X", "Twitter"), names(v.match("https://x.com")), "same domain first, then equivalent");
            eq(Arrays.asList("Twitter", "X"), names(v.match("https://mobile.twitter.com")), "and the other way round");
            eq(0, v.match("androidapp://com.twitter.android").size(), "an app matches only a login linked to it");
        }});

        test("autofill: the curated table and package words only order the picker", new T.Case() { public void run() throws Exception {
            eq("x.com", AppDomains.domain("com.twitter.android"), "X");
            eq("amazon.com", AppDomains.domain("com.amazon.mShop.android.shopping"), "case-insensitive");
            check(AppDomains.domain("com.evil.twitter") == null, "unknown");
            eq(Arrays.asList("twitter"), AppDomains.words("com.twitter.android"), "words");
            eq(Arrays.asList("example", "shop"), AppDomains.words("com.example.shop.app"), "generic parts dropped");
            Vault v = new Vault(new Clock("d"));
            add(v, "Bank", "https://bank.com");
            add(v, "My twitter", "https://twitter.com"); // equivalent to x.com, the table's domain for the X app
            add(v, "Alpha");
            add(v, "Linked", "androidapp://com.twitter.android");
            String recent = add(v, "Zed", "https://zed.example");
            String url = "androidapp://com.twitter.android";
            List<Item> r = AutofillPick.rank(v.logins(), url, "com.twitter.android", null);
            eq(Arrays.asList("Linked", "My twitter", "Alpha", "Bank", "Zed"), names(r), "match, app domain, then the rest by name");
            eq(AutofillPick.APP_DOMAIN, AutofillPick.score(r.get(1), url, "com.twitter.android", null), "via the table");
            eq(AutofillPick.OTHER, AutofillPick.score(r.get(2), url, "com.twitter.android", null), "unrelated");
            r = AutofillPick.rank(v.logins(), url, "com.twitter.android", recent);
            eq("Zed", r.get(0).name(), "the login filled a moment ago comes first");
            Vault w = new Vault(new Clock("d"));
            add(w, "Example shop", "https://shop.example.org");
            add(w, "Other");
            eq(AutofillPick.WORD, AutofillPick.score(AutofillPick.rank(w.logins(), "androidapp://com.example.shop", "com.example.shop", null).get(0),
                    "androidapp://com.example.shop", "com.example.shop", null), "package words");
            check(!v.match(url).isEmpty() && v.match(url).size() == 1, "the table never makes a match by itself");
        }});

        test("autofill: Remember for <app> links the login, once", new T.Case() { public void run() throws Exception {
            final Vault v = new Vault(new Clock("d"));
            final String id = add(v, "X", "https://x.com");
            List<MapOp> ops = v.rememberUrl(id, "androidapp://com.twitter.android", 5);
            check(!ops.isEmpty(), "ops");
            eq(Arrays.asList("https://x.com", "androidapp://com.twitter.android"), v.get(id).urls(), "appended");
            eq("X", v.match("androidapp://com.twitter.android").get(0).name(), "the next fill matches automatically");
            eq(0, v.rememberUrl(id, "androidapp://com.twitter.android", 6).size(), "already linked: nothing to do");
            eq("p", v.get(id).str("pass"), "the password is untouched");
            check(v.get(id).history().isEmpty(), "no history entry");
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.rememberUrl("nope", "androidapp://a.b", 7); }}, "no such login");
            throwsA(Vault.BadInput.class, new T.Case() { public void run() throws Exception { v.rememberUrl(id, "", 7); }}, "no url");
        }});

        test("autofill: a multi-step sign-in remembers the login for a few minutes", new T.Case() { public void run() {
            RecentFills.clear();
            RecentFills.record("androidapp://com.twitter.android", "abc", 1000);
            eq("abc", RecentFills.recent("androidapp://com.twitter.android", 1000 + 60_000), "a minute later");
            check(RecentFills.recent("androidapp://com.other", 2000) == null, "another app");
            check(RecentFills.recent("androidapp://com.twitter.android", 1000 + RecentFills.WINDOW_MS + 1) == null, "too late");
            RecentFills.record("androidapp://com.twitter.android", "def", 5000);
            RecentFills.record("androidapp://com.twitter.android", "old", 4000);
            eq("def", RecentFills.recent("androidapp://com.twitter.android", 6000), "an older event does not replace a newer one");
            RecentFills.clear();
            check(RecentFills.recent("androidapp://com.twitter.android", 6000) == null, "cleared on lock");
        }});

        test("autofill: X's first step (phone, email, or username) is a username field", new T.Case() { public void run() {
            FormFields.Field u = new FormFields.Field("u");
            u.inputType = 0x01;
            u.hint = "Phone, email, or username";
            eq(FormFields.Kind.USERNAME, FormFields.classify(u), "username step");
            FormFields.Form f = FormFields.parse(Arrays.asList(u));
            check(f.username == u && f.password == null && f.fillable(), "a username-only screen");
            FormFields.Field ph = new FormFields.Field("ph");
            ph.inputType = 0x01;
            ph.hint = "Phone number";
            eq(FormFields.Kind.NONE, FormFields.classify(ph), "a phone field alone is not a username");
        }});
    }
}
