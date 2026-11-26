package app.raypass;

import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Bundle;
import android.os.SystemClock;
import android.service.autofill.Dataset;
import android.text.Editable;
import android.text.TextWatcher;
import android.view.View;
import android.view.ViewGroup;
import android.view.WindowManager;
import android.view.autofill.AutofillId;
import android.view.autofill.AutofillManager;
import android.view.autofill.AutofillValue;
import android.widget.AdapterView;
import android.widget.BaseAdapter;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ListView;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import app.raypass.core.AutofillPick;
import app.raypass.core.Item;
import app.raypass.core.RecentFills;
import app.raypass.core.Totp;
import app.raypass.core.UrlMatch;
import app.raypass.core.Vault;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/**
 * "Search Raypass" / "Unlock Raypass" in the autofill dropdown: unlock if needed, then list the
 * logins (suggestions first, {@link AutofillPick}; "Show all logins" and a search box for the
 * rest). The person taps one; it fills this screen's fields (EXTRA_AUTHENTICATION_RESULT). With
 * "Remember for <app>" (on by default) the app's androidapp://<package>, or the page's site, is
 * added to the login's URLs, an ordinary synced vault change, so the next fill there is
 * automatic. The link is always the person's choice: nothing here fills without a tap.
 *
 * Extras (set by RaypassAutofillService, immutable PendingIntent): url, pkg, browser, and the
 * AutofillIds user_id, pass_id, otp_id of the fields on screen (each may be absent).
 */
public class AutofillPickActivity extends BaseActivity {
    private String url, pkg, label;
    private boolean browser, showingList, showAll;
    private List<Item> ranked = new ArrayList<>();
    private final List<Item> shown = new ArrayList<>();
    private final Adapter adapter = new Adapter();
    private EditText search;
    private CheckBox remember;
    private TextView more;

    @Override
    boolean needsVault() { return false; }

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        setFinishOnTouchOutside(false);
        setResult(RESULT_CANCELED);
        Intent in = getIntent();
        url = in.getStringExtra("url");
        pkg = in.getStringExtra("pkg");
        browser = in.getBooleanExtra("browser", false);
        if (url == null || UrlMatch.parse(url) == null) {
            finish();
            return;
        }
        label = browser || !url.startsWith("androidapp://") ? UrlMatch.displayHost(url) : appLabel(pkg);
        getWindow().setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT);
        if (repo.vault() != null) {
            showList();
            return;
        }
        if (!repo.session().initialized()) {
            Toast.makeText(this, "Open Raypass to set up your vault first", Toast.LENGTH_LONG).show();
            finish();
            return;
        }
        ScrollView sv = new ScrollView(this);
        sv.addView(UnlockView.build(this, false, new UnlockView.Done() {
            public void unlocked() { showList(); }
        }));
        setContentView(sv);
    }

    /** The app's name for "Remember for …"; its package when Android does not let us see it. */
    private String appLabel(String p) {
        if (p == null) return "this app";
        try {
            PackageManager pm = getPackageManager();
            CharSequence l = pm.getApplicationLabel(pm.getApplicationInfo(p, 0));
            if (l != null && l.length() > 0) return l.toString();
        } catch (PackageManager.NameNotFoundException | RuntimeException e) {
            // Package visibility (Android 11+) can hide it: fall back to the package name.
        }
        return p;
    }

    private void showList() {
        Vault v = repo.vault();
        if (v == null) {
            finish();
            return;
        }
        showingList = true;
        repo.touch();
        String recent = RecentFills.recent(url, SystemClock.elapsedRealtime());
        ranked = AutofillPick.rank(v.logins(), url, browser ? null : pkg, recent);
        boolean anySuggested = !ranked.isEmpty() && AutofillPick.suggested(AutofillPick.score(ranked.get(0), url, browser ? null : pkg, recent));
        showAll = !anySuggested;

        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 20);
        col.setPadding(pad, pad, pad, Ui.dp(this, 8));
        col.addView(Ui.title(this, "Pick a login"));
        col.addView(Ui.text(this, "to fill " + label, 14, R.color.text2), Ui.matchWrap(this, 2));
        search = Ui.input(this, "Search logins", false);
        search.addTextChangedListener(new TextWatcher() {
            public void beforeTextChanged(CharSequence s, int a, int b, int c) {}
            public void onTextChanged(CharSequence s, int a, int b, int c) {}
            public void afterTextChanged(Editable s) { filter(); }
        });
        col.addView(search, Ui.matchWrap(this, 14));
        remember = new CheckBox(this);
        remember.setText("Remember for " + label);
        remember.setTextColor(Ui.color(this, R.color.text));
        remember.setChecked(true);
        col.addView(remember, Ui.matchWrap(this, 8));

        ListView list = new ListView(this);
        list.setAdapter(adapter);
        list.setDivider(null);
        list.setOnItemClickListener(new AdapterView.OnItemClickListener() {
            public void onItemClick(AdapterView<?> parent, View view, int pos, long id) { pick(shown.get(pos)); }
        });
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1);
        lp.topMargin = Ui.dp(this, 6);
        col.addView(list, lp);
        more = Ui.flat(this, "Show all logins");
        more.setOnClickListener(new View.OnClickListener() {
            public void onClick(View x) {
                showAll = true;
                filter();
            }
        });
        col.addView(more);
        TextView cancel = Ui.flat(this, "Cancel");
        cancel.setOnClickListener(new View.OnClickListener() {
            public void onClick(View x) { finish(); }
        });
        col.addView(cancel);
        setContentView(col);
        getWindow().setSoftInputMode(WindowManager.LayoutParams.SOFT_INPUT_STATE_HIDDEN | WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE);
        filter();
    }

    /** Applies the search (over every login) or, with no query, the suggestions / all toggle. */
    private void filter() {
        String q = search.getText().toString().trim().toLowerCase(Locale.ROOT);
        Vault v = repo.vault();
        shown.clear();
        if (v != null) {
            List<Item> hits = q.isEmpty() ? null : v.list(q, Item.LOGIN, false, false);
            String recent = RecentFills.recent(url, SystemClock.elapsedRealtime());
            for (Item it : ranked) {
                if (hits != null) {
                    if (containsId(hits, it.id)) shown.add(it);
                } else if (showAll || AutofillPick.suggested(AutofillPick.score(it, url, browser ? null : pkg, recent))) {
                    shown.add(it);
                }
            }
        }
        more.setVisibility(q.isEmpty() && !showAll ? View.VISIBLE : View.GONE);
        adapter.notifyDataSetChanged();
    }

    private static boolean containsId(List<Item> items, String id) {
        for (Item it : items) if (it.id.equals(id)) return true;
        return false;
    }

    /** Fills it into this screen, links it when asked, and hands the dataset back. */
    private void pick(Item picked) {
        Vault v = repo.vault();
        Item it = v == null ? null : v.get(picked.id);
        if (it == null) {
            finish();
            return;
        }
        AutofillId userId = getIntent().getParcelableExtra("user_id");
        AutofillId passId = getIntent().getParcelableExtra("pass_id");
        AutofillId otpId = getIntent().getParcelableExtra("otp_id");
        Dataset.Builder d = new Dataset.Builder();
        boolean set = false;
        if (userId != null && !it.str("user").isEmpty()) {
            d.setValue(userId, AutofillValue.forText(it.str("user")));
            set = true;
        }
        if (passId != null && !it.str("pass").isEmpty()) {
            d.setValue(passId, AutofillValue.forText(it.str("pass")));
            set = true;
        }
        if (otpId != null && !it.str("totp").isEmpty()) {
            try {
                d.setValue(otpId, AutofillValue.forText(Totp.parse(it.str("totp")).code(System.currentTimeMillis())));
                set = true;
            } catch (Totp.Invalid e) {
                // No code to fill; the login itself still fills.
            }
        }
        if (!set) {
            Toast.makeText(this, "This login has nothing to fill here", Toast.LENGTH_SHORT).show();
            return;
        }
        if (remember.isChecked() && !containsId(v.match(url), it.id)) {
            try {
                repo.commit(v.rememberUrl(it.id, url, System.currentTimeMillis()));
            } catch (Exception e) {
                // Filling still works; only the link for next time is missing.
                Toast.makeText(this, "Raypass could not remember this login for " + label, Toast.LENGTH_LONG).show();
            }
        }
        RecentFills.record(url, it.id, SystemClock.elapsedRealtime());
        repo.touch();
        setResult(RESULT_OK, new Intent().putExtra(AutofillManager.EXTRA_AUTHENTICATION_RESULT, d.build()));
        finish();
    }

    @Override
    public void changed() {
        if (isFinishing() || isDestroyed()) return;
        if (showingList && repo.vault() == null) {
            finish(); // locked while open: drop the list of names
            return;
        }
        if (showingList && isResumedNow()) {
            Vault v = repo.vault();
            ranked = AutofillPick.rank(v.logins(), url, browser ? null : pkg, RecentFills.recent(url, SystemClock.elapsedRealtime()));
            filter();
        }
    }

    /** Rows: avatar, name, user (or the first URL's host). */
    private final class Adapter extends BaseAdapter {
        public int getCount() { return shown.size(); }

        public Object getItem(int i) { return shown.get(i); }

        public long getItemId(int i) { return i; }

        public View getView(int i, View convert, ViewGroup parent) {
            Item it = shown.get(i);
            LinearLayout r = Ui.row(AutofillPickActivity.this);
            int pad = Ui.dp(AutofillPickActivity.this, 4);
            r.setPadding(pad, Ui.dp(AutofillPickActivity.this, 10), pad, Ui.dp(AutofillPickActivity.this, 10));
            int s = Ui.dp(AutofillPickActivity.this, 36);
            r.addView(Ui.avatar(AutofillPickActivity.this, it.name(), it.type), new LinearLayout.LayoutParams(s, s));
            LinearLayout texts = Ui.column(AutofillPickActivity.this);
            texts.setPadding(Ui.dp(AutofillPickActivity.this, 12), 0, 0, 0);
            TextView name = Ui.text(AutofillPickActivity.this, it.name(), 16, R.color.text);
            name.setSingleLine(true);
            name.setEllipsize(android.text.TextUtils.TruncateAt.END);
            texts.addView(name);
            String sub = it.str("user");
            if (sub.isEmpty() && !it.urls().isEmpty()) sub = UrlMatch.displayHost(it.urls().get(0));
            TextView st = Ui.text(AutofillPickActivity.this, sub, 13, R.color.text2);
            st.setSingleLine(true);
            st.setEllipsize(android.text.TextUtils.TruncateAt.END);
            texts.addView(st);
            r.addView(texts, Ui.weight(1));
            return r;
        }
    }
}
