# Platform notes

What is implemented, and what was verified where. "Verified" means an actual run on the named
machine.

## Windows 11 ARM64

| Feature | Status | Evidence |
|---|---|---|
| Unit and view tests | verified | `go test ./...` on build 26300 |
| WSL version and distribution detection | verified | fixtures captured on WSL 3.0.3.0 |
| WSLc detection, native Compose probe | verified | fixtures captured on WSLc 3.0.3.0 |
| WSL distribution list, lifecycle, compact, sparse, move | verified | live runs on `wslpilot-test-d1` (WSL 3.0.3.0), fixtures |
| WSLc lists, inspect, events, exec streams | verified | live runs on `wslpilot-test-*` objects (WSLc 3.0.3.0), fixtures |
| Config editing (`.wslconfig`, `wsl.conf`, `settings.yaml`) | implemented | unit tests |
| diskpart compaction (elevated) | verified | live test `internal/core` (UAC accepted): a WSLc vhd volume 308 MB → 14 MB |
| `wpbk` repository | verified | unit, fault-injection and spec tests |
| `wpbk` encryption, plan passwords (DPAPI) | verified | unit, spec and leakage tests; encrypted plan end to end |
| Automatic updates (in-app flow) | implemented | unit test of the auto-install timing; "not available in this build" checked in a dev build; a real update needs an installed release older than the newest |
| Volume tar import/export (owners, modes, links) | verified | live test `internal/volume` |
| Distribution backup (folder, archive, repository), verify, restore as new, partial restore | verified | live test `internal/backup` on `wslpilot-test-d1` |
| Compose builtin backend up / update / down | verified | live test `internal/compose` (`wslpilot-test-compose`) |
| Compose project backup (files, bind folder, named volume) and restore | verified | live test `internal/core` (`wslpilot-test-pbk`): stopped and started again, bind file extracted, volume restored with its owner; unit tests over folder, archive and repository |
| Distribution export as VHD and import of the copy | verified | by hand after `wsl --shutdown` on `wslpilot-test-vhd` |
| WSLc sessions: isolation, switching the Containers page | verified | by hand with a second session attached by `session enter`; view test |
| Scheduler, retention, retries | implemented | unit tests; user-level `schtasks /Create /SC MINUTE` verified by hand |
| Restart watcher | implemented | unit tests over recorded events |
| GUI pages (0.1.1 redesign) | verified | view tests; every page rendered light and dark and compared with `design/`; native window on build 26300 (`go build`, not the installer): every page, Settings sheet, theme toggle, maximize and restore, close to tray, populated container and volume tables (`wslpilot-test-ui-*`); 0.1.2 driven with `mygo-automation`: the Compose page with its Data card, Open in Volumes, the Volumes page's project owner, a project backup and a volume restore through the window, the plan editor, and switching the language |
| Terminal plugin, tray menu, notifications | implemented | not exercised by hand yet (closing to the tray is verified) |
| Container edit (safety export, validated recreate) | verified | live test `internal/gui` (`wslpilot-test-edit`) |
| WSLc data location move | verified | live test `internal/core` on an otherwise empty session: image and volume moved, content checked, settings.yaml restored |

## Windows 11 x64

| Feature | Status | Evidence |
|---|---|---|
| Everything | not yet verified | — |
