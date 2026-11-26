package app.raypass;

import android.app.assist.AssistStructure;
import android.content.Intent;
import android.os.Bundle;
import android.service.autofill.Dataset;
import android.service.autofill.FillResponse;
import android.view.autofill.AutofillId;
import android.view.autofill.AutofillManager;
import android.widget.ScrollView;
import android.widget.Toast;

/**
 * The small dialog autofill opens while Raypass is locked: unlock (password or biometrics), then
 * either hand the matching datasets back to the autofill framework (mode "fill": a one-time-code
 * screen on its own; login screens use {@link AutofillPickActivity}), fill a one-time code (mode
 * "otp"), or finish a save that was waiting for the unlock (mode "save", the login kept in memory
 * under a token).
 */
public class AutofillAuthActivity extends BaseActivity {
    @Override
    boolean needsVault() { return false; }

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        setFinishOnTouchOutside(false);
        if (repo.vault() != null) {
            done();
            return;
        }
        ScrollView sv = new ScrollView(this);
        sv.addView(UnlockView.build(this, false, new UnlockView.Done() {
            public void unlocked() { done(); }
        }));
        setContentView(sv);
    }

    private void done() {
        String mode = getIntent().getStringExtra("mode");
        if ("save".equals(mode)) {
            String[] s = Repo.takePendingSave(getIntent().getStringExtra("token"));
            if (s != null) {
                try {
                    repo.saveLogin(s[0], s[1], s[2]);
                    Toast.makeText(this, "Saved to Raypass", Toast.LENGTH_SHORT).show();
                } catch (Exception e) {
                    Toast.makeText(this, "Raypass could not save this login", Toast.LENGTH_LONG).show();
                }
                s[2] = null;
            } else {
                // Kept only in memory and only for a while: Raypass restarted or it expired.
                Toast.makeText(this, "This login is no longer waiting to be saved. Save it again.", Toast.LENGTH_LONG).show();
            }
            setResult(RESULT_OK);
            finish();
            return;
        }
        if ("otp".equals(mode)) {
            Dataset d = repo.vault() == null ? null : RaypassAutofillService.otpResult(repo.vault(),
                    getIntent().getStringExtra("item"), (AutofillId) getIntent().getParcelableExtra("field"));
            if (d == null) setResult(RESULT_CANCELED);
            else setResult(RESULT_OK, new Intent().putExtra(AutofillManager.EXTRA_AUTHENTICATION_RESULT, d));
            repo.touch();
            finish();
            return;
        }
        AssistStructure st = getIntent().getParcelableExtra(AutofillManager.EXTRA_ASSIST_STRUCTURE);
        FillResponse r = null;
        if (st != null && repo.vault() != null) {
            AutofillParser p = new AutofillParser(st);
            if (p.url != null) r = RaypassAutofillService.response(this, p, repo.vault(), getIntent().getBundleExtra(AutofillManager.EXTRA_CLIENT_STATE));
        }
        if (r == null) {
            Toast.makeText(this, "No saved login for this app or site", Toast.LENGTH_SHORT).show();
            setResult(RESULT_CANCELED);
        } else {
            setResult(RESULT_OK, new Intent().putExtra(AutofillManager.EXTRA_AUTHENTICATION_RESULT, r));
        }
        finish();
    }
}
