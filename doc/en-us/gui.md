# GUI

How the WSLPilot window is built (`internal/gui`, `cmd/wslpilot`). The pages follow the mockups in
`design/`; the window shell, the settings sheet and the update flow follow the same patterns as
the MyGo apps Kyokai and EasyTier MyGo. Rules for writing views are in `architecture.md`
("UI thread") and `.agents/skills/mygo-maintenance`.

## Window shell

| Part | Behavior |
|---|---|
| Frame | The system title bar is hidden (`gui.TitleBarOptions`); the window background is Mica. The sidebar is drawn on the Mica, the page on a raised rounded pane beside it. |
| Pane header | Page icon, title and one line of context; it is the title bar: drag to move, double-click to maximize. Page actions sit on its right, then WSLPilot's own minimize, maximize (with snap layouts) and close buttons on Windows. |
| Sidebar | App icon and name, the pages with Lucide icons, and at the bottom Tasks (with a count of running jobs), the appearance toggle (system → light → dark), Settings and the version. |
| Close | Keeps WSLPilot in the tray unless Settings › General says Quit; the first time, a notification says where it went (`closeHinted`). |
| Tray | Open WSLPilot, Check for updates (opens Settings at About & updates and checks), Quit. |
| Start hidden | At sign-in, and after an update that was installed while the window was closed (`relaunchHidden`). |

## Settings sheet

Settings is a sheet over the whole window, not a page. On its left are a search field and the
sections in groups; on its right, the chosen section as rows: an icon tile, a title, one sentence,
and the control.

| Group | Section | Rows |
|---|---|---|
| Preferences | General | Appearance, open at sign-in, closing the window (keep in tray or quit) |
| This computer | Backups & agent | Background agent, default backup folder, volume helper image |
| This computer | WSL Containers | Data location (move), restart the session, the `settings.yaml` form |
| WSLPilot | About & updates | Version and source code, open source licenses, updates, the data folder |

Search matches row titles and sentences; choosing a result opens its section, scrolls to the row
and tints it for a moment.

## Updates

WSLPilot updates itself in the app; MyGo's update window plugin is not used. Releases are signed
(`mygo.json` `updates`, see `ci-cd.md`), and only an installed copy can update
(`Updater.Enabled`): a portable or development build shows "Updates are not available in this
build."

| Setting | Behavior |
|---|---|
| Check for updates daily | On by default. A loop checks once a day while WSLPilot runs (the first time 10 s after start when due) and notifies once per new version (`announcedVersion`). |
| Install updates automatically | Never (default), when the computer has been idle 15 minutes, or every day at a chosen hour. Only when no job runs; a failed attempt waits 6 hours. |
| Install and restart | Downloads with progress, verifies the signature, installs and relaunches; a window that was in the tray stays there. |

The update loop and `autoInstallDue` are in `internal/gui/update.go`; the preferences are in
`app-data.md`.

## Pages

| Page | Contents |
|---|---|
| Dashboard | Stat cards for WSL, WSL Containers and Compose (a number, a status pill, a line, a link); disk usage bars on one scale (used, reclaimable, or on disk when the inside is not measured); activity (running jobs, recent results, next planned backup); quick actions. |
| WSL | A table of distributions (default pill, state dot, version, size, location) and the chosen one's card: a grid of actions, sparse disk, the inline compact panel with its steps and "Back up first", Unregister. A distribution's settings open as their own page with a breadcrumb, a switch between `wsl.conf` and `.wslconfig`, grouped rows marked Changed, and a card of the changes to save. |
| Containers | Containers, Images and Networks as segments; the container table (state, ports, CPU, memory, restart policy, Compose project) and the chosen container's logs in a dark console with search, Shell, Restart, Stop and more. |
| Compose | Project cards; a project page with service cards, actions, volumes, and the `compose.yaml` editor with diagnostics on the offending lines and an issue list. |
| Volumes | The volume table with the chosen volume's actions under it (Export TAR, Back up, Compact for vhd volumes, Delete) and the TAR import panel beside it (a new volume, or replacing a volume's contents after a safety backup; an entry-count check after import). Remove unused deletes volumes no container uses, backing each up first. |
| Backups | Restore first: open a backup made anywhere, what can be restored, the locations, then the versions of the chosen source and a step card to restore one. Plans: the plan table and the chosen plan with Run now, Edit, Delete and its history. |
| Terminal | Tabs of shells in distributions, containers or PowerShell. |
| Tasks | Every job of this run with progress and log. |

## Building blocks

Layout numbers and colors are tokens in `internal/gui/style.go`; icons are Lucide SVGs in
`internal/gui/icons.go`. Shared pieces: `card`, `pill`, `stateText`, `bar`, `callout`,
`emptyState`, `iconButton`, `actionButton`, row lists (`rows.go`), and `paneHead` for every page
header. On a selected table row, colored labels switch to the row's text color (`selTone`).
