// Command raypass-native is the native messaging host of the Raypass browser extension
// (design/DESIGN.md "Browser extension"). The browser starts it with the extension's origin as
// its argument and talks to it over stdin/stdout with the native messaging framing (a uint32
// length in native byte order, then that many bytes of UTF-8 JSON).
//
//	raypass-native                        the host itself (started by the browser)
//	raypass-native install [--extension-id ID]... [--firefox-id ID] [--path BIN] [--all]
//	raypass-native uninstall
//	raypass-native version
//
// Every request is {"id","action","args"} and every answer {"id","ok":true,"result"} or
// {"id","ok":false,"error":{"code","message"}}. The host forwards only the actions in
// allowedActions, by running `sol do <action> --args - --json` with the arguments on stdin
// (never on the command line, where other users could read them in the process list). Sol's own
// sign-in is the gate: the host holds no secret and no key, and remembers nothing between
// requests.
//
// Invariants: nothing the browser or sol sends is logged (only action names, exit codes and
// error codes go to stderr, which the browser keeps in its own log); an answer larger than the
// browser's 1 MB limit is replaced by an error; a request larger than maxIn ends the session.
package main

import (
	"fmt"
	"os"
)

// version is the host's version, reported by the host-local "ping" action.
const version = "0.1.0"

func main() {
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "install":
			os.Exit(installCmd(args[1:], os.Stdout, os.Stderr))
		case "uninstall":
			os.Exit(uninstallCmd(args[1:], os.Stdout, os.Stderr))
		case "version", "--version", "-v":
			fmt.Println("raypass-native", version)
			return
		case "help", "--help", "-h":
			fmt.Print(usage)
			return
		}
	}
	// Anything else is a browser starting us: Chrome passes the caller's origin
	// ("chrome-extension://<id>/"), Firefox the manifest path and the add-on id.
	h := newHost(loadConfig())
	if err := h.serve(os.Stdin, os.Stdout); err != nil {
		logf("session ended: %v", err)
		os.Exit(1)
	}
}

const usage = `raypass-native: the native messaging host of the Raypass browser extension.

  raypass-native install [--extension-id ID]... [--firefox-id ID] [--path BIN] [--all]
      write the host manifests for Chrome, Chromium, Brave, Edge, Vivaldi and Firefox
      (Linux, macOS; prints the registry commands on Windows). By default only browsers
      whose profile folder exists; --all writes them all.
  raypass-native uninstall
      remove them
  raypass-native version

The browser starts the host itself; it runs "sol do <action> --args - --json".
SOL_BIN picks the sol binary, SOL_DIR its data folder (both are remembered by install).
`

// logf writes a diagnostic line to stderr. Callers pass only action names, codes and errors of
// our own making: never request or response bodies.
func logf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "raypass-native: "+format+"\n", a...)
}
