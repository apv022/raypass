package app.raypass;

import android.app.Activity;
import android.os.Handler;
import android.os.Looper;
import android.view.Gravity;
import android.view.KeyEvent;
import android.view.View;
import android.view.inputmethod.EditorInfo;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ProgressBar;
import android.widget.TextView;

import app.raypass.core.Crypto;

/**
 * The master-password screen, used by the app and by the autofill unlock dialog: setup (password
 * twice, then the vault is created) or unlock (password, or biometrics when turned on). Key
 * derivation runs off the main thread; the password field is cleared when done.
 */
final class UnlockView {
    interface Done { void unlocked(); }

    private UnlockView() {}

    private static long lastAutoPrompt;

    static View build(final Activity a, final boolean setup, final Done done) {
        final Repo repo = Repo.get(a);
        final Handler main = new Handler(Looper.getMainLooper());
        LinearLayout col = Ui.column(a);
        int pad = Ui.dp(a, 24);
        col.setPadding(pad, Ui.dp(a, 32), pad, pad);
        col.setGravity(Gravity.CENTER_HORIZONTAL);

        TextView logo = Ui.text(a, "🔑", 44, R.color.text);
        logo.setGravity(Gravity.CENTER);
        col.addView(logo);
        TextView title = Ui.title(a, setup ? "Create your vault" : "Raypass is locked");
        title.setGravity(Gravity.CENTER);
        col.addView(title, Ui.matchWrap(a, 8));
        TextView sub = Ui.text(a, setup
                ? "Choose a master password. It encrypts your vault on every device; Sol only ever carries ciphertext. It cannot be recovered, so keep it somewhere safe."
                : "Enter your master password.", 14, R.color.text2);
        sub.setGravity(Gravity.CENTER);
        col.addView(sub, Ui.matchWrap(a, 6));

        final EditText pw = Ui.input(a, "Master password", true);
        col.addView(pw, Ui.matchWrap(a, 24));
        final EditText pw2 = Ui.input(a, "Repeat master password", true);
        if (setup) col.addView(pw2, Ui.matchWrap(a, 10));
        final TextView err = Ui.text(a, "", 14, R.color.danger);
        col.addView(err, Ui.matchWrap(a, 8));
        final ProgressBar spin = new ProgressBar(a);
        spin.setVisibility(View.GONE);
        final Button go = Ui.button(a, setup ? "Create vault" : "Unlock", true);
        col.addView(go, Ui.matchWrap(a, 8));
        col.addView(spin, Ui.matchWrap(a, 8));

        final Runnable submit = new Runnable() {
            public void run() {
                final String p = pw.getText().toString();
                if (p.isEmpty()) return;
                if (setup) {
                    if (p.length() < 8) { err.setText("Use at least 8 characters (a passphrase of 4+ words is best)."); return; }
                    if (!p.equals(pw2.getText().toString())) { err.setText("The two passwords differ."); return; }
                }
                err.setText(setup ? "Creating the vault…" : "Unlocking…");
                go.setEnabled(false);
                spin.setVisibility(View.VISIBLE);
                repo.bg.execute(new Runnable() {
                    public void run() {
                        String msg = null;
                        try {
                            if (setup) repo.setup(p);
                            else repo.unlock(p);
                        } catch (Crypto.WrongPasswordException e) {
                            msg = "Wrong password.";
                        } catch (Exception e) {
                            msg = setup ? "Could not create the vault." : "The vault could not be opened (corrupt keyring?).";
                        }
                        final String m = msg;
                        main.post(new Runnable() {
                            public void run() {
                                go.setEnabled(true);
                                spin.setVisibility(View.GONE);
                                if (m == null) {
                                    pw.setText("");
                                    pw2.setText("");
                                    err.setText("");
                                    done.unlocked();
                                } else {
                                    err.setText(m);
                                    pw.selectAll();
                                }
                            }
                        });
                    }
                });
            }
        };
        go.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { submit.run(); } });
        TextView.OnEditorActionListener enter = new TextView.OnEditorActionListener() {
            public boolean onEditorAction(TextView v, int id, KeyEvent e) {
                if (id == EditorInfo.IME_ACTION_DONE || id == EditorInfo.IME_ACTION_GO || (e != null && e.getKeyCode() == KeyEvent.KEYCODE_ENTER)) {
                    submit.run();
                    return true;
                }
                return false;
            }
        };
        (setup ? pw2 : pw).setImeOptions(EditorInfo.IME_ACTION_GO);
        (setup ? pw2 : pw).setOnEditorActionListener(enter);

        if (!setup && repo.session().keyring() != null && Bio.enabled(a, repo.session().keyring().id)) {
            final Button bio = Ui.button(a, "Unlock with biometrics", false);
            col.addView(bio, Ui.matchWrap(a, 10));
            final Runnable ask = new Runnable() {
                public void run() {
                    Bio.unlock(a, new Bio.Done() {
                        public void ok(byte[] vk) {
                            try {
                                repo.unlockWithKey(vk);
                                done.unlocked();
                            } catch (Exception e) {
                                Bio.disable(a);
                                err.setText("Biometric unlock no longer matches this vault. Use your master password.");
                            }
                        }

                        public void failed(String why) { err.setText(why); }
                    });
                }
            };
            bio.setOnClickListener(new View.OnClickListener() { public void onClick(View v) { ask.run(); } });
            // Offer it at once, but not again right after a cancel (the prompt pauses the activity).
            if (System.currentTimeMillis() - lastAutoPrompt > 5000) {
                lastAutoPrompt = System.currentTimeMillis();
                main.post(ask);
            }
        } else {
            pw.requestFocus();
        }
        return col;
    }
}
