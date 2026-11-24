package app.raypass;

import android.content.Context;
import android.content.res.ColorStateList;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.graphics.drawable.RippleDrawable;
import android.text.InputType;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;

/**
 * Programmatic view helpers in one Material-ish style (no XML layouts, no AndroidX): colours come
 * from res/values(-night)/colors.xml so dark mode follows the system. Every string from vault data
 * goes into a TextView as plain text (never Html), so nothing in the data is interpreted.
 */
final class Ui {
    private Ui() {}

    static int dp(Context c, float v) {
        return Math.round(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v, c.getResources().getDisplayMetrics()));
    }

    static int color(Context c, int res) { return c.getColor(res); }

    static GradientDrawable rounded(Context c, int fill, float radiusDp, int strokeColor) {
        GradientDrawable d = new GradientDrawable();
        d.setColor(fill);
        d.setCornerRadius(dp(c, radiusDp));
        if (strokeColor != 0) d.setStroke(dp(c, 1), strokeColor);
        return d;
    }

    static LinearLayout column(Context c) {
        LinearLayout l = new LinearLayout(c);
        l.setOrientation(LinearLayout.VERTICAL);
        return l;
    }

    static LinearLayout row(Context c) {
        LinearLayout l = new LinearLayout(c);
        l.setOrientation(LinearLayout.HORIZONTAL);
        l.setGravity(Gravity.CENTER_VERTICAL);
        return l;
    }

    static TextView text(Context c, CharSequence s, float sp, int colorRes) {
        TextView t = new TextView(c);
        t.setText(s);
        t.setTextSize(sp);
        t.setTextColor(color(c, colorRes));
        return t;
    }

    static TextView title(Context c, CharSequence s) {
        TextView t = text(c, s, 22, R.color.text);
        t.setTypeface(Typeface.create("sans-serif-medium", Typeface.NORMAL));
        return t;
    }

    static TextView label(Context c, CharSequence s) {
        TextView t = text(c, s, 12, R.color.text2);
        t.setAllCaps(true);
        t.setLetterSpacing(0.06f);
        return t;
    }

    /** A filled (primary) or tonal button. */
    static Button button(Context c, CharSequence s, boolean primary) {
        Button b = new Button(c);
        b.setText(s);
        b.setAllCaps(false);
        b.setTextSize(15);
        b.setStateListAnimator(null);
        int fill = color(c, primary ? R.color.accent : R.color.accent_soft);
        b.setTextColor(color(c, primary ? R.color.on_accent : R.color.accent));
        b.setBackground(new RippleDrawable(ColorStateList.valueOf(color(c, R.color.outline)), rounded(c, fill, 22, 0), null));
        b.setPadding(dp(c, 18), 0, dp(c, 18), 0);
        b.setMinHeight(dp(c, 44));
        b.setMinimumHeight(dp(c, 44));
        return b;
    }

    /** A borderless text button. */
    static Button flat(Context c, CharSequence s) {
        Button b = new Button(c, null, android.R.attr.borderlessButtonStyle);
        b.setText(s);
        b.setAllCaps(false);
        b.setTextColor(color(c, R.color.accent));
        b.setTextSize(14);
        b.setMinWidth(0);
        b.setMinimumWidth(0);
        b.setPadding(dp(c, 10), 0, dp(c, 10), 0);
        return b;
    }

    static EditText input(Context c, CharSequence hint, boolean password) {
        EditText e = new EditText(c);
        e.setHint(hint);
        e.setTextSize(16);
        e.setTextColor(color(c, R.color.text));
        e.setHintTextColor(color(c, R.color.text2));
        e.setSingleLine(true);
        e.setBackground(rounded(c, color(c, R.color.surface), 12, color(c, R.color.outline)));
        e.setPadding(dp(c, 14), dp(c, 12), dp(c, 14), dp(c, 12));
        if (password) {
            e.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_PASSWORD);
            e.setTypeface(Typeface.MONOSPACE);
            // Our own password fields are not autofill targets for other services.
            e.setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO);
        }
        return e;
    }

    /** A card container. */
    static LinearLayout card(Context c) {
        LinearLayout l = column(c);
        l.setBackground(rounded(c, color(c, R.color.surface), 16, color(c, R.color.outline)));
        l.setPadding(dp(c, 16), dp(c, 12), dp(c, 16), dp(c, 12));
        return l;
    }

    static LinearLayout.LayoutParams lp(int w, int h) { return new LinearLayout.LayoutParams(w, h); }

    static LinearLayout.LayoutParams matchWrap(Context c, int topDp) {
        LinearLayout.LayoutParams p = new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        p.topMargin = dp(c, topDp);
        return p;
    }

    static LinearLayout.LayoutParams weight(float w) {
        return new LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, w);
    }

    /** A filter chip. */
    static TextView chip(Context c, CharSequence s, boolean on) {
        TextView t = text(c, s, 14, on ? R.color.on_accent : R.color.text);
        t.setBackground(rounded(c, color(c, on ? R.color.accent : R.color.surface2), 16, 0));
        t.setPadding(dp(c, 14), dp(c, 7), dp(c, 14), dp(c, 7));
        return t;
    }

    /** A round badge with the first letter of a name, coloured by type. */
    static TextView avatar(Context c, String name, String type) {
        TextView t = text(c, "", 17, R.color.on_accent);
        t.setGravity(Gravity.CENTER);
        t.setTextColor(0xFFFFFFFF);
        setAvatar(t, name, type);
        return t;
    }

    /** Points an avatar at another item (rows are reused while scrolling). */
    static void setAvatar(TextView t, String name, String type) {
        t.setText(name.isEmpty() ? "?" : name.substring(0, Character.charCount(name.codePointAt(0))).toUpperCase());
        int[] palette = {0xFF3E5BD8, 0xFF8E44AD, 0xFF16A085, 0xFFD35400, 0xFFB7950B};
        int i = Math.max(0, app.raypass.core.Item.TYPES.indexOf(type));
        GradientDrawable d = new GradientDrawable();
        d.setShape(GradientDrawable.OVAL);
        d.setColor(palette[i % palette.length]);
        t.setBackground(d);
    }

    static View divider(Context c) {
        View v = new View(c);
        v.setBackgroundColor(color(c, R.color.outline));
        v.setLayoutParams(new LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, Math.max(1, dp(c, 0.5f))));
        return v;
    }

    static String typeLabel(String t) {
        switch (t) {
            case "login": return "Login";
            case "note": return "Secure note";
            case "card": return "Card";
            case "identity": return "Identity";
            case "wallet": return "Crypto wallet";
        }
        return t;
    }

    static String fieldLabel(String f) {
        switch (f) {
            case "user": return "Username";
            case "pass": return "Password";
            case "urls": return "Websites and apps";
            case "totp": return "One-time code (TOTP)";
            case "holder": return "Cardholder";
            case "number": return "Card number";
            case "exp": return "Expiry (MM/YY)";
            case "cvv": return "Security code";
            case "brand": return "Brand";
            case "first": return "First name";
            case "last": return "Last name";
            case "email": return "Email";
            case "phone": return "Phone";
            case "address": return "Address";
            case "company": return "Company";
            case "seed": return "Seed phrase";
            case "privkey": return "Private key";
            case "network": return "Network";
            case "passphrase": return "BIP39 passphrase";
            case "derivation": return "Derivation path";
            case "notes": return "Notes";
            case "folder": return "Folder";
            case "tags": return "Tags";
            case "name": return "Name";
        }
        return f;
    }
}
