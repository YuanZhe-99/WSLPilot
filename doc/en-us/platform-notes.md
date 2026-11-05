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
| diskpart compaction (elevated) | implemented | unit tests; UAC path not yet run |
| `wpbk` repository | verified | unit, fault-injection and spec tests |
| Volume tar import/export (owners, modes, links) | verified | live test `internal/volume` |
| Distribution backup (folder, archive, repository), verify, restore as new, partial restore | verified | live test `internal/backup` on `wslpilot-test-d1` |
| Compose builtin backend up / update / down | verified | live test `internal/compose` (`wslpilot-test-compose`) |
| Scheduler, retention, retries | implemented | unit tests; user-level `schtasks /Create /SC MINUTE` verified by hand |
| Restart watcher | implemented | unit tests over recorded events |
| GUI pages | implemented | view tests, page renders checked by eye; packaged app starts (10 s smoke run) |
| Terminal plugin, tray, notifications, UAC prompt | implemented | not exercised by hand yet |

## Windows 11 x64

| Feature | Status | Evidence |
|---|---|---|
| Everything | not yet verified | — |
