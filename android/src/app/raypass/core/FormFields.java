package app.raypass.core;

import java.util.ArrayList;
import java.util.List;
import java.util.Locale;

/**
 * Autofill field heuristics, independent of Android's AssistStructure so they run in JDK tests.
 * The Android service copies each ViewNode's attributes into a {@link Field} (in traversal order)
 * and asks {@link #parse} which are the username, password and one-time-code fields.
 *
 * Order of evidence: explicit autofill hints (the app or page said so), then the HTML input type
 * and autocomplete attribute, then the inputType variation, then words in the resource id, HTML
 * name/id and hint text. A username is only taken when there is a password field or the field
 * says it is a username, so search boxes are not filled.
 */
public final class FormFields {
    private FormFields() {}

    /** What a field is. */
    public enum Kind { NONE, USERNAME, PASSWORD, NEW_PASSWORD, OTP }

    // android.text.InputType values (copied: this class must not depend on android.*).
    static final int CLASS_MASK = 0x0f, VARIATION_MASK = 0xff0, CLASS_TEXT = 1, CLASS_NUMBER = 2,
            TEXT_PASSWORD = 0x80, TEXT_VISIBLE_PASSWORD = 0x90, TEXT_WEB_PASSWORD = 0xe0, NUMBER_PASSWORD = 0x10,
            TEXT_EMAIL = 0x20, TEXT_WEB_EMAIL = 0xd0;

    /** The attributes of one input field we look at. */
    public static final class Field {
        /** The platform's handle (an AutofillId on Android). */
        public final Object id;
        /** View.getAutofillHints() (may be null). */
        public String[] hints;
        /** View input type (0 if unknown). */
        public int inputType;
        /** The resource entry name (e.g. "et_password"). */
        public String idEntry = "";
        /** The hint text shown in the empty field. */
        public String hint = "";
        /** HTML attributes for WebView/browser fields ("" when absent). */
        public String htmlType = "", htmlName = "", htmlId = "", htmlAutocomplete = "";
        /** Whether the field is visible and editable (non-editable fields are skipped). */
        public boolean editable = true;
        /** The current text (for saves; never logged). */
        public String text;
        /**
         * The web domain and scheme of the page or frame the field is in ("" outside web
         * content): an iframe from another site has its own, see {@link AutofillTarget#resolve}.
         */
        public String webDomain = "", webScheme = "";
        /** Whether the field has input focus (it decides which frame a request is for). */
        public boolean focused;

        /** A field with a platform handle. */
        public Field(Object id) { this.id = id; }
    }

    /** The fields found: each may be null. */
    public static final class Form {
        /** The username / email field. */
        public Field username;
        /** The (current) password field. */
        public Field password;
        /** A second password (new password / confirm), kept for saves. */
        public Field newPassword;
        /** A one-time-code field. */
        public Field otp;

        /** Reports whether there is anything to fill (a login, or a one-time code on its own step). */
        public boolean fillable() { return username != null || password != null || otp != null; }

        /** The fields to fill or save, in order. */
        public List<Field> all() {
            List<Field> out = new ArrayList<>();
            if (username != null) out.add(username);
            if (password != null) out.add(password);
            if (newPassword != null) out.add(newPassword);
            if (otp != null) out.add(otp);
            return out;
        }
    }

    private static boolean has(String s, String... words) {
        String l = s.toLowerCase(Locale.ROOT);
        for (String w : words) if (l.contains(w)) return true;
        return false;
    }

    /** Classifies one field on its own. */
    public static Kind classify(Field f) {
        if (!f.editable) return Kind.NONE;
        if (f.hints != null) {
            for (String h : f.hints) {
                if (h == null) continue;
                String l = h.toLowerCase(Locale.ROOT);
                if (l.equals("newpassword") || l.equals("new-password")) return Kind.NEW_PASSWORD;
                if (l.equals("password") || l.equals("current-password")) return Kind.PASSWORD;
                if (l.contains("otp") || l.equals("one-time-code")) return Kind.OTP;
                if (l.equals("username") || l.equals("emailaddress") || l.equals("email") || l.equals("newusername")) return Kind.USERNAME;
            }
        }
        String ac = f.htmlAutocomplete.toLowerCase(Locale.ROOT);
        if (ac.contains("new-password")) return Kind.NEW_PASSWORD;
        if (ac.contains("current-password")) return Kind.PASSWORD;
        if (ac.contains("one-time-code")) return Kind.OTP;
        if (ac.contains("username") || ac.equals("email")) return Kind.USERNAME;
        String ht = f.htmlType.toLowerCase(Locale.ROOT);
        if (ht.equals("password")) return Kind.PASSWORD;
        if (ht.equals("hidden") || ht.equals("submit") || ht.equals("button") || ht.equals("checkbox") || ht.equals("radio")
                || ht.equals("search") || ht.equals("file") || ht.equals("image") || ht.equals("reset")) return Kind.NONE;
        int cls = f.inputType & CLASS_MASK, var = f.inputType & VARIATION_MASK;
        String words = f.idEntry + " " + f.htmlName + " " + f.htmlId + " " + f.hint;
        // Before the inputType: code boxes are often numberPassword (a PIN-style input), and an
        // OTP field taken for the password would get the password filled into it.
        if (has(words, "otp", "one-time", "2fa", "totp", "verification code", "auth code", "authcode", "mfa")) return Kind.OTP;
        if (cls == CLASS_TEXT && (var == TEXT_PASSWORD || var == TEXT_VISIBLE_PASSWORD || var == TEXT_WEB_PASSWORD)) return Kind.PASSWORD;
        if (cls == CLASS_NUMBER && var == NUMBER_PASSWORD) return Kind.PASSWORD;
        if (has(words, "search", "captcha", "coupon", "promo", "zip", "postal")) return Kind.NONE;
        // A phone field is not a username, unless it also takes one: X's first sign-in step asks
        // for "Phone, email, or username".
        if (has(words, "phone") && !has(words, "user", "email", "e-mail", "login")) return Kind.NONE;
        if (has(words, "password", "passwd", "passcode", "pwd", "pass_", "kennwort", "contraseña", "mot de passe", "mot_de_passe", "senha", "parola")) {
            if (has(words, "new", "confirm", "repeat", "again", "retype")) return Kind.NEW_PASSWORD;
            return Kind.PASSWORD;
        }
        if (cls == CLASS_TEXT && (var == TEXT_EMAIL || var == TEXT_WEB_EMAIL)) return Kind.USERNAME;
        if (ht.equals("email")) return Kind.USERNAME;
        if (has(words, "user", "login", "email", "e-mail", "account", "benutzer", "usuario", "identifiant", "nickname")) return Kind.USERNAME;
        return Kind.NONE;
    }

    /** Text-ish field that might hold a username when it sits right before a password. */
    private static boolean texty(Field f) {
        if (!f.editable) return false;
        if (has(f.idEntry + " " + f.htmlName + " " + f.htmlId + " " + f.hint, "search", "captcha", "coupon", "promo")) return false;
        String ht = f.htmlType.toLowerCase(Locale.ROOT);
        if (!ht.isEmpty()) return ht.equals("text") || ht.equals("email") || ht.equals("tel");
        return (f.inputType & CLASS_MASK) == CLASS_TEXT;
    }

    /** Finds the login fields among fields given in traversal order. */
    public static Form parse(List<Field> fields) {
        Form form = new Form();
        Kind[] kinds = new Kind[fields.size()];
        int firstPass = -1;
        for (int i = 0; i < fields.size(); i++) {
            kinds[i] = classify(fields.get(i));
            Field f = fields.get(i);
            switch (kinds[i]) {
                case PASSWORD:
                    if (form.password == null) { form.password = f; firstPass = i; }
                    else if (form.newPassword == null) form.newPassword = f;
                    break;
                case NEW_PASSWORD:
                    if (form.password == null && form.newPassword == null) { form.password = f; firstPass = i; }
                    else if (form.newPassword == null) form.newPassword = f;
                    break;
                case OTP:
                    if (form.otp == null) form.otp = f;
                    break;
                default:
                    break;
            }
        }
        // The username: the last field before the password that says it is one; else the nearest
        // text field before the password; else (no password: a username-only first step) the first
        // field that says it is a username.
        if (firstPass >= 0) {
            for (int i = firstPass - 1; i >= 0; i--) if (kinds[i] == Kind.USERNAME) { form.username = fields.get(i); break; }
            if (form.username == null) {
                for (int i = firstPass - 1; i >= 0; i--) {
                    if (kinds[i] == Kind.NONE && texty(fields.get(i))) { form.username = fields.get(i); break; }
                    if (kinds[i] != Kind.NONE) break;
                }
            }
        } else {
            for (int i = 0; i < fields.size(); i++) if (kinds[i] == Kind.USERNAME) { form.username = fields.get(i); break; }
        }
        return form;
    }
}
