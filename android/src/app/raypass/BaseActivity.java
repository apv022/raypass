package app.raypass;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.view.WindowManager;

/**
 * Every Raypass window: FLAG_SECURE (no screenshots, no recents thumbnail of a secret), activity
 * counts as use for auto-lock, and screens that need the vault close themselves when it locks.
 */
abstract class BaseActivity extends Activity implements Repo.Listener {
    Repo repo;

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_SECURE);
        repo = Repo.get(this);
    }

    /** True for screens that show vault data (they finish when the vault locks). */
    boolean needsVault() { return true; }

    @Override
    protected void onResume() {
        super.onResume();
        repo.addListener(this);
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
        repo.removeListener(this);
    }

    @Override
    public void onUserInteraction() {
        super.onUserInteraction();
        repo.touch();
    }

    @Override
    public void changed() {
        if (needsVault() && repo.vault() == null) backToMain();
        else refresh();
    }

    /** Re-render from current state. */
    void refresh() {}

    void backToMain() {
        startActivity(new Intent(this, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP));
        finish();
    }
}
