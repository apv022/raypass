package app.raypass.test;

/** Runs every test suite on the JDK: {@code android/test.sh}. Exit status 1 on any failure. */
public final class Main {
    public static void main(String[] args) throws Exception {
        String root = args.length > 0 ? args[0] : "..";
        JsonTest.register();
        CryptoTest.register(root);
        CrdtTest.register();
        VaultTest.register();
        TotpTest.register();
        UrlMatchTest.register();
        FormFieldsTest.register();
        GeneratorTest.register(root);
        System.exit(T.runAll() == 0 ? 0 : 1);
    }
}
