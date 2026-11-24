package app.raypass;

import android.app.AlertDialog;
import android.content.DialogInterface;
import android.content.Intent;
import android.graphics.Typeface;
import android.net.Uri;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.view.Menu;
import android.view.MenuItem;
import android.view.View;
import android.widget.Button;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;
import android.widget.Toast;

import app.raypass.core.Item;
import app.raypass.core.Json;
import app.raypass.core.MapOp;
import app.raypass.core.Totp;
import app.raypass.core.UrlMatch;
import app.raypass.core.Vault;

import java.text.DateFormat;
import java.util.Arrays;
import java.util.Date;
import java.util.List;
import java.util.Map;

/**
 * One item: every field with copy buttons, secrets hidden until revealed, a live TOTP code, the
 * password history, and edit / favourite / trash / restore / delete-forever in the action bar.
 */
public class ItemActivity extends BaseActivity {
    private String id;
    private final Handler main = new Handler(Looper.getMainLooper());
    private Runnable tick;
    private final java.util.Set<String> revealed = new java.util.HashSet<>();

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        id = getIntent().getStringExtra("id");
        if (getActionBar() != null) getActionBar().setDisplayHomeAsUpEnabled(true);
    }

    @Override
    protected void onResume() {
        super.onResume();
        if (repo.vault() != null) render();
    }

    @Override
    protected void onPause() {
        super.onPause();
        if (tick != null) main.removeCallbacks(tick);
        revealed.clear(); // leaving the screen hides secrets again
    }

    @Override
    void refresh() { render(); }

    private Item item() {
        Vault v = repo.vault();
        return v == null ? null : v.get(id);
    }

    private void render() {
        final Item it = item();
        if (it == null) {
            finish();
            return;
        }
        invalidateOptionsMenu();
        setTitle(Ui.typeLabel(it.type));
        if (tick != null) main.removeCallbacks(tick);
        tick = null;
        ScrollView sv = new ScrollView(this);
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 16);
        col.setPadding(pad, pad, pad, Ui.dp(this, 32));
        sv.addView(col);

        LinearLayout head = Ui.row(this);
        int s = Ui.dp(this, 48);
        head.addView(Ui.avatar(this, it.name(), it.type), new LinearLayout.LayoutParams(s, s));
        LinearLayout ht = Ui.column(this);
        ht.setPadding(Ui.dp(this, 14), 0, 0, 0);
        ht.addView(Ui.title(this, it.name() + (it.fav() ? "  ★" : "")));
        String meta = Ui.typeLabel(it.type) + (it.str("folder").isEmpty() ? "" : " · " + it.str("folder"));
        ht.addView(Ui.text(this, meta, 13, R.color.text2));
        head.addView(ht, Ui.weight(1));
        col.addView(head);
        if (it.trashed() != 0) {
            TextView t = Ui.text(this, "In the trash since " + DateFormat.getDateTimeInstance().format(new Date(it.trashed())), 14, R.color.danger);
            col.addView(t, Ui.matchWrap(this, 12));
        }

        LinearLayout card = Ui.card(this);
        col.addView(card, Ui.matchWrap(this, 16));
        boolean any = false;
        for (String f : Item.typeFields(it.type)) {
            if (f.equals("urls") || f.equals("history")) continue;
            String v = it.str(f);
            if (v.isEmpty()) continue;
            if (any) card.addView(Ui.divider(this));
            any = true;
            if (f.equals("totp")) addTotp(card, v);
            else addField(card, f, Ui.fieldLabel(f), v, Item.secretField(f), f.equals("seed") || f.equals("address") || f.equals("privkey"));
        }
        for (String u : it.urls()) {
            if (any) card.addView(Ui.divider(this));
            any = true;
            addUrl(card, u);
        }
        int n = 0;
        for (Map<String, Object> fld : it.fields()) {
            if (any) card.addView(Ui.divider(this));
            any = true;
            addField(card, "field." + n++, Json.str(fld, "name", ""), Json.str(fld, "value", ""), Json.bool(fld, "hidden", false), false);
        }
        if (!any) card.setVisibility(View.GONE);

        if (!it.str("notes").isEmpty()) {
            LinearLayout nc = Ui.card(this);
            nc.addView(Ui.label(this, "Notes"));
            TextView nt = Ui.text(this, it.str("notes"), 15, R.color.text);
            nt.setTextIsSelectable(true);
            nc.addView(nt, Ui.matchWrap(this, 6));
            Button copy = Ui.flat(this, "Copy");
            final String notes = it.str("notes");
            copy.setOnClickListener(new View.OnClickListener() {
                public void onClick(View v) { Clip.copy(ItemActivity.this, "Notes", notes, it.type.equals(Item.NOTE)); }
            });
            nc.addView(copy);
            col.addView(nc, Ui.matchWrap(this, 12));
        }
        List<String> tags = it.strings("tags");
        if (!tags.isEmpty()) col.addView(Ui.text(this, "Tags: " + android.text.TextUtils.join(", ", tags), 13, R.color.text2), Ui.matchWrap(this, 12));

        final List<Map<String, Object>> hist = it.history();
        if (!hist.isEmpty()) {
            final LinearLayout hc = Ui.card(this);
            hc.addView(Ui.label(this, "Password history"));
            final Button show = Ui.flat(this, "Show " + hist.size() + " previous password" + (hist.size() == 1 ? "" : "s"));
            hc.addView(show);
            show.setOnClickListener(new View.OnClickListener() {
                public void onClick(View v) {
                    hc.removeView(show);
                    int k = 0;
                    for (Map<String, Object> h : hist) {
                        addField(hc, "hist." + k, "Until " + DateFormat.getDateTimeInstance().format(new Date(Json.num(h, "until", 0))),
                                Json.str(h, "pass", ""), true, false);
                        revealed.add("hist." + k++);
                    }
                }
            });
            col.addView(hc, Ui.matchWrap(this, 12));
        }
        DateFormat df = DateFormat.getDateTimeInstance();
        col.addView(Ui.text(this, "Created " + df.format(new Date(it.created())) + "\nUpdated " + df.format(new Date(it.updated())), 12, R.color.text2),
                Ui.matchWrap(this, 16));
        setContentView(sv);
    }

    private void addField(LinearLayout parent, final String key, final String label, final String value, final boolean secret, boolean mono) {
        LinearLayout row = Ui.row(this);
        row.setPadding(0, Ui.dp(this, 8), 0, Ui.dp(this, 8));
        LinearLayout texts = Ui.column(this);
        texts.addView(Ui.label(this, label));
        final TextView v = Ui.text(this, secret && !revealed.contains(key) ? "••••••••••" : value, 16, R.color.text);
        if (secret || mono) v.setTypeface(Typeface.MONOSPACE);
        v.setTextIsSelectable(!secret);
        texts.addView(v);
        row.addView(texts, Ui.weight(1));
        if (secret) {
            final Button eye = Ui.flat(this, revealed.contains(key) ? "Hide" : "Show");
            eye.setOnClickListener(new View.OnClickListener() {
                public void onClick(View b) {
                    if (revealed.remove(key)) {
                        v.setText("••••••••••");
                        eye.setText("Show");
                    } else {
                        revealed.add(key);
                        v.setText(value);
                        eye.setText("Hide");
                    }
                }
            });
            row.addView(eye);
        }
        Button copy = Ui.flat(this, "Copy");
        copy.setOnClickListener(new View.OnClickListener() {
            public void onClick(View b) { Clip.copy(ItemActivity.this, label, value, secret); }
        });
        row.addView(copy);
        parent.addView(row);
    }

    private void addUrl(LinearLayout parent, final String url) {
        LinearLayout row = Ui.row(this);
        row.setPadding(0, Ui.dp(this, 8), 0, Ui.dp(this, 8));
        LinearLayout texts = Ui.column(this);
        texts.addView(Ui.label(this, url.startsWith("androidapp://") ? "App" : "Website"));
        texts.addView(Ui.text(this, url, 15, R.color.text));
        row.addView(texts, Ui.weight(1));
        UrlMatch.Url p = UrlMatch.parse(url);
        if (p != null && (p.scheme.equals("https") || p.scheme.equals("http"))) {
            Button open = Ui.flat(this, "Open");
            final String target = url.contains("://") ? url : "https://" + url;
            open.setOnClickListener(new View.OnClickListener() {
                public void onClick(View b) {
                    try {
                        startActivity(new Intent(Intent.ACTION_VIEW, Uri.parse(target)));
                    } catch (Exception e) {
                        Toast.makeText(ItemActivity.this, "No app can open it", Toast.LENGTH_SHORT).show();
                    }
                }
            });
            row.addView(open);
        }
        Button copy = Ui.flat(this, "Copy");
        copy.setOnClickListener(new View.OnClickListener() {
            public void onClick(View b) { Clip.copy(ItemActivity.this, "Address", url, false); }
        });
        row.addView(copy);
        parent.addView(row);
    }

    private void addTotp(LinearLayout parent, String secret) {
        final Totp t;
        try {
            t = Totp.parse(secret);
        } catch (Totp.Invalid e) {
            addField(parent, "totp", "One-time code (TOTP)", "Not a valid TOTP secret", false, false);
            return;
        }
        LinearLayout row = Ui.row(this);
        row.setPadding(0, Ui.dp(this, 8), 0, Ui.dp(this, 8));
        LinearLayout texts = Ui.column(this);
        texts.addView(Ui.label(this, t.steam ? "Steam Guard code" : "One-time code"));
        final TextView code = Ui.text(this, "", 24, R.color.accent);
        code.setTypeface(Typeface.MONOSPACE, Typeface.BOLD);
        code.setLetterSpacing(0.12f);
        texts.addView(code);
        row.addView(texts, Ui.weight(1));
        final TextView left = Ui.text(this, "", 14, R.color.text2);
        left.setPadding(0, 0, Ui.dp(this, 8), 0);
        row.addView(left);
        Button copy = Ui.flat(this, "Copy");
        copy.setOnClickListener(new View.OnClickListener() {
            public void onClick(View b) { Clip.copy(ItemActivity.this, "Code", t.code(System.currentTimeMillis()), true); }
        });
        row.addView(copy);
        parent.addView(row);
        tick = new Runnable() {
            public void run() {
                long now = System.currentTimeMillis();
                String c = t.code(now);
                code.setText(c.length() == 6 ? c.substring(0, 3) + " " + c.substring(3) : c);
                int r = t.remaining(now);
                left.setText(r + "s");
                left.setTextColor(getColor(r <= 5 ? R.color.danger : R.color.text2));
                main.postDelayed(this, 1000 - now % 1000);
            }
        };
        tick.run();
    }

    @Override
    public boolean onCreateOptionsMenu(Menu menu) {
        Item it = item();
        if (it == null) return true;
        if (it.trashed() == 0) {
            menu.add(0, 1, 0, "Edit").setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS);
            menu.add(0, 2, 1, it.fav() ? "Remove from favourites" : "Add to favourites");
            menu.add(0, 3, 2, "Move to trash");
        } else {
            menu.add(0, 4, 0, "Restore").setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS);
            menu.add(0, 5, 1, "Delete forever");
        }
        return true;
    }

    @Override
    public boolean onOptionsItemSelected(MenuItem mi) {
        final Vault v = repo.vault();
        Item it = item();
        if (v == null || it == null) return true;
        try {
            long now = System.currentTimeMillis();
            switch (mi.getItemId()) {
                case android.R.id.home: finish(); return true;
                case 1: startActivity(new Intent(this, EditActivity.class).putExtra("id", id)); return true;
                case 2: commit(v.update(id, Json.map("fav", !it.fav()), now)); return true;
                case 3: commit(v.trash(Arrays.asList(id), now)); finish(); return true;
                case 4: commit(v.restore(Arrays.asList(id), now)); return true;
                case 5:
                    new AlertDialog.Builder(this).setTitle("Delete forever?")
                            .setMessage("“" + it.name() + "” is removed from every device. This cannot be undone.")
                            .setPositiveButton("Delete", new DialogInterface.OnClickListener() {
                                public void onClick(DialogInterface d, int w) {
                                    try {
                                        commit(v.purge(Arrays.asList(id)));
                                        finish();
                                    } catch (Exception e) {
                                        Toast.makeText(ItemActivity.this, "Could not delete: " + e.getMessage(), Toast.LENGTH_LONG).show();
                                    }
                                }
                            }).setNegativeButton("Cancel", null).show();
                    return true;
            }
        } catch (Exception e) {
            Toast.makeText(this, "Could not save: " + e.getMessage(), Toast.LENGTH_LONG).show();
        }
        return super.onOptionsItemSelected(mi);
    }

    private void commit(List<MapOp> ops) throws Exception {
        repo.commit(ops);
        render();
    }
}
