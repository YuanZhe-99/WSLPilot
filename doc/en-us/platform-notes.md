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
| GUI shell (sidebar, dashboard) | implemented | view tests; native window not yet inspected |

## Windows 11 x64

| Feature | Status | Evidence |
|---|---|---|
| Everything | not yet verified | — |
