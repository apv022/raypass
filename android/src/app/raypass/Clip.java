package app.raypass;

import android.content.ClipData;
import android.content.ClipDescription;
import android.content.ClipboardManager;
import android.content.Context;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.os.PersistableBundle;
import android.widget.Toast;

/**
 * Copying to the clipboard. Secrets are marked sensitive ({@code EXTRA_IS_SENSITIVE}, honoured by
 * Android 13+ which then hides the preview) and cleared after {@code s.clip_clear} seconds — only
 * when the clipboard still holds our copy, or when Android no longer lets us read it (background).
 */
final class Clip {
    private Clip() {}

    private static final String EXTRA_IS_SENSITIVE = "android.content.extra.IS_SENSITIVE";
    private static final Handler main = new Handler(Looper.getMainLooper());
    private static Runnable pending;
    private static long copiedAt;

    static void copy(final Context c, String label, String value, boolean secret) {
        final ClipboardManager cm = (ClipboardManager) c.getSystemService(Context.CLIPBOARD_SERVICE);
        if (cm == null) return;
        ClipData clip = ClipData.newPlainText(secret ? "Raypass" : label, value);
        if (secret) {
            PersistableBundle extras = new PersistableBundle();
            extras.putBoolean(EXTRA_IS_SENSITIVE, true);
            clip.getDescription().setExtras(extras);
        }
        cm.setPrimaryClip(clip);
        if (Build.VERSION.SDK_INT < 33) Toast.makeText(c, label + " copied", Toast.LENGTH_SHORT).show();
        if (!secret) return;
        int secs = 30;
        app.raypass.core.Vault v = Repo.get(c).vault();
        if (v != null) secs = v.clipClear();
        if (secs <= 0) return;
        if (pending != null) main.removeCallbacks(pending);
        final long stamp = copiedAt = System.currentTimeMillis();
        pending = new Runnable() {
            public void run() {
                pending = null;
                ClipDescription d = cm.getPrimaryClipDescription();
                // d == null: we may not read it from the background; clear rather than leave a secret.
                // Otherwise clear only our own copy (same label and a timestamp at or after it).
                if (d != null && !(d.getLabel() != null && d.getLabel().toString().equals("Raypass") && d.getTimestamp() >= stamp - 1000)) return;
                if (Build.VERSION.SDK_INT >= 28) cm.clearPrimaryClip();
                else cm.setPrimaryClip(ClipData.newPlainText("", ""));
            }
        };
        main.postDelayed(pending, secs * 1000L);
    }
}
