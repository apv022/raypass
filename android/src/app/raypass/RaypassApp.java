package app.raypass;

import android.app.Application;

/** Process start: loads the shared URL suffix list and the ciphertext cache and starts syncing with Sol when connected. */
public class RaypassApp extends Application {
    @Override
    public void onCreate() {
        super.onCreate();
        // The Go app's public-suffix list (../urlmatch/suffixes.txt), so both match URLs alike.
        try (java.io.InputStream in = getResources().openRawResource(R.raw.suffixes)) {
            app.raypass.core.UrlMatch.loadSuffixes(in);
        } catch (Exception e) {
            android.util.Log.w("Raypass", "suffix list unreadable: using the built-in one");
        }
        Repo.get(this).startSync();
    }
}
