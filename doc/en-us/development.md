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
go test ./...           # unit and view tests (no window, no WSL needed)
```

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

## Capturing fixtures

Capture from Git Bash so bytes are kept as the tool wrote them, into
`internal/<adapter>/testdata/<upstream version>/`, and scrub user names:

```bash
wsl.exe --version > internal/wsl/testdata/3.0.3.0/version-en-utf16.txt
wslc.exe info --format json > internal/wslc/testdata/3.0.3.0/info.json
```
