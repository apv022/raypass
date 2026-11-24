# Raypass. Standard library Go and Sol's SDK only; no Node, no build step for the UIs.

GO ?= go
EXE := $(shell $(GO) env GOEXE)

.PHONY: check build dev browser-test extension package wordlist clean

# gofmt + go vet (Linux, Windows, macOS) + tests with -race. Must pass before a change is done.
check:
	@out=$$(gofmt -l $$(git ls-files '*.go')); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...
	GOOS=windows $(GO) vet ./...
	GOOS=darwin $(GO) vet ./...
	$(GO) test -race ./...

# bin/raypass (the Sol app) and bin/raypass-native (the browser extension's native host).
build:
	$(GO) build -trimpath -o bin/raypass$(EXE) ./cmd/raypass
	$(GO) build -trimpath -o bin/raypass-native$(EXE) ./cmd/raypass-native

# The UI against an in-memory Sol: one person, two devices, a seeded vault.
dev:
	$(GO) run ./cmd/raypass-dev

# Every */test/index.html in headless Chromium.
browser-test:
	$(GO) run ./cmd/browsertest

# build/raypass-extension-{chrome,firefox}.zip
extension:
	$(GO) run ./cmd/extpack

# build/raypass-app/: a directory `sol app install build/raypass-app` accepts: app.json, the
# raypass binary its exec names, and icon.svg. Cross-build with GOOS/GOARCH; for Windows the
# exec of the copied app.json becomes raypass.exe.
package:
	mkdir -p build/raypass-app
	$(GO) build -trimpath -o build/raypass-app/raypass$(EXE) ./cmd/raypass
	sed 's/"exec": "raypass"/"exec": "raypass$(EXE)"/' app.json > build/raypass-app/app.json
	cp ui/icon.svg build/raypass-app/icon.svg
	@echo "Install with: sol app install build/raypass-app"

# Replace gen/wordlist.txt (a generated fallback when the EFF list could not be fetched) with the
# EFF large word list, checking its shape (7776 "ddddd<TAB>word" lines) first.
wordlist:
	curl -fsSL -o gen/wordlist.txt.new https://www.eff.org/files/2016/07/18/eff_large_wordlist.txt
	@n=$$(grep -cE '^[1-6]{5}	[a-z-]+$$' gen/wordlist.txt.new); if [ "$$n" != 7776 ]; then echo "unexpected word list ($$n lines)"; rm -f gen/wordlist.txt.new; exit 1; fi
	mv gen/wordlist.txt.new gen/wordlist.txt
	$(GO) test ./gen

clean:
	rm -rf bin build
