# Upstream contract

Only behavior observed in a real run or read in official source is recorded here, with the version
it was observed on. Unverified assumptions belong in `PLAN.md`.

## Tested versions

| Component | Version | Machine |
|---|---|---|
| WSL | 3.0.3.0 (kernel 6.18.54.1-1) | Windows 11 ARM64, build 26300 |
| WSLc | 3.0.3.0 (session manager 3.0.3) | Windows 11 ARM64, build 26300 |
| MyGo | v0.7.4 | Windows 11 ARM64, build 26300 |

## wsl.exe

| Behavior | Handling |
|---|---|
| Output to a pipe is UTF-16LE without a BOM; with `WSL_UTF8=1` it is UTF-8. Lines end in CRLF. | `procexec.Run` sets `WSL_UTF8=1` and `procexec.Decode` accepts both. |
| Labels of `--version` and the header and states of `--list --verbose` are localized. | `wsl.ParseVersion` matches lines by position; nothing matches labels. |
| `--version` lines, in order: WSL, kernel, WSLg, MSRDC, Direct3D, DXCore, Windows. | Positional parse; Windows is the last line. |
| `--list --quiet` prints one name per line. | `wsl.ParseNames`. |
| An unknown first argument (`wsl.exe compose`) runs as a Linux command in the default distribution (exit 127 when not found). | Never probe features with `wsl.exe <word>`. |
| `--help` lists `--manage <Distro> --move/--resize/--set-sparse`, `--export --format`, `--import --vhd`, `--import-in-place`, `--update`, `--unregister`. | Used by the WSL page (planned). |

Fixtures: `internal/wsl/testdata/3.0.3.0/` (captured), `internal/wsl/testdata/synthetic/`
(hand-written localized output, marked as such).

## wslc.exe

| Behavior | Handling |
|---|---|
| `wslc info --format json` returns `Client` (Version, KernelVersion, WindowsVersion, SettingsFile, …) and `Server` (SessionManagerVersion, Sessions). | `wslc.ParseInfo`; the main detection entry point. |
| An unknown command exits 1. `wslc compose` does not exist in 3.0.3.0. | `wslc.HasNativeCompose` treats exit 0 of `wslc compose --help` as present. |
| The settings file is `%LOCALAPPDATA%\wslc\settings.yaml`; it may not exist. | Reported path is taken from `wslc info`. |
| `wslc run` has no `--restart` option. | Restart policies must be implemented by WSLPilot (planned). |
| `wslc volume create` drivers: `guest` (default) and `vhd`. | — |
| `wslc container cp` reads a stream from `-` (stdin); `-a` is only accepted for Docker compatibility. | Ownership preservation through `cp` is not assumed. |

Fixtures: `internal/wslc/testdata/3.0.3.0/` (user name scrubbed to `user`).

## MyGo

| Behavior | Handling |
|---|---|
| Native UI needs no WebView2; it draws with Direct3D 11 on Windows 10/11 x64 and arm64. | WSLPilot uses native UI only. |
| `mygo generate` writes `locales/catalog_gen.go` from the ARB catalogs. | Run after editing a catalog; the file is committed. |
