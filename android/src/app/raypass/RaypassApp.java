package app.raypass;

import android.app.Application;

/** Process start: loads the ciphertext cache and starts syncing with Sol when connected. */
public class RaypassApp extends Application {
    @Override
    public void onCreate() {
        super.onCreate();
        Repo.get(this).startSync();
    }
}
