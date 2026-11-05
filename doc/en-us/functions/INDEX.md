# Package index

| Package | Role | Key declarations |
|---|---|---|
| `cmd/wslpilot` | GUI entry point: single instance, theme, main window | `main` |
| `internal/version` | Release version, kept equal to `mygo.json` | `Version` |
| `internal/upstream` | Upstream versions WSLPilot was tested with (`tested.json`) | `Tested`, `Manifest`, `Compare`, `NewerThanTested` |
| `internal/procexec` | Child processes from argument vectors; output decoding; raw streams | `Runner`, `Run`, `Streamer`, `Stream`, `Spec`, `Decode`, `ExitError` |
| `internal/wsl` | `wsl.exe` adapter: list, lifecycle, import/export, move, compact, sparse, exec, file read/write | `Client`, `Distro`, `List`, `ParseListVerbose`, `ParseListOnline`, `InstallArgs`, `ExecArgs`, `Compact`, `ReadFile`, `WriteFile` |
| `internal/wslc` | `wslc.exe` adapter: containers, images, volumes, networks, stats, events, build | `Client`, `Container`, `ContainerSpec`, `CreateArgs`, `Volume`, `Network`, `Event`, `ParseLines`, `ParseArray`, `HasNativeCompose` |
| `internal/capability` | One combined probe of WSL, WSLc and native Compose | `Prober`, `Report` |
| `internal/store` | WSLPilot metadata: versioned JSON documents, atomic writes | `Store`, `Default`, `Load`, `Save`, `WriteFileAtomic`, `ErrNewerSchema` |
| `internal/jobs` | Long operations: progress, cancel, log, audit log of destructive actions | `Manager`, `Job`, `Snapshot`, `Audit`, `ProgressWriter`, `FormatBytes` |
| `internal/configdoc` | Raw-document editing of `.wslconfig`, `wsl.conf`, `settings.yaml`; form schemas; conflict-checked saves | `INI`, `YAML`, `Field`, `WSLConf`, `WSLConfig`, `WSLcSettings`, `ApplyINI`, `ApplyYAML`, `Snapshot.Save`, `DiffLines` |
| `internal/vhdx` | diskpart compaction of VHDX files WSL cannot compact itself | `Script`, `Compact`, `Elevator`, `ErrInUse` |
| `internal/elevate` | One elevated child process through the UAC prompt | `Run`, `ErrDeclined` |
| `internal/backup/wpbk` | Incremental backup repository (see `backup-format.md`) | `Init`, `Open`, `Repo.Backup`, `Restore`, `Entries`, `Check`, `Forget`, `ApplyPolicy`, `Prune`, `RebuildIndex` |
| `internal/gui` | Native UI: sidebar, pages, dashboard | `App`, `New`, `App.View`, `App.Refresh` |
| `locales` | Generated message accessors from `locales/*.json` | `catalog_gen.go` |
| `tools/checkdocs` | Verifies the `doc/` language mirror | `main` |
| `tools/icon` | Renders the app icon SVG to PNG with MyGo's renderer | `render`, `matte` |
| `tools/deps` | Compares the MyGo and WSL pins with the latest releases | `check` |
