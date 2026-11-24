package app.raypass.test;

import static app.raypass.test.T.*;

import app.raypass.core.Totp;

import java.nio.charset.StandardCharsets;

/** TOTP: RFC 6238 appendix B, parsing, Steam. */
final class TotpTest {
    static void register() {
        test("totp: RFC 6238 appendix B (SHA1, SHA256, SHA512)", new T.Case() { public void run() {
            byte[] s1 = "12345678901234567890".getBytes(StandardCharsets.US_ASCII);
            byte[] s256 = "12345678901234567890123456789012".getBytes(StandardCharsets.US_ASCII);
            byte[] s512 = "1234567890123456789012345678901234567890123456789012345678901234".getBytes(StandardCharsets.US_ASCII);
            long[] times = {59, 1111111109, 1111111111, 1234567890, 2000000000, 20000000000L};
            String[] sha1 = {"94287082", "07081804", "14050471", "89005924", "69279037", "65353130"};
            String[] sha256 = {"46119246", "68084774", "67062674", "91819424", "90698825", "77737706"};
            String[] sha512 = {"90693936", "25091201", "99943326", "93441116", "38618901", "47863826"};
            for (int i = 0; i < times.length; i++) {
                eq(sha1[i], Totp.hotp(s1, times[i] / 30, 8, "SHA1"), "sha1 #" + i);
                eq(sha256[i], Totp.hotp(s256, times[i] / 30, 8, "SHA256"), "sha256 #" + i);
                eq(sha512[i], Totp.hotp(s512, times[i] / 30, 8, "SHA512"), "sha512 #" + i);
            }
        }});
        test("totp: bare base32 with spaces, lowercase and padding", new T.Case() { public void run() throws Exception {
            Totp t = Totp.parse(" gezd gnbv gy3t qojq gezd gnbv gy3t qojq== ");
            eq(6, t.digits, "digits");
            eq(30, t.period, "period");
            eq("12345678901234567890", new String(t.secret, StandardCharsets.US_ASCII), "secret");
            eq("287082", t.code(59000), "code");
            eq(1, t.remaining(59000), "remaining");
            eq(30, t.remaining(60000), "remaining at boundary");
        }});
        test("totp: otpauth URI parameters and label", new T.Case() { public void run() throws Exception {
            Totp t = Totp.parse("otpauth://totp/ACME%20Co:alice%40example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=ACME+Co&algorithm=sha-512&digits=8&period=60");
            eq("ACME Co", t.issuer, "issuer");
            eq("alice@example.com", t.account, "account");
            eq("SHA512", t.algorithm, "alg");
            eq(8, t.digits, "digits");
            eq(60, t.period, "period");
        }});
        test("totp: invalid inputs are refused", new T.Case() { public void run() {
            for (final String bad : new String[]{"", "otpauth://hotp/x?secret=GEZDGNBV", "otpauth://totp/x?secret=GEZDGNBV&digits=9",
                    "otpauth://totp/x?secret=GEZDGNBV&algorithm=MD5", "otpauth://totp/x", "not base32!", "A", "otpauth://totp/x?secret=GEZDGNBV&period=0"}) {
                throwsA(Totp.Invalid.class, new T.Case() { public void run() throws Exception { Totp.parse(bad); }}, "refused #" + bad.length());
            }
        }});
        test("totp: Steam Guard codes", new T.Case() { public void run() throws Exception {
            Totp t = Totp.parse("steam://GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ");
            check(t.steam, "steam");
            eq(5, t.digits, "digits");
            eq("VHHQY", t.code(1234567890000L), "shared vector");
            check(Totp.parse("otpauth://totp/Steam:me?secret=GEZDGNBVGY3TQOJQ&issuer=Steam&digits=5").steam, "issuer Steam + 5 digits");
            check(Totp.parse("otpauth://totp/x?secret=GEZDGNBVGY3TQOJQ&encoder=steam").steam, "encoder=steam");
        }});
    }
}
