package app.raypass;

import android.app.Activity;
import android.content.Context;
import android.content.SharedPreferences;
import android.hardware.biometrics.BiometricManager;
import android.hardware.biometrics.BiometricPrompt;
import android.os.Build;
import android.os.CancellationSignal;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyPermanentlyInvalidatedException;
import android.security.keystore.KeyProperties;
import android.util.Base64;

import app.raypass.core.Crypto;

import java.security.KeyStore;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

/**
 * Optional biometric unlock: the vault key wrapped (AES-GCM) by an Android Keystore key that
 * requires biometric authentication for every use and is invalidated when a new fingerprint or
 * face is enrolled. The wrapped key and IV live in private prefs; the Keystore key never leaves
 * the secure hardware. Disabling deletes both. Android 9+ (BiometricPrompt).
 */
final class Bio {
    private Bio() {}

    private static final String ALIAS = "raypass_vault_key_wrap";

    interface Done {
        void ok(byte[] vk);

        void failed(String why);
    }

    private static SharedPreferences prefs(Context c) { return c.getSharedPreferences("bio", Context.MODE_PRIVATE); }

    static boolean supported(Context c) {
        if (Build.VERSION.SDK_INT < 28) return false;
        if (Build.VERSION.SDK_INT >= 30) {
            BiometricManager bm = c.getSystemService(BiometricManager.class);
            return bm != null && bm.canAuthenticate(BiometricManager.Authenticators.BIOMETRIC_STRONG) == BiometricManager.BIOMETRIC_SUCCESS;
        }
        if (Build.VERSION.SDK_INT >= 29) {
            BiometricManager bm = c.getSystemService(BiometricManager.class);
            return bm != null && bm.canAuthenticate() == BiometricManager.BIOMETRIC_SUCCESS;
        }
        return c.getPackageManager().hasSystemFeature("android.hardware.fingerprint");
    }

    /** Enabled for the vault with this keyring id. */
    static boolean enabled(Context c, String keyringId) {
        SharedPreferences p = prefs(c);
        return keyringId != null && keyringId.equals(p.getString("kid", null)) && p.getString("ct", null) != null;
    }

    static void disable(Context c) {
        prefs(c).edit().clear().apply();
        try {
            KeyStore ks = KeyStore.getInstance("AndroidKeyStore");
            ks.load(null);
            ks.deleteEntry(ALIAS);
        } catch (Exception ignored) {
            // nothing to delete
        }
    }

    private static SecretKey key(boolean create) throws Exception {
        KeyStore ks = KeyStore.getInstance("AndroidKeyStore");
        ks.load(null);
        if (!create && ks.containsAlias(ALIAS)) return (SecretKey) ks.getKey(ALIAS, null);
        KeyGenerator g = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
        KeyGenParameterSpec.Builder b = new KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .setUserAuthenticationRequired(true)
                .setInvalidatedByBiometricEnrollment(true);
        if (Build.VERSION.SDK_INT >= 30) b.setUserAuthenticationParameters(0, KeyProperties.AUTH_BIOMETRIC_STRONG);
        g.init(b.build());
        return g.generateKey();
    }

    private static BiometricPrompt prompt(Activity a, String subtitle) {
        BiometricPrompt.Builder b = new BiometricPrompt.Builder(a).setTitle("Raypass").setSubtitle(subtitle);
        if (Build.VERSION.SDK_INT >= 30) b.setAllowedAuthenticators(BiometricManager.Authenticators.BIOMETRIC_STRONG);
        b.setNegativeButton("Use master password", a.getMainExecutor(), new android.content.DialogInterface.OnClickListener() {
            public void onClick(android.content.DialogInterface d, int w) {}
        });
        return b.build();
    }

    /** Wraps vk (a copy the caller owns; wiped here) after a biometric confirmation. */
    static void enable(final Activity a, final byte[] vk, final String keyringId, final Done done) {
        final Cipher c;
        try {
            c = Cipher.getInstance("AES/GCM/NoPadding");
            c.init(Cipher.ENCRYPT_MODE, key(true));
        } catch (Exception e) {
            Crypto.wipe(vk);
            done.failed("Biometric unlock is not available on this device");
            return;
        }
        prompt(a, "Confirm to turn on biometric unlock").authenticate(new BiometricPrompt.CryptoObject(c), new CancellationSignal(),
                a.getMainExecutor(), new BiometricPrompt.AuthenticationCallback() {
                    @Override public void onAuthenticationSucceeded(BiometricPrompt.AuthenticationResult r) {
                        try {
                            Cipher ok = r.getCryptoObject().getCipher();
                            byte[] ct = ok.doFinal(vk);
                            prefs(a).edit().putString("kid", keyringId)
                                    .putString("iv", Base64.encodeToString(ok.getIV(), Base64.NO_WRAP))
                                    .putString("ct", Base64.encodeToString(ct, Base64.NO_WRAP)).apply();
                            done.ok(null);
                        } catch (Exception e) {
                            done.failed("Could not turn on biometric unlock");
                        } finally {
                            Crypto.wipe(vk);
                        }
                    }

                    @Override public void onAuthenticationError(int code, CharSequence msg) {
                        Crypto.wipe(vk);
                        done.failed(String.valueOf(msg));
                    }
                });
    }

    /** Asks for a biometric and unwraps the vault key. */
    static void unlock(final Activity a, final Done done) {
        SharedPreferences p = prefs(a);
        final byte[] iv, ct;
        final Cipher c;
        try {
            iv = Base64.decode(p.getString("iv", ""), Base64.NO_WRAP);
            ct = Base64.decode(p.getString("ct", ""), Base64.NO_WRAP);
            c = Cipher.getInstance("AES/GCM/NoPadding");
            c.init(Cipher.DECRYPT_MODE, key(false), new GCMParameterSpec(128, iv));
        } catch (KeyPermanentlyInvalidatedException e) {
            disable(a);
            done.failed("Biometrics changed on this device: unlock with your master password and turn biometric unlock on again");
            return;
        } catch (Exception e) {
            disable(a);
            done.failed("Biometric unlock is not set up");
            return;
        }
        prompt(a, "Unlock your vault").authenticate(new BiometricPrompt.CryptoObject(c), new CancellationSignal(), a.getMainExecutor(),
                new BiometricPrompt.AuthenticationCallback() {
                    @Override public void onAuthenticationSucceeded(BiometricPrompt.AuthenticationResult r) {
                        try {
                            done.ok(r.getCryptoObject().getCipher().doFinal(ct));
                        } catch (Exception e) {
                            done.failed("Biometric unlock failed");
                        }
                    }

                    @Override public void onAuthenticationError(int code, CharSequence msg) {
                        if (code == BiometricPrompt.BIOMETRIC_ERROR_USER_CANCELED || code == BiometricPrompt.BIOMETRIC_ERROR_CANCELED
                                || code == BiometricPrompt.BIOMETRIC_ERROR_NEGATIVE_BUTTON) {
                            done.failed("");
                        } else {
                            done.failed(String.valueOf(msg));
                        }
                    }
                });
    }
}
