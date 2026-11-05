# Upstream contract

Only behavior observed in a real run or read in official source is recorded here, with the version
it was observed on. Unverified assumptions belong in `PLAN.md`.

## Tested versions

The machine-readable list is `internal/upstream/tested.json`; keep it and this table in step.

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
| `--help` lists `--manage <Distro> --move/--resize/--set-sparse`, `--export --format`, `--import --vhd`, `--import-in-place`, `--update`, `--unregister`. | `wsl.Client` methods. |
| `--list --verbose`: a leading `*` marks the default; the name has no spaces; the last column is the version digit. The state column is localized. | `wsl.ParseListVerbose` reads only `*`, name and version; running state comes from `--list --running --quiet`. |
| `--list --online`: localized introduction and header, then one `<name> <friendly name>` per line after the last blank line. | `wsl.ParseListOnline`. |
| Paths are not printed. `HKCU\Software\Microsoft\Windows\CurrentVersion\Lxss\{GUID}` holds `DistributionName`, `BasePath`, `VhdFileName`, `Flavor`, `OsVersion`, `DefaultUid`; WSL also writes a Windows Terminal fragment per distribution (`TerminalProfilePath`). | Read-only, for paths and details only (`wsl.Client.List`). |
| `--manage <Distro> --compact` exists (3.0.3.0), needs no elevation, works on a running distribution and terminates it. A 281 MB disk with 200 MB deleted shrank to 72 MB. | `wsl.Client.Compact`; `SupportsCompact` looks for the literal `--compact` in `--help`. |
| `--manage --set-sparse true` succeeds and warns that sparse VHDs are experimental. `--manage --move` moves the disk and updates `BasePath`. | `SetSparse`, `Move`. |
| `--export <Distro> -` streams the tar to stdout; the reader must drain it, or wsl.exe blocks. | `wsl.Client.Export` with a writer. |
| `--export --format vhd` failed with `ERROR_SHARING_VIOLATION` after an interrupted export, also with the distribution terminated; not retried after `--shutdown` (not verified). | The GUI offers VHD export only after a shutdown. |
| `--install` options: `--name`, `--location`, `--version`, `--fixed-vhd`, `--vhd-size`, `--web-download`, `--from-file`, `--no-launch`. | `wsl.InstallArgs` always adds `--no-launch`. |
| `--distribution X --user U --exec argv…` runs argv without a Linux shell. | `wsl.ExecArgs`. |

Fixtures: `internal/wsl/testdata/3.0.3.0/` (captured), `internal/wsl/testdata/synthetic/`
(hand-written localized output, marked as such).

## wslc.exe

| Behavior | Handling |
|---|---|
| `wslc info --format json` returns `Client` (Version, KernelVersion, WindowsVersion, SettingsFile, …) and `Server` (SessionManagerVersion, Sessions). | `wslc.ParseInfo`; the main detection entry point. |
| An unknown command exits 1. `wslc compose` does not exist in 3.0.3.0. | `wslc.HasNativeCompose` treats exit 0 of `wslc compose --help` as present. |
| The settings file is `%LOCALAPPDATA%\wslc\settings.yaml`; it may not exist. | Reported path is taken from `wslc info`. |
| `wslc run` has no `--restart` option. | Restart policies are implemented by WSLPilot (agent). |
| `wslc volume create` drivers: `guest` (default) and `vhd`. | — |
| `wslc container cp` reads a stream from `-` (stdin); `-a` is only accepted for Docker compatibility. | Ownership preservation through `cp` is not assumed. |
| `list` commands with `--format json` print JSON Lines (one object per line, Docker's `--format json` shape); `inspect` prints an indented JSON array. | `wslc.ParseLines`, `wslc.ParseArray`. |
| The list `Labels` field is one comma-joined string whose values may contain commas (WSLc's own `com.microsoft.wsl.container.metadata` label holds JSON). | Labels are read from `inspect` only. |
| `inspect` of a missing object exits 1 and prints `[]`. | `wslc.ErrNotFound`. |
| `--filter label=k=v` and `--filter name=…` work on `container list` and `volume list`. | Compose looks containers up by label. |
| Published ports without an address bind to `127.0.0.1` (`session.defaultBindingAddress`). | Shown in the port column. |
| `volume create -d vhd` requires `-o SizeBytes=<n>`; `inspect` then shows `Status.HostPath`, the VHDX under `%LOCALAPPDATA%\wslc\sessions\<session>\volumes\`. | `CreateVolume`; vhd volumes can be compacted with `vhdx`. |
| `-v "C:\path with space:/b:ro"` bind-mounts a Windows folder (virtiofs, read-only honored). | `MountSpec` renders `source:target[:ro]`. |
| Binary data passes through `wslc exec -i` stdin and stdout unchanged (3 MB tar, SHA-256 identical both ways); owners, modes, symlinks and hard links survive `tar -xp --numeric-owner` (busybox tar 1.36.1 in alpine). | Volume import/export pipeline. |
| `container export` refuses running containers (`WSLC_E_CONTAINER_IS_RUNNING`) and leaves an empty `-o` file behind. | Export stops first; failed outputs are deleted. |
| `system events --format json` streams Docker-shaped events (`Type`, `Action`, `Actor.Attributes`, `timeNano`) and stays open. | `wslc.Client.Events`; restart policies watch `die`. |
| `image save` writes with `--output`, `image load` reads with `--input`; `network connect` takes `--network-alias`. | `Save`, `Load`, `Connect`. |
| `wslc build` exists (`--file`, `--tag`, `--build-arg`, `--target`, `--progress plain`). | Compose `build` is supported through it. |
| `settings.yaml` keys (Microsoft Learn, 2026-10-16): `session.cpuCount`, `memorySize`, `maxStorageSize`, `storagePath`, `defaultBindingAddress`, `hostLoopback`, `idleTimeout`, and `credentialStore`; every value accepts `default`. Changing `storagePath` does not move existing data. | `configdoc.WSLcSettings`; data-location migration is export and re-import. |

Fixtures: `internal/wslc/testdata/3.0.3.0/` (user name scrubbed to `user`).

## Configuration files

Keys and defaults of `wsl.conf` and `.wslconfig` come from Microsoft Learn "Advanced settings
configuration in WSL" (2026-10-16), those of `settings.yaml` from "WSL container settings" (same
date). `internal/configdoc/schema.go` copies the subset the forms show.

| Behavior | Handling |
|---|---|
| Section and key names are case-insensitive; Microsoft's own example writes `localhostforwarding`. | `configdoc.INI` looks keys up case-insensitively and keeps their spelling. |
| `.wslconfig` path values must double their backslashes. | `Field.Encode` / `Decode` with `EscapeBackslashes`. |
| `wsl.conf` changes apply after the distribution stops (about 8 seconds after the last shell); `.wslconfig` after `wsl --shutdown`; WSLc session settings after `wslc system session terminate`. | `Field.Effect` drives the restart hint. |
| `wslc settings` creates the file with every setting commented out and set to `default`. | `configdoc.YAML.Set` uncomments such a line. |

## MyGo

| Behavior | Handling |
|---|---|
| Native UI needs no WebView2; it draws with Direct3D 11 on Windows 10/11 x64 and arm64. | WSLPilot uses native UI only. |
| `mygo generate` writes `locales/catalog_gen.go` from the ARB catalogs. | Run after editing a catalog; the file is committed. |
