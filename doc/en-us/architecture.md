# Architecture

## Processes

| Process | Status | Role |
|---|---|---|
| `wslpilot` (`cmd/wslpilot`) | implemented | The GUI. Runs as the signed-in user, never elevated. Single instance; closing the window keeps it in the tray unless Settings say Quit. Runs due backup plans every minute and checks for updates daily while it runs (`gui.md`). |
| `wslpilot-agent` (`cmd/wslpilot-agent`) | implemented | `run-due` (Task Scheduler, every 15 minutes) and `watch` (at sign-in: restart policies, Compose autostart, due plans). Built without a console window. |
| `wslpilot-cli` (`cmd/wslpilot-cli`) | implemented | Scripting and debugging: status, lists, backup, catalog, verify, restore as new objects, compact, plans. |
| elevated child | implemented | Only `diskpart` for compaction WSL cannot do itself, started through the UAC prompt (`internal/elevate`). |

## Layering

```
gui, cmd  →  core  →  domain packages  →  adapters (wsl, wslc)  →  procexec
                       compose, backup, volume, scheduler, restart,
                       configdoc, vhdx, wtprofile, jobs, store
```

- A package may only import packages to its right (or beside it in the domain layer, never
  upward). Views never build command arguments; adapters own every argument vector.
- **Adapters** (`internal/wsl`, `internal/wslc`) turn upstream commands into Go types. Parsers are
  pure functions over decoded text, tested against fixtures captured from real runs
  (`testdata/<upstream version>/`). Clients take a `procexec.Runner` and `procexec.Streamer`, so
  tests substitute recorded output.
- **`internal/procexec`** starts child processes from argument vectors (never a shell string),
  with `WSL_UTF8=1`, no console window, output decoded from UTF-16LE or UTF-8, and raw streams for
  tar pipes.
- **`internal/core`** builds the services once for each process (adapters, store, jobs, backup
  runner, scheduler, Compose registry).
- **`internal/capability`** probes WSL, WSLc and native Compose and hands a `Report` to everything
  that decides what to offer.

## Cross-cutting rules

- **Upstream owns state.** Distributions, containers, images, networks and volumes are always
  read back from the CLI. WSLPilot persists only its own metadata ([app-data.md](app-data.md)).
- **UI thread.** Views are functions of `gui.App` state. Commands run on goroutines (`App.async`)
  or as jobs (`internal/jobs`) and publish results through the window's `Update`; views do no I/O.
- **Danger actions** go through one confirmation dialog that offers a backup first (on by
  default), may require typing the object's name, and write the audit log.
- **Long operations are jobs**: progress, cancellation, a log, a final state, and a notification
  when they took long.
- **Localization.** Interface strings come from `locales/*.json` (ARB) through the generated
  `locales` package; `locales/messages.tsv` is the source of most of them (`go run ./tools/catalog`).
  Upstream output is never matched on localized labels.

## Repository layout

| Path | Contents |
|---|---|
| `cmd/wslpilot`, `cmd/wslpilot-agent`, `cmd/wslpilot-cli` | The three programs |
| `internal/` | All application packages (see `functions/INDEX.md`) |
| `locales/` | ARB catalogs, their TSV source and the generated `catalog_gen.go` |
| `design/` | Static HTML mockups of the screens, exported from the design canvas (see `design/README.md`) |
| `resources/` | App icon (`icon.svg` is the source, `icon.png` is rendered from it) and packaged resources |
| `tools/checkdocs` | Documentation mirror check |
| `tools/icon` | Renders `resources/icon.svg` to the 1024×1024 `resources/icon.png` |
| `tools/catalog` | Merges `locales/messages.tsv` into the ARB catalogs |
| `tools/deps` | Upstream version check (see `ci-cd.md`) |
| `scripts/build.sh` | Release packaging for windows/amd64 and windows/arm64 |
| `.github/workflows` | CI, release and the weekly upstream check |
| `.agents/skills/` | Agent skills (MyGo maintenance) |
