# Version history

## 0.1.0

Released 2026-11-05. The first release. Tested with WSL 3.0.3.0, WSLc 3.0.3.0 and MyGo v0.7.4 on
Windows 11 ARM64; CI builds and tests x64 and ARM64.

- WSL distributions: install, import, export, start and stop, default, WSL 1/2 conversion, move,
  resize, sparse disks, disk compaction (`wsl --manage --compact`, or diskpart in an elevated
  window on older WSL), default user, package updates, terminal.
- Settings as forms with raw editing under Advanced: `.wslconfig`, `wsl.conf`, WSLc
  `settings.yaml`; only changed lines are rewritten, files are backed up first.
- WSL Containers: containers, images, networks, logs, shell, run, edit configuration (with a
  safety export), restart policies, moving container data to another folder.
- Compose: built-in backend with validation that reports every unsupported key, native volume or
  Windows folder per volume, autostart.
- Volumes: TAR import and export through a helper container (owners, modes and links kept); the
  helper image is configurable.
- Backups: folder, archive and incremental repository (optionally encrypted), a restore-first
  catalog that opens backups from anywhere, plans with retention and retries, a background agent.
- Tray, open at sign-in, notifications, light and dark themes, English and Simplified Chinese,
  signed automatic updates from GitHub releases.

## 0.0.1

Not tagged: repository initialized. MyGo v0.7.4 native UI shell with a sidebar of pages and a
dashboard showing detected WSL, WSLc and Compose status.
