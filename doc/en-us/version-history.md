# Version history

## 0.1.4

Released 2026-11-06. Tested with WSL 3.0.3.0, WSLc 3.0.3.0 and MyGo v0.8.1 on Windows 11 ARM64.

- Fix: a window that had sat in the tray could show only the parts that changed (a hovered row, a
  scrollbar) over an empty frame. WSLPilot now draws whole frames instead of MyGo 0.8's partial
  redraw (`upstream.md`, MyGo).
- Containers › Clean up…: stopped containers, unused networks, dangling or all unused images and,
  on request, volumes no container will use (never a Compose project's), each backed up first.
- MyGo v0.8.1.

## 0.1.3

Released 2026-11-06. Tested with WSL 3.0.3.0, WSLc 3.0.3.0 and MyGo v0.8.0 on Windows 11 ARM64.

- Backup versions can be deleted, also from a right-click menu on the version. In an incremental
  repository the version merges into the next one: data later versions also hold stays, only
  what it alone held is freed; the last version takes the repository with it.
- Settings › General: Start in the tray, under Open WSLPilot at sign-in (on by default; off opens the
  window at sign-in).
- The sidebar folds to a rail of icons and back (the button beside the app's name), as in Kyokai.
- The pane sits 8 px from the window's edges (4 px from the sidebar) with 4 px corners on Windows, as
  in Kyokai, so its corners no longer look rounder than the window's.
- A new app icon: a plain penguin drawn for WSLPilot, in place of the window with a penguin after Tux.
- Fix: view tests raced on a shared update channel and could end before background saves did,
  which failed the CI race detector.

## 0.1.2

Released 2026-11-06. Tested with WSL 3.0.3.0, WSLc 3.0.3.0 and MyGo v0.8.0 on Windows 11 ARM64.

- A Compose project backup is one item holding the project's files, its bind folders and its named
  volumes (layout version 2, `backups.md`); the project stops for a consistent copy and its
  services start again. It works in folder, archive and incremental backups. Restoring brings back
  the whole project or one volume. Backups from 0.1.0 and 0.1.1 still restore.
- The Compose project page shows each service's ports and mounts and a Data card that lists the
  project's volumes and folders the way the Volumes page does; Back up project… says what the
  backup holds first. The Volumes page names the project that owns a volume.
- Fix: Remove unused no longer deletes the volumes of a Compose project that was taken down.
- Clearer wording: Remove containers… (compose down), Remove from WSLPilot…, the validation
  result, and the backup plan editor (schedules and kinds in words, the time from lists, what each
  retention rule keeps); the plan card is one column of label and value.
- Settings › General has a Language choice (same as Windows, English, 简体中文).
- MyGo v0.8.0.

## 0.1.1

Released 2026-11-05. A redesigned interface; the data layer and formats are unchanged. Tested with
WSL 3.0.3.0, WSLc 3.0.3.0 and MyGo v0.7.4 on Windows 11 ARM64.

- A new window: Mica background, a sidebar with icons, the page on a raised pane whose header is
  the title bar, and WSLPilot's own window buttons with snap layouts (`gui.md`).
- Every page rebuilt after the design mockups: stat cards and disk usage bars on the dashboard,
  tables with status dots and pills, a distribution card with an inline compact panel, distribution
  settings as their own page, container logs in a console, a restore-first Backups page, Volumes
  with an inline TAR import, a Tasks page.
- Settings is a searchable sheet in groups (General, Backups & agent, WSL Containers, About &
  updates).
- Updates are installed in the app: daily check, one notice per version, Install and restart
  with progress, and optional automatic installs when idle or at a chosen hour; MyGo's update
  window is no longer used. The tray gains Check for updates.
- Closing the window can quit instead of keeping WSLPilot in the tray; the first close says where
  the window went.
- Fixes: lists loaded first by the dashboard no longer start with a row chosen (Compose opened
  the first project); counts read "1 image", not "1 images".

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
