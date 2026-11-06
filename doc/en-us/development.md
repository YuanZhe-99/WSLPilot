# Development

## Environment

- Windows 11 x64 or ARM64 with WSL installed (`wsl --version` must work).
- Go 1.27.3 or later (the `go` directive in `go.mod`; newer toolchains download it automatically).
- No cgo, no C toolchain, no Node or Bun: the interface is MyGo native UI.

The MyGo CLI is a module tool: `go tool mygo` runs the version pinned in `go.mod`.

## Commands

```bash
go tool mygo dev        # build, run, and rebuild on change
go tool mygo build      # package into build/windows-<arch>/
go tool mygo generate   # regenerate locales/catalog_gen.go after editing locales/*.json
go run ./tools/icon     # re-render resources/icon.png after editing resources/icon.svg
go test ./...           # unit and view tests (no window, no WSL needed)
go run ./tools/catalog  # merge locales/messages.tsv into the catalogs, then run mygo generate
scripts/build.sh -skip-installer windows/arm64   # package the app, agent and CLI
WSLPILOT_LIVE=1 go test ./internal/...           # live tests on wslpilot-test-* objects
WSLPILOT_SHOTS=<folder> go test -run Screenshots ./internal/gui   # render every page to PNG
```

The app icon is a penguin drawn for WSLPilot. After editing `resources/icon.svg`, run `go run ./tools/icon`, which writes
`resources/icon.png` and the 64-pixel sidebar and tray icons.

## Full local gate

```bash
gofmt -l cmd internal tools
go vet ./...
go test ./...
CGO_ENABLED=0 go test ./...
go tool mygo vet .
go tool mygo i18n check
go run ./tools/checkdocs
```

## Live tests

Tests that run the real `wsl.exe` / `wslc.exe` are gated by `WSLPILOT_LIVE=1` and may only create
and remove resources named `wslpilot-test-*`.

They need WSL 3.0.3.0 or later with WSL Containers and the `alpine:3.20` image. They cover the
volume tar round trip (`internal/volume`), distribution backup to every kind and restore as a new
distribution (`internal/backup`, needs `wslpilot-test-d1`), and a Compose project up, update and
down (`internal/compose`), and a Compose project backup with its folder and volume restored
(`internal/core`, `wslpilot-test-pbk`).

## Driving the window

A build tagged `e2e` links MyGo's automation plugin (`cmd/wslpilot/automation_e2e.go`); release
builds leave it out. Started with `MYGO_AUTOMATION` naming a socket, the window can be read as
text and driven with `mygo-automation` (snapshot, press, fill, screenshot). Set `WSLPILOT_DATA` to
an empty folder to keep its settings and projects apart from yours, and set a backup folder in that
copy's settings: the default backup folder is in your profile either way.

```bash
go build -tags e2e -o .dev/wslpilot-check.exe ./cmd/wslpilot
go build -o .dev/mygo-automation.exe github.com/egoist/mygo/plugins/automation/cmd/mygo-automation
```

## Capturing fixtures

Capture from Git Bash so bytes are kept as the tool wrote them, into
`internal/<adapter>/testdata/<upstream version>/`, and scrub user names:

```bash
wsl.exe --version > internal/wsl/testdata/3.0.3.0/version-en-utf16.txt
wslc.exe info --format json > internal/wslc/testdata/3.0.3.0/info.json
```
