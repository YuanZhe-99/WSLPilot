# WSLPilot UI design

Static HTML mockups of WSLPilot's screens. Open `index.html` in a browser; the sidebar and the
in-page links move between screens. They are a design reference for the MyGo native UI, not code
the app runs: the app draws the same layouts with `ui.Sidebar`, `ui.Table`, `ui/editor`, the
terminal plugin and MyGo dialogs.

The editable source is the design canvas
<https://claude.ai/artifact/H1EtLxkwG9h3yURRJNxprT>. These files are an exported copy; when the
canvas changes, re-export them rather than editing both.

| File | Screen |
|---|---|
| `index.html` | Dashboard |
| `MainDark.html` | Dashboard in the dark theme (other screens swap the same tokens) |
| `WSL.html` | WSL distributions, with the VHDX compaction flow |
| `DistroSettings.html` | Distribution settings: `wsl.conf` as a form, raw editing under Advanced |
| `WSLConfig.html` | WSL-wide settings: `.wslconfig` as a form |
| `Containers.html` | Containers, with logs |
| `Compose.html` | Compose project: services, editor, unsupported-key errors |
| `Volumes.html` | Volumes and TAR import |
| `Backups.html` | Backups, restore view: everything restorable, opening outside backups |
| `BackupPlans.html` | Backups, plans view |

## Rules the mockups show

- Danger actions confirm and offer a backup first, on by default.
- Long jobs show progress and can be cancelled.
- Unsupported Compose keys are errors, never ignored; a project's backend never switches on its own.
- Configuration files are forms for common settings; editing the raw file is Advanced, only changed
  lines are rewritten, and the file is backed up first.
- Backups are restore-first: any folder, archive, `wsl --export` file or incremental repository can
  be opened, checked and restored, including ones from another PC, which stay read-only.

Sample names, sizes and times in the mockups are illustrative.

Not drawn yet: Terminal, app Settings, Images, Networks, first-run setup.
