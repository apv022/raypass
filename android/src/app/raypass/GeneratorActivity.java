package app.raypass;

import android.content.Intent;
import android.graphics.Typeface;
import android.os.Bundle;
import android.view.MenuItem;
import android.view.View;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.CompoundButton;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.SeekBar;
import android.widget.TextView;

import app.raypass.core.Generator;

import java.io.InputStream;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/**
 * raypass.generate on the phone: characters (length, classes, avoid ambiguous) or words from the
 * shared word list (count, separator, capitalize, a number), with the entropy shown. Started with
 * {@code pick=true} it returns the result to the edit form. Needs no vault, so it works locked.
 */
public class GeneratorActivity extends BaseActivity {
    private static List<String> words;
    private Generator gen;
    private boolean wordsMode;
    private int length = 20, count = 5;
    private CheckBox upper, lower, digits, symbols, ambiguous, capitalize, number;
    private EditText separator;
    private TextView out, bits;
    private LinearLayout charsBox, wordsBox;
    private String current = "";

    @Override
    boolean needsVault() { return false; }

    static synchronized List<String> words(android.content.Context c) {
        if (words == null) {
            try (InputStream in = c.getResources().openRawResource(R.raw.wordlist)) {
                words = Generator.readWords(in);
            } catch (Exception e) {
                words = new ArrayList<>();
            }
        }
        return words;
    }

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        if (getActionBar() != null) getActionBar().setDisplayHomeAsUpEnabled(true);
        gen = new Generator(words(this));
        ScrollView sv = new ScrollView(this);
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 16);
        col.setPadding(pad, pad, pad, Ui.dp(this, 32));
        sv.addView(col);

        LinearLayout card = Ui.card(this);
        out = Ui.text(this, "", 20, R.color.text);
        out.setTypeface(Typeface.MONOSPACE);
        out.setTextIsSelectable(false);
        card.addView(out);
        bits = Ui.text(this, "", 13, R.color.text2);
        card.addView(bits, Ui.matchWrap(this, 6));
        LinearLayout actions = Ui.row(this);
        Button again = Ui.flat(this, "Regenerate");
        again.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { generate(); } });
        Button copy = Ui.flat(this, "Copy");
        copy.setOnClickListener(new View.OnClickListener() {
            public void onClick(View v) { Clip.copy(GeneratorActivity.this, "Password", current, true); }
        });
        actions.addView(again);
        actions.addView(copy);
        if (getIntent().getBooleanExtra("pick", false)) {
            Button use = Ui.button(this, "Use", true);
            use.setOnClickListener(new View.OnClickListener() {
                public void onClick(View v) {
                    setResult(RESULT_OK, new Intent().putExtra("password", current));
                    finish();
                }
            });
            actions.addView(use);
        }
        card.addView(actions, Ui.matchWrap(this, 6));
        col.addView(card);

        LinearLayout modes = Ui.row(this);
        final TextView cm = Ui.chip(this, "Characters", true), wm = Ui.chip(this, "Words", false);
        modes.addView(cm);
        LinearLayout.LayoutParams sp = new LinearLayout.LayoutParams(Ui.dp(this, 8), 1);
        modes.addView(new View(this), sp);
        modes.addView(wm);
        col.addView(modes, Ui.matchWrap(this, 16));

        charsBox = Ui.column(this);
        final TextView lenLabel = Ui.label(this, "Length: " + length);
        charsBox.addView(lenLabel, Ui.matchWrap(this, 12));
        SeekBar len = new SeekBar(this);
        len.setMax(128 - 4);
        len.setProgress(length - 4);
        len.setOnSeekBarChangeListener(new SeekBar.OnSeekBarChangeListener() {
            public void onProgressChanged(SeekBar s, int p, boolean user) {
                length = p + 4;
                lenLabel.setText("Length: " + length);
                generate();
            }
            public void onStartTrackingTouch(SeekBar s) {}
            public void onStopTrackingTouch(SeekBar s) {}
        });
        charsBox.addView(len);
        CompoundButton.OnCheckedChangeListener regen = new CompoundButton.OnCheckedChangeListener() {
            public void onCheckedChanged(CompoundButton b, boolean on) { generate(); }
        };
        upper = check(charsBox, "A–Z", true, regen);
        lower = check(charsBox, "a–z", true, regen);
        digits = check(charsBox, "0–9", true, regen);
        symbols = check(charsBox, "Symbols (!#$%…)", true, regen);
        ambiguous = check(charsBox, "Avoid look-alikes (Il1O0o…)", false, regen);
        col.addView(charsBox);

        wordsBox = Ui.column(this);
        final TextView cntLabel = Ui.label(this, "Words: " + count);
        wordsBox.addView(cntLabel, Ui.matchWrap(this, 12));
        SeekBar cnt = new SeekBar(this);
        cnt.setMax(20 - 3);
        cnt.setProgress(count - 3);
        cnt.setOnSeekBarChangeListener(new SeekBar.OnSeekBarChangeListener() {
            public void onProgressChanged(SeekBar s, int p, boolean user) {
                count = p + 3;
                cntLabel.setText("Words: " + count);
                generate();
            }
            public void onStartTrackingTouch(SeekBar s) {}
            public void onStopTrackingTouch(SeekBar s) {}
        });
        wordsBox.addView(cnt);
        wordsBox.addView(Ui.label(this, "Separator"), Ui.matchWrap(this, 10));
        separator = Ui.input(this, "-", false);
        separator.setText("-");
        separator.addTextChangedListener(new android.text.TextWatcher() {
            public void beforeTextChanged(CharSequence s, int a, int b2, int c) {}
            public void onTextChanged(CharSequence s, int a, int b2, int c) {}
            public void afterTextChanged(android.text.Editable s) { generate(); }
        });
        wordsBox.addView(separator, Ui.matchWrap(this, 4));
        capitalize = check(wordsBox, "Capitalize", false, regen);
        number = check(wordsBox, "Add a number", false, regen);
        wordsBox.setVisibility(View.GONE);
        col.addView(wordsBox);

        cm.setOnClickListener(new View.OnClickListener() {
            public void onClick(View v) { setMode(false, cm, wm); }
        });
        wm.setOnClickListener(new View.OnClickListener() {
            public void onClick(View v) { setMode(true, cm, wm); }
        });
        if (words(this).isEmpty()) wm.setVisibility(View.GONE);
        setContentView(sv);
        generate();
    }

    private void setMode(boolean w, TextView cm, TextView wm) {
        wordsMode = w;
        cm.setBackground(Ui.chip(this, "", !w).getBackground());
        cm.setTextColor(getColor(!w ? R.color.on_accent : R.color.text));
        wm.setBackground(Ui.chip(this, "", w).getBackground());
        wm.setTextColor(getColor(w ? R.color.on_accent : R.color.text));
        charsBox.setVisibility(w ? View.GONE : View.VISIBLE);
        wordsBox.setVisibility(w ? View.VISIBLE : View.GONE);
        generate();
    }

    private CheckBox check(LinearLayout col, String label, boolean on, CompoundButton.OnCheckedChangeListener l) {
        CheckBox c = new CheckBox(this);
        c.setText(label);
        c.setTextColor(getColor(R.color.text));
        c.setChecked(on);
        c.setOnCheckedChangeListener(l);
        col.addView(c);
        return c;
    }

    private void generate() {
        if (out == null || upper == null || number == null) return; // still building
        try {
            Generator.Result r = wordsMode
                    ? gen.words(count, separator.getText().toString(), capitalize.isChecked(), number.isChecked())
                    : gen.chars(length, upper.isChecked(), lower.isChecked(), digits.isChecked(), symbols.isChecked(), ambiguous.isChecked());
            current = r.password;
            out.setText(current);
            String strength = r.entropyBits >= 100 ? "excellent" : r.entropyBits >= 75 ? "strong" : r.entropyBits >= 50 ? "fair" : "weak";
            bits.setText(String.format(Locale.ROOT, "%.0f bits of entropy · %s", r.entropyBits, strength));
        } catch (RuntimeException e) {
            current = "";
            out.setText("");
            bits.setText(e.getMessage());
        }
    }

    @Override
    public boolean onOptionsItemSelected(MenuItem mi) {
        if (mi.getItemId() == android.R.id.home) {
            finish();
            return true;
        }
        return super.onOptionsItemSelected(mi);
    }
}
