package app.raypass;

import android.app.AlertDialog;
import android.content.ActivityNotFoundException;
import android.content.DialogInterface;
import android.content.Intent;
import android.graphics.Typeface;
import android.os.Bundle;
import android.text.Editable;
import android.text.TextWatcher;
import android.view.Gravity;
import android.view.Menu;
import android.view.MenuItem;
import android.view.View;
import android.view.ViewGroup;
import android.widget.AdapterView;
import android.widget.BaseAdapter;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.HorizontalScrollView;
import android.widget.LinearLayout;
import android.widget.ListView;
import android.widget.ScrollView;
import android.widget.TextView;

import app.raypass.core.Item;
import app.raypass.core.Vault;

import java.util.ArrayList;
import java.util.List;

/**
 * The launcher screen. Routes on {@link Repo.State}: connect to Sol, wait for the first sync, set
 * up a vault, unlock, or the vault list (search, type filters, favourites, trash) with add, the
 * generator, settings and lock in the action bar. Re-renders on every Repo change.
 */
public class MainActivity extends BaseActivity {
    private static final int CONNECT = 1;
    private static final String[] FILTERS = {"All", "Favourites", "Logins", "Notes", "Cards", "Identities", "Wallets", "Trash"};
    private static final String[] FILTER_TYPES = {"", "", "login", "note", "card", "identity", "wallet", ""};

    private Repo.State shown;
    private int filter;
    private String query = "";
    private String connectError = "";
    private ItemsAdapter adapter;
    private TextView banner, empty;
    private LinearLayout chips;

    @Override
    boolean needsVault() { return false; }

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        if (b != null) {
            filter = b.getInt("filter", 0);
            query = b.getString("query", "");
        }
    }

    @Override
    protected void onSaveInstanceState(Bundle out) {
        super.onSaveInstanceState(out);
        out.putInt("filter", filter);
        out.putString("query", query); // a search term, not a secret
    }

    @Override
    protected void onResume() {
        super.onResume();
        repo.startSync();
        render(false);
    }

    @Override
    void refresh() { render(false); }

    private void render(boolean force) {
        Repo.State s = repo.state();
        if (s != shown || force && s != Repo.State.UNLOCKED) {
            shown = s;
            invalidateOptionsMenu();
            switch (s) {
                case NOT_CONNECTED: setContentView(scroll(connectView())); setTitle("Raypass"); break;
                case WAITING: setContentView(scroll(waitingView())); setTitle("Raypass"); break;
                case SETUP: setContentView(scroll(unlockView(true))); setTitle("Raypass"); break;
                case LOCKED: setContentView(scroll(unlockView(false))); setTitle("Raypass"); break;
                case UNLOCKED: setContentView(vaultView()); setTitle("Vault"); break;
            }
        }
        if (s == Repo.State.UNLOCKED) {
            updateBanner();
            reload();
        } else if (s == Repo.State.WAITING && banner != null) {
            banner.setText(repo.connError().isEmpty() ? "Reading your vault from Sol…" : repo.connError() + ". Retrying…");
        }
    }

    private View scroll(View v) {
        ScrollView s = new ScrollView(this);
        s.setFillViewport(true);
        s.addView(v);
        return s;
    }

    private View connectView() {
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 24);
        col.setPadding(pad, Ui.dp(this, 48), pad, pad);
        col.setGravity(Gravity.CENTER_HORIZONTAL);
        TextView logo = Ui.text(this, "🔑", 48, R.color.text);
        col.addView(logo);
        TextView t = Ui.title(this, "Raypass");
        t.setGravity(Gravity.CENTER);
        col.addView(t, Ui.matchWrap(this, 8));
        TextView sub = Ui.text(this, "A password manager that syncs through your own Sol. Sol carries only ciphertext between your devices; your master password never leaves this phone.\n\nRaypass needs the Sol app on this phone. Connect once and approve it in Sol.", 15, R.color.text2);
        sub.setGravity(Gravity.CENTER);
        col.addView(sub, Ui.matchWrap(this, 8));
        Button go = Ui.button(this, "Connect to Sol", true);
        go.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { connect(); } });
        col.addView(go, Ui.matchWrap(this, 28));
        String e = !connectError.isEmpty() ? connectError : repo.connError();
        if (!e.isEmpty()) col.addView(Ui.text(this, e, 14, R.color.danger), Ui.matchWrap(this, 12));
        return col;
    }

    private View waitingView() {
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 24);
        col.setPadding(pad, Ui.dp(this, 64), pad, pad);
        col.setGravity(Gravity.CENTER_HORIZONTAL);
        col.addView(new android.widget.ProgressBar(this));
        banner = Ui.text(this, "Reading your vault from Sol…", 15, R.color.text2);
        banner.setGravity(Gravity.CENTER);
        col.addView(banner, Ui.matchWrap(this, 16));
        TextView note = Ui.text(this, "Raypass reads your keys from Sol before it offers to create a vault, so a phone never starts a second vault next to the one you already have.", 13, R.color.text2);
        note.setGravity(Gravity.CENTER);
        col.addView(note, Ui.matchWrap(this, 16));
        Button again = Ui.flat(this, "Connect again");
        again.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { connect(); } });
        col.addView(again, Ui.matchWrap(this, 16));
        return col;
    }

    private View unlockView(boolean setup) {
        return UnlockView.build(this, setup, new UnlockView.Done() {
            public void unlocked() { render(true); }
        });
    }

    /** The CONNECT request (Sol's android/README.md "For Android developers"). */
    static Intent connectIntent() {
        return new Intent("app.sol.action.CONNECT")
                .putExtra("app_id", "raypass")
                .putExtra("name", "Raypass")
                .putExtra("description", "Password manager. Stores your vault, encrypted on this phone, in your Sol feeds.")
                .putExtra("scopes", new String[]{"feeds"});
    }

    /** Handles a CONNECT result: null when connected, else a message for the person. */
    static String connectResult(Repo repo, int res, Intent data) {
        if (res == RESULT_OK && data != null && data.getStringExtra("token") != null && data.getStringExtra("api") != null) {
            repo.onConnected(data.getStringExtra("api"), data.getStringExtra("token"));
            return null;
        }
        String err = data == null ? "canceled" : String.valueOf(data.getStringExtra("error"));
        switch (err) {
            case "denied": return "You did not allow Raypass in Sol.";
            case "not_set_up": return "Sol is not set up yet. Open Sol, set it up, then connect.";
            case "canceled": return "Connecting was canceled.";
            default: return "Sol could not connect Raypass (" + err + ").";
        }
    }

    private void connect() {
        try {
            startActivityForResult(connectIntent(), CONNECT);
        } catch (ActivityNotFoundException e) {
            connectError = "Sol is not installed on this phone. Install Sol, set it up, then connect.";
            render(true);
        }
    }

    @Override
    protected void onActivityResult(int req, int res, Intent data) {
        super.onActivityResult(req, res, data);
        if (req != CONNECT) return;
        String e = connectResult(repo, res, data);
        connectError = e == null ? "" : e;
        render(true);
    }

    // ---- the vault list ----

    private View vaultView() {
        FrameLayout frame = new FrameLayout(this);
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 16);
        banner = Ui.text(this, "", 13, R.color.text2);
        banner.setPadding(pad, Ui.dp(this, 6), pad, Ui.dp(this, 6));
        banner.setBackgroundColor(getColor(R.color.surface2));
        col.addView(banner);
        EditText search = Ui.input(this, "Search", false);
        search.setText(query);
        search.addTextChangedListener(new TextWatcher() {
            public void beforeTextChanged(CharSequence s, int a, int b, int c) {}
            public void onTextChanged(CharSequence s, int a, int b, int c) {}
            public void afterTextChanged(Editable s) {
                query = s.toString();
                reload();
            }
        });
        LinearLayout.LayoutParams sp = Ui.matchWrap(this, 8);
        sp.leftMargin = sp.rightMargin = pad;
        col.addView(search, sp);
        HorizontalScrollView hs = new HorizontalScrollView(this);
        hs.setHorizontalScrollBarEnabled(false);
        chips = Ui.row(this);
        chips.setPadding(pad, Ui.dp(this, 10), pad, Ui.dp(this, 6));
        hs.addView(chips);
        col.addView(hs);
        drawChips();
        ListView list = new ListView(this);
        list.setDivider(null);
        list.setClipToPadding(false);
        list.setPadding(0, 0, 0, Ui.dp(this, 88));
        adapter = new ItemsAdapter();
        list.setAdapter(adapter);
        list.setOnItemClickListener(new AdapterView.OnItemClickListener() {
            public void onItemClick(AdapterView<?> p, View v, int pos, long id) {
                startActivity(new Intent(MainActivity.this, ItemActivity.class).putExtra("id", adapter.items.get(pos).id));
            }
        });
        empty = Ui.text(this, "", 15, R.color.text2);
        empty.setGravity(Gravity.CENTER);
        empty.setPadding(pad, Ui.dp(this, 48), pad, pad);
        FrameLayout listFrame = new FrameLayout(this);
        listFrame.addView(list);
        listFrame.addView(empty, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT));
        col.addView(listFrame, new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1));
        frame.addView(col);
        Button add = Ui.button(this, "+  New", true);
        add.setElevation(Ui.dp(this, 6));
        add.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { chooseType(); } });
        FrameLayout.LayoutParams ap = new FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, Ui.dp(this, 52), Gravity.BOTTOM | Gravity.END);
        ap.setMargins(pad, pad, pad, Ui.dp(this, 20));
        frame.addView(add, ap);
        return frame;
    }

    private void drawChips() {
        chips.removeAllViews();
        for (int i = 0; i < FILTERS.length; i++) {
            final int k = i;
            TextView c = Ui.chip(this, FILTERS[i], i == filter);
            c.setOnClickListener(new View.OnClickListener() {
                public void onClick(View v) {
                    filter = k;
                    drawChips();
                    reload();
                }
            });
            LinearLayout.LayoutParams p = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
            p.rightMargin = Ui.dp(this, 8);
            chips.addView(c, p);
        }
    }

    private void updateBanner() {
        if (banner == null) return;
        String msg;
        if (!repo.connected()) msg = repo.connError().isEmpty() ? "Not connected to Sol: changes stay on this phone." : repo.connError();
        else if (!repo.online()) msg = (repo.connError().isEmpty() ? "Offline" : repo.connError()) + (repo.pending() > 0 ? " · " + repo.pending() + " change(s) waiting" : "");
        else msg = repo.pending() > 0 ? "Syncing " + repo.pending() + " change(s)…" : "";
        int orphans = repo.session().orphaned();
        if (orphans > 0) msg = (msg.isEmpty() ? "" : msg + "\n") + orphans + " entries belong to another vault set up elsewhere and cannot be read here.";
        banner.setText(msg);
        banner.setVisibility(msg.isEmpty() ? View.GONE : View.VISIBLE);
        if (!repo.connected()) {
            banner.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { connect(); } });
        } else {
            banner.setOnClickListener(null);
        }
    }

    private void reload() {
        Vault v = repo.vault();
        if (v == null || adapter == null) return;
        boolean trash = filter == FILTERS.length - 1;
        adapter.items = v.list(query, FILTER_TYPES[filter], trash, filter == 1);
        adapter.notifyDataSetChanged();
        int[] counts = v.counts();
        if (!adapter.items.isEmpty()) empty.setText("");
        else if (!query.trim().isEmpty()) empty.setText("Nothing matches “" + query.trim() + "”.");
        else if (trash) empty.setText("The trash is empty.");
        else if (counts[0] == 0) empty.setText("Your vault is empty.\nTap + New to add a login, a note, a card, an identity or a wallet.");
        else empty.setText("Nothing here yet.");
        empty.setVisibility(adapter.items.isEmpty() ? View.VISIBLE : View.GONE);
    }

    private void chooseType() {
        final String[] labels = new String[Item.TYPES.size()];
        for (int i = 0; i < labels.length; i++) labels[i] = Ui.typeLabel(Item.TYPES.get(i));
        new AlertDialog.Builder(this).setTitle("New item").setItems(labels, new DialogInterface.OnClickListener() {
            public void onClick(DialogInterface d, int which) {
                startActivity(new Intent(MainActivity.this, EditActivity.class).putExtra("type", Item.TYPES.get(which)));
            }
        }).show();
    }

    @Override
    public boolean onCreateOptionsMenu(Menu menu) {
        if (repo.state() == Repo.State.UNLOCKED) {
            menu.add(0, 1, 0, "Lock").setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS);
            menu.add(0, 2, 1, "Generator");
            menu.add(0, 3, 2, "Settings");
        } else if (repo.state() == Repo.State.LOCKED) {
            menu.add(0, 3, 2, "Settings");
        }
        return true;
    }

    @Override
    public boolean onOptionsItemSelected(MenuItem item) {
        switch (item.getItemId()) {
            case 1: repo.lock(); return true;
            case 2: startActivity(new Intent(this, GeneratorActivity.class)); return true;
            case 3: startActivity(new Intent(this, SettingsActivity.class)); return true;
        }
        return super.onOptionsItemSelected(item);
    }

    private final class ItemsAdapter extends BaseAdapter {
        List<Item> items = new ArrayList<>();

        public int getCount() { return items.size(); }

        public Object getItem(int i) { return items.get(i); }

        public long getItemId(int i) { return i; }

        public View getView(int i, View convert, ViewGroup parent) {
            Item it = items.get(i);
            LinearLayout row = Ui.row(MainActivity.this);
            int pad = Ui.dp(MainActivity.this, 16);
            row.setPadding(pad, Ui.dp(MainActivity.this, 10), pad, Ui.dp(MainActivity.this, 10));
            TextView av = Ui.avatar(MainActivity.this, it.name(), it.type);
            int s = Ui.dp(MainActivity.this, 40);
            row.addView(av, new LinearLayout.LayoutParams(s, s));
            LinearLayout texts = Ui.column(MainActivity.this);
            texts.setPadding(Ui.dp(MainActivity.this, 14), 0, 0, 0);
            TextView name = Ui.text(MainActivity.this, it.name(), 16, R.color.text);
            name.setSingleLine(true);
            name.setEllipsize(android.text.TextUtils.TruncateAt.END);
            name.setTypeface(Typeface.create("sans-serif-medium", Typeface.NORMAL));
            texts.addView(name);
            String sub = it.subtitle();
            if (sub.isEmpty()) sub = Ui.typeLabel(it.type);
            TextView st = Ui.text(MainActivity.this, sub, 13, R.color.text2);
            st.setSingleLine(true);
            st.setEllipsize(android.text.TextUtils.TruncateAt.END);
            texts.addView(st);
            row.addView(texts, Ui.weight(1));
            if (it.fav()) row.addView(Ui.text(MainActivity.this, "★", 18, R.color.accent));
            return row;
        }
    }
}
