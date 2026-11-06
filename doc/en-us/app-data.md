# App data

WSLPilot keeps only its own metadata (behavior contract: upstream owns state). Everything lives
under `%APPDATA%\WSLPilot`, or `$WSLPILOT_DATA` when set (tests, portable use).

## Documents

Every JSON document is an envelope written atomically (temporary file, fsync, rename) by
`internal/store`:

```json
{ "schemaVersion": 1, "data": { } }
```

A build refuses to read a document with a newer `schemaVersion` than it knows (`ErrNewerSchema`)
and never rewrites it.

| Path | Content | Schema |
|---|---|---|
| `settings.json` | `theme` (`system`, `light`, `dark`), `backupDir`, `helperImage` (volume helper image, empty = `alpine:3.20`); window: `closeQuits`, `closeHinted`, `relaunchHidden`; updates: `noUpdateCheck`, `lastUpdateCheck`, `announcedVersion`, `autoInstall` (`""`, `idle`, `daily`), `autoInstallHour` (see `gui.md`) | 1 |
| `locations.json` | backup locations opened from outside: `path`, `readOnly`, `label` | 1 |
| `projects/<id>/project.json` | a Compose project (see [compose.md](compose.md)) | 1 |
| `plans/<id>.json` | a backup plan (see [backups.md](backups.md)) | 1 |
| `state/<id>.json` | a plan's `lastRun`, `lastResult`, `nextRun`, `attempt`, `lastSuccess` | 1 |

## Logs and locks

| Path | Content |
|---|---|
| `history/<plan id>.jsonl` | one record per plan run: `start`, `end`, `result`, `items`, `expired`, `attempt` |
| `logs/audit.jsonl` | destructive actions: `time`, `action` (e.g. `wsl.unregister`), `target`, `detail`, `result` |
| `logs/agent.log` | wslpilot-agent's log, rotated at 1 MiB |
| `locks/scheduler.lock` | process ID and time of the process running plans |

## Files outside the app data

| Path | Written when |
|---|---|
| `<backup folder>` (default `%USERPROFILE%\WSLPilot Backups`) | manual backups (`Repository\`), safety backups before danger actions (`Safety\`) |
| `<backup folder>\Safety\container-<name>-<time>.tar` | a container's own files, exported before "Edit configuration" replaces it |
| `<backup folder>\WSLc move <time>\` | images (`images.tar`) and volumes (`volumes\*.tar`) exported while moving WSLc data to a new `storagePath`; the old session storage also stays in place |
| `<file>.wslpilot-<time>.bak` | before a Windows config file (`.wslconfig`, `settings.yaml`, a Compose file) is replaced |
| `/etc/wsl.conf.wslpilot-<time>` | inside a distribution, before its `wsl.conf` is replaced |
| `%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\WSLPilot\wslpilot.json` | Windows Terminal profiles WSLPilot adds |
| Task Scheduler `WSLPilot\RunDue`, `HKCU\…\Run\WSLPilotAgent` | the background agent is installed |
