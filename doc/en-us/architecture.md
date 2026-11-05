# Architecture

## Processes

| Process | Status | Role |
|---|---|---|
| `wslpilot` (`cmd/wslpilot`) | implemented (shell) | The GUI. Runs as the signed-in user, never elevated. Single instance. |
| `wslpilot-agent` | planned | Scheduled backups, restart policies and Compose autostart while the GUI is closed. |
| `wslpilot-cli` | planned | Scripting and debugging entry point over the same packages. |

## Layering

```
gui  →  domain packages (compose, volume, backup, …)  →  adapters (wsl, wslc)  →  procexec
                       ↘  capability  ↗
```

- A package may only import packages to its right. Views never build command arguments.
- **Adapters** (`internal/wsl`, `internal/wslc`) turn upstream commands into Go types. Parsers are
  pure functions over decoded text, tested against fixtures captured from real runs
  (`testdata/<upstream version>/`). Clients take a `procexec.Runner`, so tests substitute recorded
  output.
- **`internal/procexec`** starts child processes from argument vectors (never a shell string),
  with `WSL_UTF8=1`, no console window, and output decoded from UTF-16LE or UTF-8.
- **`internal/capability`** probes WSL, WSLc and native Compose once and hands a `Report` to
  everything that decides what to offer.

## Cross-cutting rules

- **Upstream owns state.** Distributions, containers, images, networks and volumes are always
  read back from the CLI. WSLPilot persists only its own metadata.
- **UI thread.** Views are functions of `gui.App` state. Probes and commands run on goroutines and
  publish results through the window's `Update`; a probe in flight is not started twice.
- **Localization.** Interface strings come from `locales/*.json` (ARB) through the generated
  `locales` package; upstream output is never matched on localized labels.

## Repository layout

| Path | Contents |
|---|---|
| `cmd/wslpilot` | GUI entry point |
| `internal/` | All application packages (see `functions/INDEX.md`) |
| `locales/` | ARB catalogs and the generated `catalog_gen.go` |
| `design/` | Static HTML mockups of the screens, exported from the design canvas (see `design/README.md`) |
| `resources/` | App icon (`icon.svg` is the source, `icon.png` is rendered from it) and packaged resources |
| `tools/checkdocs` | Documentation mirror check |
| `tools/icon` | Renders `resources/icon.svg` to the 1024×1024 `resources/icon.png` |
| `.agents/skills/` | Agent skills (MyGo maintenance) |
