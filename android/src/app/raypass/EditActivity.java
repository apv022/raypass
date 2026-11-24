package app.raypass;

import android.content.Intent;
import android.graphics.Typeface;
import android.os.Bundle;
import android.text.InputType;
import android.view.Menu;
import android.view.MenuItem;
import android.view.View;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.Toast;

import app.raypass.core.Item;
import app.raypass.core.Json;
import app.raypass.core.MapOp;
import app.raypass.core.Totp;
import app.raypass.core.Vault;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Add or edit an item of any type. On save only changed fields are written (an emptied field is
 * cleared), so concurrent edits of other fields on other devices are kept; a changed login
 * password moves the old one into the history (Vault.update does that).
 */
public class EditActivity extends BaseActivity {
    private static final int GENERATE = 2;
    private String id, type;
    private final Map<String, EditText> inputs = new LinkedHashMap<>();
    private CheckBox fav;
    private LinearLayout fieldsBox;
    private final List<View[]> customRows = new ArrayList<>();

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        id = getIntent().getStringExtra("id");
        type = getIntent().getStringExtra("type");
        if (getActionBar() != null) getActionBar().setDisplayHomeAsUpEnabled(true);
        Vault v = repo.vault();
        if (v == null) {
            backToMain();
            return;
        }
        Item it = id == null ? null : v.get(id);
        if (id != null && it == null) {
            finish();
            return;
        }
        if (it != null) type = it.type;
        if (!Item.validType(type)) type = Item.LOGIN;
        setTitle((it == null ? "New " : "Edit ") + Ui.typeLabel(type).toLowerCase());
        build(it);
    }

    private boolean multiline(String f) {
        return f.equals("notes") || f.equals("urls") || f.equals("address") || f.equals("seed");
    }

    private EditText addInput(LinearLayout col, final String f, String value) {
        boolean secret = Item.secretField(f) && !f.equals("totp") && !f.equals("number");
        col.addView(Ui.label(this, Ui.fieldLabel(f)), Ui.matchWrap(this, 14));
        EditText e = Ui.input(this, "", secret);
        if (multiline(f)) {
            e.setSingleLine(false);
            e.setMinLines(f.equals("notes") ? 4 : 2);
            e.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_MULTI_LINE
                    | (f.equals("seed") ? InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS | InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD : 0));
        }
        if (f.equals("urls")) e.setHint("One per line: https://example.com or androidapp://com.example");
        if (f.equals("totp")) e.setHint("otpauth://… URI, base32 secret or steam://…");
        if (f.equals("exp")) e.setHint("MM/YY");
        if (f.equals("tags")) e.setHint("Comma separated");
        if (f.equals("number") || f.equals("cvv")) e.setInputType(InputType.TYPE_CLASS_NUMBER | (f.equals("cvv") ? InputType.TYPE_NUMBER_VARIATION_PASSWORD : 0));
        if (f.equals("email")) e.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS);
        if (f.equals("phone")) e.setInputType(InputType.TYPE_CLASS_PHONE);
        if (Item.secretField(f) || f.equals("totp")) {
            e.setTypeface(Typeface.MONOSPACE);
            e.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
        }
        e.setText(value);
        if (f.equals("pass")) {
            LinearLayout row = Ui.row(this);
            row.addView(e, Ui.weight(1));
            final EditText pe = e;
            Button show = Ui.flat(this, "Show");
            show.setOnClickListener(new View.OnClickListener() {
                public void onClick(View v) {
                    boolean hidden = (pe.getInputType() & InputType.TYPE_TEXT_VARIATION_PASSWORD) == InputType.TYPE_TEXT_VARIATION_PASSWORD;
                    pe.setInputType(InputType.TYPE_CLASS_TEXT | (hidden ? InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD : InputType.TYPE_TEXT_VARIATION_PASSWORD));
                    pe.setTypeface(Typeface.MONOSPACE);
                    ((Button) v).setText(hidden ? "Hide" : "Show");
                }
            });
            row.addView(show);
            Button gen = Ui.flat(this, "Generate");
            gen.setOnClickListener(new View.OnClickListener() {
                public void onClick(View v) {
                    startActivityForResult(new Intent(EditActivity.this, GeneratorActivity.class).putExtra("pick", true), GENERATE);
                }
            });
            row.addView(gen);
            col.addView(row, Ui.matchWrap(this, 4));
        } else {
            col.addView(e, Ui.matchWrap(this, 4));
        }
        inputs.put(f, e);
        return e;
    }

    private void build(Item it) {
        ScrollView sv = new ScrollView(this);
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 16);
        col.setPadding(pad, Ui.dp(this, 4), pad, Ui.dp(this, 40));
        sv.addView(col);
        addInput(col, "name", it == null ? "" : it.str("name"));
        for (String f : Item.typeFields(type)) {
            if (f.equals("history")) continue;
            String v;
            if (f.equals("urls")) v = it == null ? "" : android.text.TextUtils.join("\n", it.urls());
            else v = it == null ? "" : it.str(f);
            addInput(col, f, v);
        }
        addInput(col, "notes", it == null ? "" : it.str("notes"));
        addInput(col, "folder", it == null ? "" : it.str("folder"));
        addInput(col, "tags", it == null ? "" : android.text.TextUtils.join(", ", it.strings("tags")));
        fav = new CheckBox(this);
        fav.setText("Favourite");
        fav.setTextColor(getColor(R.color.text));
        fav.setChecked(it != null && it.fav());
        col.addView(fav, Ui.matchWrap(this, 14));

        col.addView(Ui.label(this, "Custom fields"), Ui.matchWrap(this, 18));
        fieldsBox = Ui.column(this);
        col.addView(fieldsBox);
        if (it != null) for (Map<String, Object> f : it.fields()) addCustom(Json.str(f, "name", ""), Json.str(f, "value", ""), Json.bool(f, "hidden", false));
        Button add = Ui.flat(this, "+ Add field");
        add.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { addCustom("", "", false); } });
        col.addView(add);

        Button save = Ui.button(this, "Save", true);
        save.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { save(); } });
        col.addView(save, Ui.matchWrap(this, 20));
        setContentView(sv);
    }

    private void addCustom(String name, String value, boolean hidden) {
        final LinearLayout box = Ui.card(this);
        final EditText n = Ui.input(this, "Field name", false);
        n.setText(name);
        final EditText v = Ui.input(this, "Value", hidden);
        v.setText(value);
        final CheckBox h = new CheckBox(this);
        h.setText("Hidden");
        h.setTextColor(getColor(R.color.text));
        h.setChecked(hidden);
        Button rm = Ui.flat(this, "Remove");
        final View[] row = {n, v, h};
        rm.setOnClickListener(new View.OnClickListener() {
            public void onClick(View b) {
                fieldsBox.removeView(box);
                customRows.remove(row);
            }
        });
        box.addView(n);
        box.addView(v, Ui.matchWrap(this, 6));
        LinearLayout r = Ui.row(this);
        r.addView(h, Ui.weight(1));
        r.addView(rm);
        box.addView(r);
        fieldsBox.addView(box, Ui.matchWrap(this, 8));
        customRows.add(row);
    }

    private Object valueOf(String f) {
        String s = inputs.get(f).getText().toString();
        if (f.equals("urls") || f.equals("tags")) {
            List<Object> out = new ArrayList<>();
            for (String p : s.split(f.equals("urls") ? "[\\n\\s]+" : "[,\\n]")) if (!p.trim().isEmpty()) out.add(p.trim());
            return out;
        }
        return s;
    }

    private List<Object> customValue() {
        List<Object> out = new ArrayList<>();
        for (View[] r : customRows) {
            String n = ((EditText) r[0]).getText().toString().trim(), v = ((EditText) r[1]).getText().toString();
            if (n.isEmpty() && v.isEmpty()) continue;
            out.add(Json.map("name", n, "value", v, "hidden", ((CheckBox) r[2]).isChecked()));
        }
        return out;
    }

    private static boolean empty(Object v) {
        return v == null || (v instanceof String && ((String) v).isEmpty()) || (v instanceof List && ((List<?>) v).isEmpty());
    }

    private void save() {
        Vault v = repo.vault();
        if (v == null) {
            backToMain();
            return;
        }
        String totp = inputs.containsKey("totp") ? inputs.get("totp").getText().toString().trim() : "";
        if (!totp.isEmpty()) {
            try {
                Totp.parse(totp);
            } catch (Totp.Invalid e) {
                inputs.get("totp").setError("Not a valid TOTP secret");
                return;
            }
        }
        long now = System.currentTimeMillis();
        try {
            List<MapOp> ops;
            if (id == null) {
                Map<String, Object> in = new LinkedHashMap<>();
                for (String f : inputs.keySet()) {
                    Object val = valueOf(f);
                    if (!empty(val)) in.put(f, val);
                }
                if (fav.isChecked()) in.put("fav", true);
                List<Object> cf = customValue();
                if (!cf.isEmpty()) in.put("fields", cf);
                String[] out = new String[1];
                ops = v.add(type, in, now, out);
                id = out[0];
            } else {
                Item it = v.get(id);
                if (it == null) {
                    finish();
                    return;
                }
                Map<String, Object> in = new LinkedHashMap<>();
                for (String f : inputs.keySet()) {
                    Object val = valueOf(f);
                    Object old = it.raw(f);
                    if (empty(val) && empty(old)) continue;
                    if (Json.write(val).equals(Json.write(old))) continue;
                    in.put(f, empty(val) && !f.equals("name") ? null : val);
                }
                if (fav.isChecked() != it.fav()) in.put("fav", fav.isChecked());
                List<Object> cf = customValue();
                if (!Json.write(cf).equals(Json.write(it.raw("fields") == null ? new ArrayList<Object>() : it.raw("fields")))) {
                    in.put("fields", cf.isEmpty() ? null : cf);
                }
                if (in.isEmpty()) {
                    finish();
                    return;
                }
                ops = v.update(id, in, now);
            }
            repo.commit(ops);
            finish();
        } catch (Vault.BadInput e) {
            Toast.makeText(this, e.getMessage(), Toast.LENGTH_LONG).show();
        } catch (Exception e) {
            Toast.makeText(this, "Could not save", Toast.LENGTH_LONG).show();
        }
    }

    @Override
    protected void onActivityResult(int req, int res, Intent data) {
        super.onActivityResult(req, res, data);
        if (req == GENERATE && res == RESULT_OK && data != null && inputs.containsKey("pass")) {
            inputs.get("pass").setText(data.getStringExtra("password"));
        }
    }

    @Override
    public boolean onCreateOptionsMenu(Menu menu) {
        menu.add(0, 1, 0, "Save").setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS);
        return true;
    }

    @Override
    public boolean onOptionsItemSelected(MenuItem mi) {
        if (mi.getItemId() == android.R.id.home) {
            finish();
            return true;
        }
        if (mi.getItemId() == 1) {
            save();
            return true;
        }
        return super.onOptionsItemSelected(mi);
    }
}
