# Package index

| Package | Role | Key declarations |
|---|---|---|
| `cmd/wslpilot` | GUI entry point: single instance, theme, main window | `main` |
| `internal/version` | Release version, kept equal to `mygo.json` | `Version` |
| `internal/upstream` | Upstream versions WSLPilot was tested with (`tested.json`) | `Tested`, `Manifest`, `Compare`, `NewerThanTested` |
| `internal/procexec` | Child processes from argument vectors; output decoding | `Runner`, `Run`, `Decode`, `ExitError` |
| `internal/wsl` | `wsl.exe` adapter | `Client`, `Version`, `ParseVersion`, `ParseNames`, `ErrNoVersion` |
| `internal/wslc` | `wslc.exe` adapter | `Client`, `Info`, `ParseInfo`, `HasNativeCompose` |
| `internal/capability` | One combined probe of WSL, WSLc and native Compose | `Prober`, `Report` |
| `internal/gui` | Native UI: sidebar, pages, dashboard | `App`, `New`, `App.View`, `App.Refresh` |
| `locales` | Generated message accessors from `locales/*.json` | `catalog_gen.go` |
| `tools/checkdocs` | Verifies the `doc/` language mirror | `main` |
| `tools/icon` | Renders the app icon SVG to PNG with MyGo's renderer | `render`, `matte` |
| `tools/deps` | Compares the MyGo and WSL pins with the latest releases | `check` |
