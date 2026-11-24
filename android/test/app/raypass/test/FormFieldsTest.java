package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.FormFields;
import app.raypass.core.FormFields.Field;
import app.raypass.core.FormFields.Kind;

import java.util.Arrays;

/** Autofill heuristics, independent of AssistStructure. */
final class FormFieldsTest {
    static Field f(String name) { return new Field(name); }

    static void register() {
        test("autofill: explicit hints win", new T.Case() { public void run() {
            Field u = f("u"), p = f("p");
            u.hints = new String[]{"username"};
            p.hints = new String[]{"password"};
            u.idEntry = "password_box"; // misleading id: the hint decides
            eq(Kind.USERNAME, FormFields.classify(u), "username hint");
            eq(Kind.PASSWORD, FormFields.classify(p), "password hint");
            Field n = f("n");
            n.hints = new String[]{"newPassword"};
            eq(Kind.NEW_PASSWORD, FormFields.classify(n), "new password");
            Field o = f("o");
            o.hints = new String[]{"smsOTPCode"};
            eq(Kind.OTP, FormFields.classify(o), "otp");
        }});
        test("autofill: inputType password variations", new T.Case() { public void run() {
            Field p = f("p");
            p.inputType = 0x81; // TEXT | TEXT_VARIATION_PASSWORD
            eq(Kind.PASSWORD, FormFields.classify(p), "text password");
            p.inputType = 0xe1;
            eq(Kind.PASSWORD, FormFields.classify(p), "web password");
            p.inputType = 0x12;
            eq(Kind.PASSWORD, FormFields.classify(p), "number password (PIN)");
            Field e = f("e");
            e.inputType = 0x21;
            eq(Kind.USERNAME, FormFields.classify(e), "email address");
        }});
        test("autofill: HTML attributes from browsers", new T.Case() { public void run() {
            Field a = f("a"), b = f("b"), c = f("c");
            a.htmlType = "email";
            b.htmlType = "password";
            c.htmlAutocomplete = "one-time-code";
            eq(Kind.USERNAME, FormFields.classify(a), "email input");
            eq(Kind.PASSWORD, FormFields.classify(b), "password input");
            eq(Kind.OTP, FormFields.classify(c), "otp input");
            Field h = f("h");
            h.htmlType = "hidden";
            h.htmlName = "password";
            eq(Kind.NONE, FormFields.classify(h), "hidden inputs are never filled");
        }});
        test("autofill: id and hint words", new T.Case() { public void run() {
            Field u = f("u"), p = f("p"), s = f("s");
            u.idEntry = "et_login_name";
            u.inputType = 1;
            p.hint = "Mot de passe";
            p.inputType = 1;
            s.idEntry = "search_src_text";
            s.inputType = 1;
            eq(Kind.USERNAME, FormFields.classify(u), "login id");
            eq(Kind.PASSWORD, FormFields.classify(p), "french hint");
            eq(Kind.NONE, FormFields.classify(s), "search box");
        }});
        test("autofill: form parse picks the username before the password", new T.Case() { public void run() {
            Field search = f("search"), user = f("user"), pass = f("pass"), remember = f("remember");
            search.idEntry = "search";
            search.inputType = 1;
            user.inputType = 1; // a plain text field with no clue at all
            pass.inputType = 0x81;
            remember.editable = false;
            FormFields.Form form = FormFields.parse(Arrays.asList(search, user, pass, remember));
            eq("user", form.username.id, "nearest text field before the password");
            eq("pass", form.password.id, "password");
            check(form.otp == null && form.newPassword == null, "nothing else");
        }});
        test("autofill: sign-up form keeps new and confirm passwords", new T.Case() { public void run() {
            Field e = f("e"), p1 = f("p1"), p2 = f("p2");
            e.htmlType = "email";
            p1.htmlAutocomplete = "new-password";
            p2.htmlType = "password";
            p2.htmlName = "confirm_password";
            FormFields.Form form = FormFields.parse(Arrays.asList(e, p1, p2));
            eq("e", form.username.id, "user");
            eq("p1", form.password.id, "first password");
            eq("p2", form.newPassword.id, "confirm");
        }});
        test("autofill: username-only first step and no-login screens", new T.Case() { public void run() {
            Field u = f("u");
            u.hints = new String[]{"emailAddress"};
            FormFields.Form form = FormFields.parse(Arrays.asList(u));
            eq("u", form.username.id, "identifier step");
            Field t = f("t");
            t.inputType = 1;
            check(!FormFields.parse(Arrays.asList(t)).fillable(), "a lone text box is not a login");
        }});
    }
}
