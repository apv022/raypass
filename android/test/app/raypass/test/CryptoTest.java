package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.Clock;
import app.raypass.core.Crypto;
import app.raypass.core.Json;
import app.raypass.core.Keyring;
import app.raypass.core.MapOp;
import app.raypass.core.Session;
import app.raypass.core.Stamp;
import app.raypass.core.VaultEntry;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.Map;

import javax.crypto.SecretKeyFactory;
import javax.crypto.spec.PBEKeySpec;

/** Crypto: the shared vectors in testdata/vectors.json (the interop contract with Go), plus RFC vectors. */
final class CryptoTest {
    static void register(final String root) {
        final Map<String, Object>[] vec = new Map[1];
        test("vectors.json is present and parses", new T.Case() { public void run() throws Exception {
            byte[] b = Files.readAllBytes(Paths.get(root, "testdata", "vectors.json"));
            vec[0] = Json.obj(Json.parse(new String(b, StandardCharsets.UTF_8)));
            check(vec[0] != null && vec[0].containsKey("keyring") && vec[0].containsKey("vault_entry"), "vectors shape");
        }});

        test("vectors: kdf password UTF-8 bytes and master key", new T.Case() { public void run() throws Exception {
            Map<String, Object> k = Json.obj(vec[0].get("kdf"));
            String pw = Json.str(k, "password", null);
            eq(Json.str(k, "password_utf8_hex", ""), Crypto.hex(pw.getBytes(StandardCharsets.UTF_8)), "utf8");
            byte[] mk = Crypto.deriveKey(pw, Crypto.unb64(Json.str(k, "salt_b64", "")), (int) Json.num(k, "iter", 0));
            eq(Json.str(k, "mk_hex", ""), Crypto.hex(mk), "mk");
        }});

        test("vectors: keyring wrap, check and body are byte-identical", new T.Case() { public void run() throws Exception {
            Map<String, Object> v = Json.obj(vec[0].get("keyring"));
            Map<String, Object> kdf = Json.obj(vec[0].get("kdf"));
            Map<String, Object> body = Json.obj(v.get("body"));
            Map<String, Object> bk = Json.obj(body.get("kdf"));
            byte[] vk = Crypto.unhex(Json.str(v, "vk_hex", ""));
            eq(Crypto.AAD_KEYRING, Json.str(v, "aad", ""), "aad");
            eq(Crypto.CHECK_LABEL, Json.str(v, "check_label", ""), "label");
            Keyring kr = Keyring.wrap(Json.str(kdf, "password", ""), vk, Json.str(body, "id", ""),
                    Crypto.unb64(Json.str(bk, "salt", "")), Crypto.unb64(Json.str(v, "nonce_b64", "")),
                    (int) Json.num(bk, "iter", 0), Stamp.fromJson(body.get("stamp")));
            eq(Json.str(v, "wrapped_b64", ""), kr.wrapped, "wrapped");
            eq(Json.str(v, "check_b64", ""), kr.check, "check");
            eq(Json.write(body), Json.write(kr.toJson()), "body JSON");
            // And the other way: parse the Go body and unwrap it.
            Keyring parsed = Keyring.parse(body);
            check(parsed.valid(1), "valid");
            check(!parsed.valid(Crypto.MIN_ITER), "iter 1000 is below the minimum");
            check(Arrays.equals(vk, parsed.unwrap(Json.str(kdf, "password", ""))), "unwrap");
            throwsA(Crypto.WrongPasswordException.class, new T.Case() { public void run() throws Exception {
                Keyring.parse(Json.obj(Json.obj(vec[0].get("keyring")).get("body"))).unwrap("wrong");
            }}, "wrong password");
        }});

        test("vectors: vault entry encrypts to the same ciphertext and decrypts", new T.Case() { public void run() throws Exception {
            Map<String, Object> v = Json.obj(vec[0].get("vault_entry"));
            byte[] vk = Crypto.unhex(Json.str(v, "vk_hex", ""));
            byte[] plain = Crypto.unb64(Json.str(v, "plaintext_b64", ""));
            eq(Json.str(v, "plaintext", ""), new String(plain, StandardCharsets.UTF_8), "plaintext forms agree");
            VaultEntry e = VaultEntry.encrypt(vk, Json.str(v, "keyring_id", ""), plain, Crypto.unb64(Json.str(v, "nonce_b64", "")));
            eq(Json.str(v, "c_b64", ""), e.c, "ciphertext");
            eq(Json.write(v.get("body")), Json.write(e.toJson()), "body JSON");
            List<MapOp> ops = VaultEntry.parse(v.get("body")).decrypt(vk);
            eq(6, ops.size(), "ops");
            check(ops.get(5).delete, "tombstone");
            // Our serializer reproduces Go's bytes for the same ops (escaping of < & > included).
            eq(Json.str(v, "plaintext", ""), MapOp.batchJson(ops), "batch JSON is byte-identical");
        }});

        test("vectors: encrypted export decrypts", new T.Case() { public void run() throws Exception {
            Map<String, Object> v = Json.obj(vec[0].get("export"));
            Map<String, Object> f = Json.obj(v.get("file"));
            Map<String, Object> kdf = Json.obj(f.get("kdf"));
            byte[] k = Crypto.deriveKey(Json.str(v, "password", ""), Crypto.unb64(Json.str(kdf, "salt", "")), (int) Json.num(kdf, "iter", 0));
            byte[] p = Crypto.open(k, Crypto.unb64(Json.str(f, "nonce", "")), Crypto.unb64(Json.str(f, "data", "")), Crypto.AAD_EXPORT);
            eq(Json.str(v, "plaintext_b64", ""), Crypto.b64(p), "export plaintext");
            byte[] c = Crypto.seal(k, Crypto.unb64(Json.str(v, "nonce_b64", "")), p, Crypto.AAD_EXPORT);
            eq(Json.str(f, "data", ""), Crypto.b64(c), "export ciphertext");
        }});

        test("vectors: TOTP codes", new T.Case() { public void run() throws Exception {
            int n = 0;
            for (Map<String, Object> t : Json.objects(vec[0].get("totp"))) {
                app.raypass.core.Totp p;
                if (t.containsKey("uri")) {
                    p = app.raypass.core.Totp.parse(Json.str(t, "uri", ""));
                } else {
                    String alg = Json.str(t, "algorithm", "SHA1");
                    byte[] sec = Json.str(t, "secret_ascii", "").getBytes(StandardCharsets.US_ASCII);
                    String code = app.raypass.core.Totp.hotp(sec, Json.num(t, "time", 0) / Json.num(t, "period", 30),
                            (int) Json.num(t, "digits", 6), alg);
                    eq(Json.str(t, "code", ""), code, "code #" + n++);
                    continue;
                }
                eq(Json.str(t, "code", ""), p.code(Json.num(t, "time", 0) * 1000), "uri code #" + n++);
                eq((int) Json.num(t, "digits", 0), p.digits, "digits");
                eq((int) Json.num(t, "period", 0), p.period, "period");
            }
            check(n >= 18, "all totp vectors ran");
        }});

        test("PBKDF2-HMAC-SHA256 RFC 7914 vector", new T.Case() { public void run() throws Exception {
            byte[] dk = Crypto.pbkdf2("passwd".getBytes(StandardCharsets.UTF_8), "salt".getBytes(StandardCharsets.UTF_8), 1, 64);
            eq("55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783",
                    Crypto.hex(dk), "dk");
        }});

        test("PBKDF2 matches the platform SecretKeyFactory", new T.Case() { public void run() throws Exception {
            String pw = "pässwörd 🔑";
            byte[] salt = Crypto.random(16);
            SecretKeyFactory f = SecretKeyFactory.getInstance("PBKDF2WithHmacSHA256");
            byte[] want = f.generateSecret(new PBEKeySpec(pw.toCharArray(), salt, 2000, 256)).getEncoded();
            check(Arrays.equals(want, Crypto.deriveKey(pw, salt, 2000)), "same key");
        }});

        test("GCM rejects tampering, wrong AAD and wrong key", new T.Case() { public void run() throws Exception {
            final byte[] k = Crypto.random(32), n = Crypto.random(12);
            final byte[] c = Crypto.seal(k, n, "hello".getBytes(StandardCharsets.UTF_8), Crypto.AAD_VAULT);
            eq(5 + 16, c.length, "tag appended");
            c[0] ^= 1;
            throwsA(Crypto.CorruptException.class, new T.Case() { public void run() throws Exception { Crypto.open(k, n, c, Crypto.AAD_VAULT); }}, "tamper");
            c[0] ^= 1;
            throwsA(Crypto.CorruptException.class, new T.Case() { public void run() throws Exception { Crypto.open(k, n, c, Crypto.AAD_KEYRING); }}, "aad");
            throwsA(Crypto.CorruptException.class, new T.Case() { public void run() throws Exception { Crypto.open(Crypto.random(32), n, c, Crypto.AAD_VAULT); }}, "key");
        }});

        test("session: setup, write, lock, unlock elsewhere, change password", new T.Case() { public void run() throws Exception {
            Session a = new Session(new Clock("dev-a"));
            final Map<String, Object> kr = a.setup("pw one", Crypto.MIN_ITER);
            check(Json.write(kr).indexOf("pw one") < 0, "no password in the keyring body");
            String[] id = new String[1];
            List<Object> urls = new ArrayList<>(Arrays.asList((Object) "https://example.com"));
            List<MapOp> ops = a.vault().add("login", Json.map("name", "Example", "user", "ann", "pass", "s3cret", "urls", urls), 1000, id);
            List<Map<String, Object>> bodies = a.encrypt(ops);
            for (Map<String, Object> b : bodies) check(Json.write(b).indexOf("s3cret") < 0 && Json.write(b).indexOf("Example") < 0, "ciphertext only");
            for (Map<String, Object> b : bodies) a.addVaultBody(b);
            // Another device sees the same feeds.
            final Session b = new Session(new Clock("dev-b"));
            b.addKeysBody(Json.parse(Json.write(kr)));
            for (Map<String, Object> body : bodies) b.addVaultBody(Json.parse(Json.write(body)));
            check(b.initialized() && !b.unlocked(), "locked");
            throwsA(Crypto.WrongPasswordException.class, new T.Case() { public void run() throws Exception { b.unlock("nope"); }}, "wrong");
            b.unlock("pw one");
            eq("s3cret", b.vault().get(id[0]).str("pass"), "decrypted");
            check(b.vault().map().sameContent(a.vault().map()), "same state");
            Map<String, Object> kr2 = b.changePassword("pw one", "pw two", 0);
            a.addKeysBody(Json.parse(Json.write(kr2)));
            a.lock();
            check(a.vault() == null, "wiped");
            throwsA(Crypto.WrongPasswordException.class, new T.Case() { public void run() throws Exception {
                Session c = new Session(new Clock("c"));
                c.addKeysBody(kr);
                c.addKeysBody(Json.parse(Json.write(Json.obj(Json.parse(Json.write(kr))))));
                c.unlock("pw two");
            }}, "old keyring alone needs the old password");
            a.unlock("pw two");
            eq("Example", a.vault().get(id[0]).name(), "after change");
        }});

        test("session: a second vault set up offline is orphaned, the first wins", new T.Case() { public void run() throws Exception {
            Session a = new Session(new Clock("a", new Clock.Wall() { public long now() { return 1000; } }));
            Session b = new Session(new Clock("b", new Clock.Wall() { public long now() { return 2000; } }));
            Map<String, Object> ka = a.setup("x", Crypto.MIN_ITER), kb = b.setup("y", Crypto.MIN_ITER);
            String[] id = new String[1];
            for (Map<String, Object> body : b.encrypt(b.vault().add("note", Json.map("name", "N"), 1, id))) a.addVaultBody(body);
            a.addKeysBody(kb);
            b.addKeysBody(ka);
            eq(Keyring.parse(ka).id, b.keyring().id, "earliest vault wins");
            check(!b.unlocked(), "b's key no longer opens the vault");
            eq(1, a.orphaned(), "b's entry is orphaned in a");
        }});

        test("keyring: a later keyring of the vault with another check is ignored (Go ChooseKeyring)", new T.Case() { public void run() throws Exception {
            byte[] vk = new byte[32], other = new byte[32];
            other[0] = 1;
            byte[] salt = new byte[16], nonce = new byte[12];
            String id = "0123456789abcdef";
            Keyring first = Keyring.wrap("pw", vk, id, salt, nonce, Crypto.MIN_ITER, new Stamp(1, 0, "a"));
            Keyring changed = Keyring.wrap("pw2", vk, id, salt, nonce, Crypto.MIN_ITER, new Stamp(2, 0, "a"));
            Keyring stray = Keyring.wrap("pw3", other, id, salt, nonce, Crypto.MIN_ITER, new Stamp(3, 0, "b"));
            Keyring.Choice c = Keyring.choose(java.util.Arrays.asList(stray, first, changed), Crypto.MIN_ITER);
            check(c.current == changed, "the latest keyring with the vault's check is current");
            eq(0, c.others.size(), "the stray keyring is not another vault");
            c = Keyring.choose(java.util.Arrays.asList(stray, first), Crypto.MIN_ITER);
            check(c.current == first, "a stray keyring does not replace the first");
        }});
    }
}
