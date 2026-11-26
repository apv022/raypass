package app.raypass;

import android.app.Application;

/** Process start: loads the shared URL suffix and equivalent-domain lists and the ciphertext cache and starts syncing with Sol when connected. */
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
        // The domains that are one account (../urlmatch/equivalent.txt), shared the same way.
        try (java.io.InputStream in = getResources().openRawResource(R.raw.equivalent)) {
            app.raypass.core.UrlMatch.loadEquivalent(in);
        } catch (Exception e) {
            android.util.Log.w("Raypass", "equivalent domain list unreadable: using the built-in one");
        }
        Repo.get(this).startSync();
    }
}
