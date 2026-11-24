package app.raypass;

import android.app.AlertDialog;
import android.content.ActivityNotFoundException;
import android.content.DialogInterface;
import android.content.Intent;
import android.net.Uri;
import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;
import android.view.MenuItem;
import android.view.View;
import android.view.autofill.AutofillManager;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.Switch;
import android.widget.TextView;
import android.widget.Toast;

import app.raypass.core.Crypto;
import app.raypass.core.Vault;

/**
 * Settings: Sol connection, autofill service, biometric unlock, the synced settings (auto-lock,
 * clipboard clearing), change master password, lock. Reachable while locked (autofill setup, the
 * connection); vault settings show only while unlocked.
 */
public class SettingsActivity extends BaseActivity {
    private static final int CONNECT = 1;
    private static final int[] AUTO_LOCK = {1, 5, 15, 30, 60, 240, 0};
    private static final int[] CLIP = {10, 30, 60, 120, 0};
    private final Handler main = new Handler(Looper.getMainLooper());

    @Override
    boolean needsVault() { return false; }

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        if (getActionBar() != null) getActionBar().setDisplayHomeAsUpEnabled(true);
    }

    @Override
    protected void onResume() {
        super.onResume();
        render();
    }

    @Override
    void refresh() { render(); }

    private LinearLayout section(LinearLayout col, String title) {
        col.addView(Ui.label(this, title), Ui.matchWrap(this, 20));
        LinearLayout c = Ui.card(this);
        col.addView(c, Ui.matchWrap(this, 6));
        return c;
    }

    private void render() {
        ScrollView sv = new ScrollView(this);
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 16);
        col.setPadding(pad, 0, pad, Ui.dp(this, 40));
        sv.addView(col);
        final Vault v = repo.vault();

        LinearLayout sol = section(col, "Sol");
        String st = !repo.connected() ? "Not connected" : repo.online() ? "Connected" : "Connected, offline" + (repo.connError().isEmpty() ? "" : " (" + repo.connError() + ")");
        sol.addView(Ui.text(this, st, 16, R.color.text));
        int[] counts = v == null ? null : v.counts();
        String info = repo.session().entryCount() + " encrypted entries cached on this phone"
                + (repo.pending() > 0 ? ", " + repo.pending() + " waiting to sync" : "")
                + (counts != null ? "\n" + counts[0] + " items, " + counts[1] + " in the trash" : "")
                + (repo.session().orphaned() > 0 ? "\n" + repo.session().orphaned() + " entries of another vault (set up elsewhere at the same time) cannot be read" : "");
        sol.addView(Ui.text(this, info, 13, R.color.text2), Ui.matchWrap(this, 4));
        LinearLayout sr = Ui.row(this);
        Button connect = Ui.flat(this, repo.connected() ? "Connect again" : "Connect to Sol");
        connect.setOnClickListener(new View.OnClickListener() {
            public void onClick(View b) {
                try {
                    startActivityForResult(MainActivity.connectIntent(), CONNECT);
                } catch (ActivityNotFoundException e) {
                    Toast.makeText(SettingsActivity.this, "Sol is not installed", Toast.LENGTH_LONG).show();
                }
            }
        });
        sr.addView(connect);
        if (repo.connected()) {
            Button dis = Ui.flat(this, "Disconnect");
            dis.setOnClickListener(new View.OnClickListener() {
                public void onClick(View b) {
                    repo.disconnect("Disconnected from Sol. The vault stays on this phone.");
                    render();
                }
            });
            sr.addView(dis);
        }
        sol.addView(sr);

        LinearLayout af = section(col, "Autofill");
        AutofillManager am = getSystemService(AutofillManager.class);
        boolean on = am != null && am.hasEnabledAutofillServices();
        af.addView(Ui.text(this, on ? "Raypass fills passwords in apps and browsers." : "Raypass is not your autofill service.", 15, R.color.text));
        if (!on) {
            Button en = Ui.flat(this, "Use Raypass for autofill");
            en.setOnClickListener(new View.OnClickListener() {
                public void onClick(View b) {
                    try {
                        startActivity(new Intent(Settings.ACTION_REQUEST_SET_AUTOFILL_SERVICE).setData(Uri.parse("package:" + getPackageName())));
                    } catch (ActivityNotFoundException e) {
                        Toast.makeText(SettingsActivity.this, "Open Android Settings > Passwords & autofill", Toast.LENGTH_LONG).show();
                    }
                }
            });
            af.addView(en);
        }

        if (v == null) {
            col.addView(Ui.text(this, "Unlock Raypass to change vault settings.", 14, R.color.text2), Ui.matchWrap(this, 20));
            setContentView(sv);
            return;
        }

        LinearLayout sec = section(col, "Security");
        if (Bio.supported(this)) {
            final Switch bio = new Switch(this);
            bio.setText("Unlock with biometrics");
            bio.setTextColor(getColor(R.color.text));
            final String kid = repo.session().keyring() == null ? null : repo.session().keyring().id;
            bio.setChecked(Bio.enabled(this, kid));
            bio.setOnClickListener(new View.OnClickListener() {
                public void onClick(View b) {
                    if (!bio.isChecked()) {
                        Bio.disable(SettingsActivity.this);
                        return;
                    }
                    byte[] vk = repo.session().keyCopy();
                    if (vk == null) {
                        bio.setChecked(false);
                        return;
                    }
                    Bio.enable(SettingsActivity.this, vk, kid, new Bio.Done() {
                        public void ok(byte[] x) { Toast.makeText(SettingsActivity.this, "Biometric unlock is on", Toast.LENGTH_SHORT).show(); }

                        public void failed(String why) {
                            bio.setChecked(false);
                            if (!why.isEmpty()) Toast.makeText(SettingsActivity.this, why, Toast.LENGTH_LONG).show();
                        }
                    });
                }
            });
            sec.addView(bio);
        }
        sec.addView(choice("Lock after", AUTO_LOCK, v.autoLock(), true));
        sec.addView(choice("Clear copied secrets after", CLIP, v.clipClear(), false));
        Button cp = Ui.flat(this, "Change master password");
        cp.setOnClickListener(new View.OnClickListener() { public void onClick(View b) { changePassword(); } });
        sec.addView(cp);
        Button lock = Ui.flat(this, "Lock now");
        lock.setOnClickListener(new View.OnClickListener() {
            public void onClick(View b) {
                repo.lock();
                finish();
            }
        });
        sec.addView(lock);
        setContentView(sv);
    }

    private static String label(int v, boolean minutes) {
        if (v == 0) return "Never";
        if (minutes) return v >= 60 ? (v / 60) + " h" : v + " min";
        return v >= 60 ? (v / 60) + " min" : v + " s";
    }

    private View choice(final String title, final int[] values, final int cur, final boolean autoLock) {
        LinearLayout row = Ui.row(this);
        row.addView(Ui.text(this, title, 15, R.color.text), Ui.weight(1));
        Button b = Ui.flat(this, label(cur, autoLock));
        b.setOnClickListener(new View.OnClickListener() {
            public void onClick(View x) {
                String[] labels = new String[values.length];
                for (int i = 0; i < values.length; i++) labels[i] = label(values[i], autoLock);
                new AlertDialog.Builder(SettingsActivity.this).setTitle(title).setItems(labels, new DialogInterface.OnClickListener() {
                    public void onClick(DialogInterface d, int i) {
                        Vault v = repo.vault();
                        if (v == null) return;
                        try {
                            repo.commit(autoLock ? v.setSettings(values[i], null) : v.setSettings(null, values[i]));
                        } catch (Exception e) {
                            Toast.makeText(SettingsActivity.this, "Could not save", Toast.LENGTH_SHORT).show();
                        }
                        render();
                    }
                }).show();
            }
        });
        row.addView(b);
        return row;
    }

    private void changePassword() {
        LinearLayout col = Ui.column(this);
        int pad = Ui.dp(this, 20);
        col.setPadding(pad, Ui.dp(this, 8), pad, 0);
        final EditText old = Ui.input(this, "Current master password", true);
        final EditText n1 = Ui.input(this, "New master password", true);
        final EditText n2 = Ui.input(this, "Repeat new master password", true);
        col.addView(old);
        col.addView(n1, Ui.matchWrap(this, 8));
        col.addView(n2, Ui.matchWrap(this, 8));
        final TextView note = Ui.text(this, "Your other devices use the new password after they sync. Entries written before stay readable.", 13, R.color.text2);
        col.addView(note, Ui.matchWrap(this, 8));
        final AlertDialog d = new AlertDialog.Builder(this).setTitle("Change master password").setView(col)
                .setPositiveButton("Change", null).setNegativeButton("Cancel", null).create();
        d.setOnShowListener(new DialogInterface.OnShowListener() {
            public void onShow(DialogInterface di) {
                final Button ok = d.getButton(AlertDialog.BUTTON_POSITIVE);
                ok.setOnClickListener(new View.OnClickListener() {
                    public void onClick(View v) {
                        final String o = old.getText().toString(), a = n1.getText().toString();
                        if (a.length() < 8) { note.setText("Use at least 8 characters."); return; }
                        if (!a.equals(n2.getText().toString())) { note.setText("The new passwords differ."); return; }
                        ok.setEnabled(false);
                        note.setText("Changing…");
                        repo.bg.execute(new Runnable() {
                            public void run() {
                                String msg = null;
                                try {
                                    repo.changePassword(o, a);
                                } catch (Crypto.WrongPasswordException e) {
                                    msg = "The current password is wrong.";
                                } catch (Exception e) {
                                    msg = "Could not change the password.";
                                }
                                final String m = msg;
                                main.post(new Runnable() {
                                    public void run() {
                                        ok.setEnabled(true);
                                        if (m != null) {
                                            note.setText(m);
                                        } else {
                                            d.dismiss();
                                            Toast.makeText(SettingsActivity.this, "Master password changed", Toast.LENGTH_SHORT).show();
                                        }
                                    }
                                });
                            }
                        });
                    }
                });
            }
        });
        d.getWindow().addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE);
        d.show();
    }

    @Override
    protected void onActivityResult(int req, int res, Intent data) {
        super.onActivityResult(req, res, data);
        if (req == CONNECT) {
            String e = MainActivity.connectResult(repo, res, data);
            if (e != null) Toast.makeText(this, e, Toast.LENGTH_LONG).show();
            render();
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
