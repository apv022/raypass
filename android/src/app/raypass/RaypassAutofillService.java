package app.raypass;

import android.app.PendingIntent;
import android.app.assist.AssistStructure;
import android.content.Context;
import android.content.Intent;
import android.content.IntentSender;
import android.os.Build;
import android.os.CancellationSignal;
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
import app.raypass.core.Totp;
import app.raypass.core.Vault;

import java.util.List;

/**
 * Android autofill: finds login fields (AutofillParser + FormFields), offers matching logins as
 * datasets when unlocked, an "Unlock Raypass" entry (authentication intent) when locked, and saves
 * new or changed logins through SaveInfo (deduplicated by user and domain, the old password kept
 * in history). Matching: androidapp://package, or the web domain from known browsers only.
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
        Repo repo = Repo.get(this);
        Vault v = repo.vault();
        if (v == null) {
            if (!repo.session().initialized()) {
                cb.onSuccess(null);
                return;
            }
            FillResponse.Builder b = new FillResponse.Builder();
            Intent i = new Intent(this, AutofillAuthActivity.class).putExtra("mode", "fill");
            // Mutable: the framework adds the AssistStructure to it. Explicit, so that is safe.
            IntentSender sender = PendingIntent.getActivity(this, requestCode(), i,
                    PendingIntent.FLAG_CANCEL_CURRENT | PendingIntent.FLAG_MUTABLE).getIntentSender();
            b.setAuthentication(p.ids(), sender, presentation(this, "Unlock Raypass", "to fill this login"));
            SaveInfo si = saveInfo(p);
            if (si != null) b.setSaveInfo(si);
            cb.onSuccess(b.build());
            return;
        }
        repo.touch();
        cb.onSuccess(response(this, p, v));
    }

    private static final java.util.concurrent.atomic.AtomicInteger codes = new java.util.concurrent.atomic.AtomicInteger();

    /**
     * A PendingIntent request code of its own: with one shared code, FLAG_CANCEL_CURRENT would
     * cancel the "Unlock" or one-time-code entry of a response that is still on screen.
     */
    static int requestCode() { return 16 + (codes.incrementAndGet() & 0xffff); }

    /**
     * The datasets for the matching logins (and their one-time codes when the screen has a code
     * field) plus SaveInfo; null if there is nothing to offer.
     */
    static FillResponse response(Context c, AutofillParser p, Vault v) {
        FillResponse.Builder b = new FillResponse.Builder();
        boolean any = false;
        List<Item> matches = v.match(p.url);
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
            Item it = matches.get(k);
            String user = it.str("user"), pass = it.str("pass");
            Dataset.Builder d = new Dataset.Builder(presentation(c, it.name(), user.isEmpty() ? "Raypass" : user));
            boolean set = false;
            if (p.form.username != null && !user.isEmpty()) {
                d.setValue((AutofillId) p.form.username.id, AutofillValue.forText(user));
                set = true;
            }
            if (p.form.password != null && !pass.isEmpty()) {
                d.setValue((AutofillId) p.form.password.id, AutofillValue.forText(pass));
                set = true;
            }
            if (set) {
                b.addDataset(d.build());
                any = true;
            }
        }
        SaveInfo si = saveInfo(p);
        if (si != null) {
            b.setSaveInfo(si);
            any = true;
        }
        return any ? b.build() : null;
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

    static SaveInfo saveInfo(AutofillParser p) {
        FormFields.Form f = p.form;
        if (f.password == null) return null;
        int type = SaveInfo.SAVE_DATA_TYPE_PASSWORD | (f.username != null ? SaveInfo.SAVE_DATA_TYPE_USERNAME : 0);
        SaveInfo.Builder b = new SaveInfo.Builder(type, new AutofillId[]{(AutofillId) f.password.id});
        if (f.username != null) b.setOptionalIds(new AutofillId[]{(AutofillId) f.username.id});
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
