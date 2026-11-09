# Package index

| Package | Role | Key declarations |
|---|---|---|
| `cmd/wslpilot` | GUI entry point: single instance, tray (open, check for updates, quit), theme, the main window with its own title bar, close to tray, plans and the update loop while running; `host` implements `gui.Host` with MyGo | `main`, `host` |
| `cmd/wslpilot-agent` | Background agent: `run-due`, `watch`, `install`, `uninstall` | `main`, `watch` |
| `cmd/wslpilot-cli` | Command-line entry point | `main` |
| `internal/version` | Release version, kept equal to `mygo.json` | `Version` |
| `internal/earlyenv` | Environment MyGo reads while it initializes (whole frames: `MYGO_PARTIAL=0`); imported first by `cmd/wslpilot` | `FullFrames` |
| `internal/upstream` | Upstream versions WSLPilot was tested with (`tested.json`) | `Tested`, `Manifest`, `Compare`, `NewerThanTested` |
| `internal/procexec` | Child processes from argument vectors; output decoding; raw streams | `Runner`, `Run`, `Streamer`, `Stream`, `Spec`, `RunnerFrom`, `Decode`, `ExitError` |
| `internal/wsl` | `wsl.exe` adapter: list, lifecycle, import/export, move, compact, sparse, exec, file read/write | `Client`, `Distro`, `List`, `ParseListVerbose`, `ParseListOnline`, `InstallArgs`, `ExecArgs`, `Compact`, `ReadFile`, `WriteFile` |
| `internal/wslc` | `wslc.exe` adapter: containers, images, volumes, networks, stats, events, build | `Client`, `WithSession`, `IsCLISession`, `Container`, `ContainerSpec`, `CreateArgs`, `Volume`, `Network`, `Event`, `ParseLines`, `ParseArray`, `HasNativeCompose` |
| `internal/capability` | One combined probe of WSL, WSLc and native Compose | `Prober`, `Report` |
| `internal/store` | WSLPilot metadata: versioned JSON documents, atomic writes | `Store`, `Default`, `Load`, `Save`, `WriteFileAtomic`, `ErrNewerSchema` |
| `internal/jobs` | Long operations: progress, cancel, log, audit log of destructive actions | `Manager`, `Job`, `Snapshot`, `Audit`, `ProgressWriter`, `FormatBytes` |
| `internal/configdoc` | Raw-document editing of `.wslconfig`, `wsl.conf`, `settings.yaml`; form schemas; conflict-checked saves | `INI`, `YAML`, `Field`, `WSLConf`, `WSLConfig`, `WSLcSettings`, `ApplyINI`, `ApplyYAML`, `Snapshot.Save`, `DiffLines` |
| `internal/vhdx` | diskpart compaction of VHDX files WSL cannot compact itself | `Script`, `Compact`, `Elevator`, `ErrInUse` |
| `internal/elevate` | One elevated child process through the UAC prompt | `Run`, `ErrDeclined` |
| `internal/secret` | DPAPI protection of stored secrets (repository passwords) | `Protect`, `Unprotect` |
| `internal/backup/wpbk` | Incremental backup repository (see `backup-format.md`) | `Init`, `Open`, `Repo.Backup`, `Restore`, `Entries`, `Delta`, `Check`, `Forget`, `ApplyPolicy`, `Prune`, `RebuildIndex`, `ChangePassword`, `ReadConfig` |
| `internal/gui` | Native UI (`gui.md`): window shell and sidebar, Dashboard, WSL, Containers, Compose, Volumes, Backups, Terminal, Tasks; the Settings sheet with search; in-app updates; confirmation dialog, config forms, style tokens and icons | `App`, `New`, `Host`, `Term`, `TitleBarOptions`, `UpdateLoop`, `confForm` |
| `locales` | Generated message accessors from `locales/*.json` | `catalog_gen.go` |
| `tools/checkdocs` | Verifies the `doc/` language mirror | `main` |
| `tools/icon` | Renders the app icon SVG to PNG with MyGo's renderer | `render`, `matte` |
| `tools/deps` | Compares the MyGo and WSL pins with the latest releases | `check` |
| `internal/core` | Wires the services for each process; Compose wiring (project data and backup, autostart) | `Core`, `New`, `BackupTarget`, `WireCompose`, `FindProject`, `ProjectData`, `MoveWSLcData`, `AgentPath` |
| `internal/compose` | Compose: backend interface, project metadata, loader, validation, planner, builtin and native backends, migration (see `compose.md`) | `Backend`, `Registry`, `Project`, `Projects`, `Load`, `Translate`, `Diff`, `Builtin`, `Native`, `Migrate` |
| `internal/volume` | Volume import/export as tar through a helper container | `Pipeline`, `Import`, `Export`, `Decompress`, `HelperImage` |
| `internal/backup` | Sources → tar → folder/archive/repository; catalog, verify, restore, retention (see `backups.md`) | `Runner`, `DeleteVersion`, `Runner.BackupProject`, `ProjectManifest`, `ReadProjectManifest`, `ExtractProject`, `ProjectVolumeStream`, `FolderWriter`, `ArchiveWriter`, `RepoWriter`, `Scan`, `GroupBySource`, `Open`, `Verify`, `PlanRetention`, `Delete`, `RepoPassword` |
| `internal/scheduler` | Backup plans, due times, retries, retention, history, lock, Task Scheduler registration | `Service`, `Plan`, `Schedule`, `Retry`, `RunDue`, `Run`, `Install` |
| `internal/restart` | Container restart policies from `wslc events` | `Watcher`, `Decide`, `Label` |
| `internal/wtprofile` | Windows Terminal fragment profiles | `Fragment`, `ContainerShell`, `DistroShell`, `GUIDFor` |
| `tools/catalog` | Merges `locales/messages.tsv` into the ARB catalogs | `main` |
