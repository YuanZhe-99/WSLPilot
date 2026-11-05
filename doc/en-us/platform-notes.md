# Platform notes

What is implemented, and what was verified where. "Verified" means an actual run on the named
machine.

## Windows 11 ARM64

| Feature | Status | Evidence |
|---|---|---|
| Unit and view tests | verified | `go test ./...` on build 26300 |
| WSL version and distribution detection | verified | fixtures captured on WSL 3.0.3.0 |
| WSLc detection, native Compose probe | verified | fixtures captured on WSLc 3.0.3.0 |
| GUI shell (sidebar, dashboard) | implemented | view tests; native window not yet inspected |

## Windows 11 x64

| Feature | Status | Evidence |
|---|---|---|
| Everything | not yet verified | — |
