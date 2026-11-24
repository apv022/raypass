package app.raypass;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.view.View;
import android.view.WindowManager;

/**
 * Every Raypass window: FLAG_SECURE (no screenshots, no recents thumbnail of a secret), excluded
 * from other autofill services (they would offer to save what is typed into Raypass's own forms,
 * vault secrets included), activity counts as use for auto-lock, and screens that need the vault
 * close themselves when it locks, also while in the background, so no decrypted value stays on
 * screen or in a view tree after a lock.
 */
abstract class BaseActivity extends Activity implements Repo.Listener {
    Repo repo;
    private boolean resumed;

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
        getWindow().getDecorView().setImportantForAutofill(View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS);
        repo = Repo.get(this);
        repo.addListener(this);
    }

    @Override
    protected void onDestroy() {
        super.onDestroy();
        repo.removeListener(this);
    }

    /** True for screens that show vault data (they finish when the vault locks). */
    boolean needsVault() { return true; }

    /** Whether the activity is in the foreground (between onResume and onPause). */
    boolean isResumedNow() { return resumed; }

    @Override
    protected void onResume() {
        super.onResume();
        resumed = true;
        if (needsVault() && repo.vault() == null) {
            backToMain();
            return;
        }
        repo.touch();
        repo.nudge();
    }

    @Override
    protected void onPause() {
        super.onPause();
        resumed = false;
    }

    @Override
    public void onUserInteraction() {
        super.onUserInteraction();
        repo.touch();
    }

    @Override
    public void changed() {
        if (isFinishing() || isDestroyed()) return;
        if (needsVault() && repo.vault() == null) {
            if (resumed) backToMain();
            else finish(); // locked in the background: drop the screen and its decrypted views
        } else if (resumed) {
            refresh(); // in the background nothing is drawn; onResume renders afresh
        }
    }

    /** Re-render from current state. */
    void refresh() {}

    void backToMain() {
        startActivity(new Intent(this, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP));
        finish();
    }
}
