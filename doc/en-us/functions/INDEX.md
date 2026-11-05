# Package index

| Package | Role | Key declarations |
|---|---|---|
| `cmd/wslpilot` | GUI entry point: single instance, theme, main window | `main` |
| `internal/version` | Release version, kept equal to `mygo.json` | `Version` |
| `internal/procexec` | Child processes from argument vectors; output decoding | `Runner`, `Run`, `Decode`, `ExitError` |
| `internal/wsl` | `wsl.exe` adapter | `Client`, `Version`, `ParseVersion`, `ParseNames`, `ErrNoVersion` |
| `internal/wslc` | `wslc.exe` adapter | `Client`, `Info`, `ParseInfo`, `HasNativeCompose` |
| `internal/capability` | One combined probe of WSL, WSLc and native Compose | `Prober`, `Report` |
| `internal/gui` | Native UI: sidebar, pages, dashboard | `App`, `New`, `App.View`, `App.Refresh` |
| `locales` | Generated message accessors from `locales/*.json` | `catalog_gen.go` |
| `tools/checkdocs` | Verifies the `doc/` language mirror | `main` |
