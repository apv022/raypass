package app.raypass;

import android.app.PendingIntent;
import android.app.assist.AssistStructure;
import android.content.Context;
import android.content.Intent;
import android.content.IntentSender;
import android.os.Build;
import android.os.Bundle;
import android.os.CancellationSignal;
import android.os.SystemClock;
import android.service.autofill.AutofillService;
import android.service.autofill.Dataset;
import android.service.autofill.FillCallback;
import android.service.autofill.FillContext;
import android.service.autofill.FillRequest;
import android.service.autofill.FillResponse;
import android.service.autofill.SaveCallback;
import android.service.autofill.SaveInfo;
import android.service.autofill.SaveRequest;
import android.util.Log;
import android.view.autofill.AutofillId;
import android.view.autofill.AutofillValue;
import android.widget.RemoteViews;

import app.raypass.core.FormFields;
import app.raypass.core.Item;
import app.raypass.core.RecentFills;
import app.raypass.core.Totp;
import app.raypass.core.Vault;

import java.util.List;

/**
 * Android autofill: finds login fields (AutofillParser + FormFields), offers matching logins as
 * datasets when unlocked, and always a "Search Raypass" entry (or "Unlock Raypass" when locked)
 * that opens {@link AutofillPickActivity}: the person picks any login, it fills, and "Remember for
 * <app>" links it to the app for next time. Saves new or changed logins through SaveInfo
 * (deduplicated by user and domain, the old password kept in history), across a username screen
 * and a later password screen with FLAG_DELAY_SAVE (API 29+). Matching: androidapp://package, or
 * the web domain from known browsers only; nothing else ever fills on its own.
 *
 * Presentations are plain RemoteViews (the dropdown under the field), which every keyboard
 * supports; no inline (keyboard-strip) suggestions are offered.
 */
public class RaypassAutofillService extends AutofillService {
    private static final String TAG = "Raypass";
    private static final int MAX_DATASETS = 8;

    @Override
    public void onFillRequest(FillRequest req, CancellationSignal cancel, FillCallback cb) {
        List<FillContext> ctx = req.getFillContexts();
        AssistStructure s = ctx.get(ctx.size() - 1).getStructure();
        AutofillParser p = new AutofillParser(s);
        if (p.url == null || getPackageName().equals(p.pkg) || !p.form.fillable()) {
            cb.onSuccess(null);
            return;
        }
        noteSelections();
        Repo repo = Repo.get(this);
        Vault v = repo.vault();
        Bundle prev = req.getClientState();
        if (v == null) {
            if (!repo.session().initialized()) {
                cb.onSuccess(null);
                return;
            }
            FillResponse.Builder b = new FillResponse.Builder();
            if (p.form.username != null || p.form.password != null) {
                // One entry: unlock, then the picker (matches first), which fills directly.
                b.addDataset(pickDataset(this, p, true));
            } else {
                // A one-time-code step on its own: unlock, then this request's code entries.
                Intent i = new Intent(this, AutofillAuthActivity.class).putExtra("mode", "fill");
                // Mutable: the framework adds the AssistStructure to it. Explicit, so that is safe.
                IntentSender sender = PendingIntent.getActivity(this, requestCode(), i,
                        PendingIntent.FLAG_CANCEL_CURRENT | PendingIntent.FLAG_MUTABLE).getIntentSender();
                b.setAuthentication(p.ids(), sender, presentation(this, "Unlock Raypass", "to fill this code"));
            }
            SaveInfo si = saveInfo(p, prev);
            if (si != null) b.setSaveInfo(si);
            b.setClientState(clientState(p, prev));
            cb.onSuccess(b.build());
            return;
        }
        repo.touch();
        cb.onSuccess(response(this, p, v, prev));
    }

    /**
     * Records the logins picked from the previous response's dropdown (the fill event history,
     * datasets named "i:<id>"), so the next screen of a multi-step sign-in offers that login first.
     */
    private void noteSelections() {
        android.service.autofill.FillEventHistory h;
        try {
            h = getFillEventHistory();
        } catch (RuntimeException e) {
            return;
        }
        if (h == null || h.getEvents() == null) return;
        for (android.service.autofill.FillEventHistory.Event e : h.getEvents()) {
            if (e.getType() != android.service.autofill.FillEventHistory.Event.TYPE_DATASET_SELECTED) continue;
            String id = e.getDatasetId();
            Bundle cs = Build.VERSION.SDK_INT >= 28 ? e.getClientState() : legacyClientState(h);
            if (id == null || !id.startsWith("i:") || cs == null) continue;
            RecentFills.record(cs.getString("url"), id.substring(2), cs.getLong("t", 0));
        }
    }

    @SuppressWarnings("deprecation")
    private static Bundle legacyClientState(android.service.autofill.FillEventHistory h) { return h.getClientState(); }

    /**
     * What a response remembers for the next request of the same session: the URL and time (for
     * {@link #noteSelections}) and, on a username-only screen, the username field, so the later
     * password screen can save both (FLAG_DELAY_SAVE).
     */
    static Bundle clientState(AutofillParser p, Bundle prev) {
        Bundle b = new Bundle();
        b.putString("url", p.url);
        b.putLong("t", SystemClock.elapsedRealtime());
        AutofillId step = null;
        if (p.form.username != null && p.form.password == null) step = (AutofillId) p.form.username.id;
        else if (p.form.username == null) step = stepUser(p, prev);
        if (step != null) b.putParcelable("step_user", step);
        return b;
    }

    /** The username field of an earlier screen of this sign-in (same URL), from the client state. */
    static AutofillId stepUser(AutofillParser p, Bundle prev) {
        if (prev == null || !p.url.equals(prev.getString("url"))) return null;
        return prev.getParcelable("step_user");
    }

    private static final java.util.concurrent.atomic.AtomicInteger codes = new java.util.concurrent.atomic.AtomicInteger();

    /**
     * A PendingIntent request code of its own: with one shared code, FLAG_CANCEL_CURRENT would
     * cancel the "Unlock" or one-time-code entry of a response that is still on screen.
     */
    static int requestCode() { return 16 + (codes.incrementAndGet() & 0xffff); }

    /**
     * The datasets for the matching logins (and their one-time codes when the screen has a code
     * field), "Search Raypass", SaveInfo and the client state; null if there is nothing to offer.
     * On a password screen with no username field (the second step of a sign-in), the login
     * filled into this app or page a few minutes ago comes first, linked or not: the person
     * picked it here a moment ago.
     */
    static FillResponse response(Context c, AutofillParser p, Vault v, Bundle prev) {
        FillResponse.Builder b = new FillResponse.Builder();
        boolean any = false;
        List<Item> matches = new java.util.ArrayList<>(v.match(p.url));
        if (p.form.password != null && p.form.username == null) {
            Item recent = v.get(String.valueOf(RecentFills.recent(p.url, SystemClock.elapsedRealtime())));
            if (recent != null && Item.LOGIN.equals(recent.type) && recent.trashed() == 0) {
                for (int k = matches.size() - 1; k >= 0; k--) if (matches.get(k).id.equals(recent.id)) matches.remove(k);
                matches.add(0, recent);
            }
        }
        if (p.form.otp != null) {
            int n = 0;
            for (Item it : matches) {
                if (n >= MAX_DATASETS) break;
                Dataset d = otpDataset(c, p, it);
                if (d == null) continue;
                b.addDataset(d);
                any = true;
                n++;
            }
        }
        for (int k = 0; k < matches.size() && k < MAX_DATASETS; k++) {
            Dataset d = loginDataset(c, p, matches.get(k));
            if (d != null) {
                b.addDataset(d);
                any = true;
            }
        }
        if (p.form.username != null || p.form.password != null) {
            b.addDataset(pickDataset(c, p, false));
            any = true;
        }
        SaveInfo si = saveInfo(p, prev);
        if (si != null) {
            b.setSaveInfo(si);
            any = true;
        }
        if (!any) return null;
        b.setClientState(clientState(p, prev));
        return b.build();
    }

    /**
     * A dataset filling it into the fields of this screen (only the username on a username-only
     * screen), named "i:<id>" for the fill event history; null when it has nothing for them.
     */
    static Dataset loginDataset(Context c, AutofillParser p, Item it) {
        String user = it.str("user"), pass = it.str("pass");
        Dataset.Builder d = new Dataset.Builder(presentation(c, it.name(), user.isEmpty() ? "Raypass" : user));
        d.setId("i:" + it.id);
        boolean set = false;
        if (p.form.username != null && !user.isEmpty()) {
            d.setValue((AutofillId) p.form.username.id, AutofillValue.forText(user));
            set = true;
        }
        if (p.form.password != null && !pass.isEmpty()) {
            d.setValue((AutofillId) p.form.password.id, AutofillValue.forText(pass));
            set = true;
        }
        return set ? d.build() : null;
    }

    /**
     * "Search Raypass" (or "Unlock Raypass" when locked): an entry whose authentication intent
     * opens the picker. The fields and the URL go in explicit extras of an immutable intent; the
     * picker returns the filled dataset.
     */
    static Dataset pickDataset(Context c, AutofillParser p, boolean locked) {
        Intent i = new Intent(c, AutofillPickActivity.class).putExtra("url", p.url).putExtra("pkg", p.pkg)
                .putExtra("browser", app.raypass.core.AutofillTarget.BROWSERS.contains(p.pkg));
        if (p.form.username != null) i.putExtra("user_id", (AutofillId) p.form.username.id);
        if (p.form.password != null) i.putExtra("pass_id", (AutofillId) p.form.password.id);
        if (p.form.otp != null) i.putExtra("otp_id", (AutofillId) p.form.otp.id);
        IntentSender sender = PendingIntent.getActivity(c, requestCode(), i,
                PendingIntent.FLAG_CANCEL_CURRENT | PendingIntent.FLAG_IMMUTABLE).getIntentSender();
        String where = p.url.startsWith("androidapp://") ? "this app" : "this site";
        Dataset.Builder d = new Dataset.Builder(locked
                ? presentation(c, "Unlock Raypass", "then pick a login for " + where)
                : presentation(c, "Search Raypass", "pick a login for " + where));
        d.setId("pick");
        for (FormFields.Field f : p.form.all()) {
            if (f == p.form.newPassword) continue;
            d.setValue((AutofillId) f.id, null); // the values come back from the picker
        }
        d.setAuthentication(sender);
        return d.build();
    }

    /**
     * A dataset that fills the one-time code of it into the code field. The code is computed when
     * the person picks it (an authentication intent that returns the filled dataset at once), not
     * now: a response can sit on screen longer than the 30 s a code lives.
     */
    private static Dataset otpDataset(Context c, AutofillParser p, Item it) {
        String secret = it.str("totp");
        if (secret.isEmpty()) return null;
        try {
            Totp.parse(secret);
        } catch (Totp.Invalid e) {
            return null;
        }
        AutofillId field = (AutofillId) p.form.otp.id;
        Intent i = new Intent(c, AutofillAuthActivity.class).putExtra("mode", "otp").putExtra("item", it.id).putExtra("field", field);
        IntentSender sender = PendingIntent.getActivity(c, requestCode(), i,
                PendingIntent.FLAG_CANCEL_CURRENT | PendingIntent.FLAG_IMMUTABLE).getIntentSender();
        Dataset.Builder d = new Dataset.Builder(presentation(c, it.name(), "One-time code"));
        d.setValue(field, null); // the value comes back from the authentication intent
        d.setAuthentication(sender);
        return d.build();
    }

    /** The dataset with the current one-time code of item id for field, or null. */
    static Dataset otpResult(Vault v, String id, AutofillId field) {
        Item it = id == null ? null : v.get(id);
        if (it == null || field == null) return null;
        try {
            Totp t = Totp.parse(it.str("totp"));
            Dataset.Builder d = new Dataset.Builder();
            d.setValue(field, AutofillValue.forText(t.code(System.currentTimeMillis())));
            return d.build();
        } catch (Totp.Invalid e) {
            return null;
        }
    }

    /**
     * What to offer to save. A password screen saves the password with the username of the same
     * screen or, on the second screen of a sign-in, of the earlier one (from the client state).
     * A username-only screen (API 29+) asks the framework to wait for the password screen
     * (FLAG_DELAY_SAVE), so "Save to Raypass?" comes once, with both.
     */
    static SaveInfo saveInfo(AutofillParser p, Bundle prev) {
        FormFields.Form f = p.form;
        if (f.password == null) {
            if (f.username == null || Build.VERSION.SDK_INT < 29) return null;
            SaveInfo.Builder b = new SaveInfo.Builder(SaveInfo.SAVE_DATA_TYPE_USERNAME, new AutofillId[]{(AutofillId) f.username.id});
            b.setFlags(SaveInfo.FLAG_DELAY_SAVE);
            return b.build();
        }
        AutofillId user = f.username != null ? (AutofillId) f.username.id : stepUser(p, prev);
        int type = SaveInfo.SAVE_DATA_TYPE_PASSWORD | (user != null ? SaveInfo.SAVE_DATA_TYPE_USERNAME : 0);
        SaveInfo.Builder b = new SaveInfo.Builder(type, new AutofillId[]{(AutofillId) f.password.id});
        if (user != null) b.setOptionalIds(new AutofillId[]{user});
        b.setFlags(SaveInfo.FLAG_SAVE_ON_ALL_VIEWS_INVISIBLE);
        return b.build();
    }

    static RemoteViews presentation(Context c, String title, String sub) {
        RemoteViews r = new RemoteViews(c.getPackageName(), android.R.layout.simple_list_item_2);
        r.setTextViewText(android.R.id.text1, title);
        r.setTextViewText(android.R.id.text2, sub);
        return r;
    }

    @Override
    public void onSaveRequest(SaveRequest req, SaveCallback cb) {
        List<FillContext> ctx = req.getFillContexts();
        // The values are in the latest structure; the fields may have been found in an earlier one.
        String user = "", pass = "", url = null;
        for (int i = ctx.size() - 1; i >= 0; i--) {
            AutofillParser p = new AutofillParser(ctx.get(i).getStructure());
            if (url == null) url = p.url;
            if (pass.isEmpty() && p.form.password != null && p.form.password.text != null) pass = p.form.password.text;
            if (user.isEmpty() && p.form.username != null && p.form.username.text != null) user = p.form.username.text;
        }
        if (url == null || pass.isEmpty()) {
            cb.onFailure("Nothing to save");
            return;
        }
        final Repo repo = Repo.get(this);
        if (repo.vault() != null) {
            try {
                repo.saveLogin(url, user.trim(), pass);
                cb.onSuccess();
            } catch (Exception e) {
                Log.w(TAG, "autofill save failed: " + e.getClass().getSimpleName());
                cb.onFailure("Raypass could not save this login");
            }
            return;
        }
        if (Build.VERSION.SDK_INT >= 28 && repo.session().initialized()) {
            // Locked: keep the login in memory only, unlock, then save (AutofillAuthActivity).
            String token = Repo.putPendingSave(url, user.trim(), pass);
            Intent i = new Intent(this, AutofillAuthActivity.class).putExtra("mode", "save").putExtra("token", token);
            cb.onSuccess(PendingIntent.getActivity(this, requestCode(), i, PendingIntent.FLAG_CANCEL_CURRENT | PendingIntent.FLAG_IMMUTABLE).getIntentSender());
            return;
        }
        cb.onFailure("Unlock Raypass, then save again");
    }
}
