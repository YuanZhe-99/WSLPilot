# Backups, restore and plans

Package `internal/backup` moves **tar streams**: every source produces one, every kind of backup
stores one, every restore consumes one. Linux metadata (owners, modes, links, devices, xattrs)
therefore travels inside tar and never depends on a Windows folder. The incremental repository
format is specified separately in [backup-format.md](backup-format.md).

## Sources

| Source | Kind string | Tar stream from | Consistency |
|---|---|---|---|
| WSL distribution | `wsl-distro` | `wsl --export <name> -` | Terminated first and started again afterwards, unless the user chose a live copy |
| WSLc volume | `wslc-volume` | helper container: `tar -cpf - --numeric-owner -C /src .` | Running containers that mount it are stopped and started again, unless live |
| Compose project | `compose-project` | one tar of the project's Compose and env files, bind folders and named volumes (see [Compose project backups](#compose-project-backups-layout-version-2)) | The project's running containers are stopped and the same services started again, unless live |

A live copy is labeled: `consistent: false` in sidecars and folder metadata, tag `inconsistent` in
repository snapshots. The interface shows it as "taken while running".

## Volume tar pipeline

`internal/volume` runs a helper container (`HelperImage`, label `io.wslpilot.helper=1`) with the
volume mounted, and streams tar through `wslc exec -i` (binary-safe, verified on WSLc 3.0.3.0).
Import accepts `.tar`, `.tar.gz` and `.tar.zst`, detected by magic bytes and decompressed on the
Windows side; `.tar.xz` is refused. Import counts the entries passing through and the entries found
in the volume afterwards. busybox tar in the helper writes no xattrs, so volume exports carry none.
Leftover helpers are removed by `CleanupHelpers`.

## Folder backups (format `wslpilot-folder`, version 1)

```
<dir>/<kind>-<name>-<YYYYMMDD-HHMMSS>/
  .wslpilot/folder.json      metadata, written with "complete": false first
  .wslpilot/manifest.jsonl   one entry per tar entry, in tar order
  files/…                    regular files and directories, browsable in Explorer
```

`folder.json` holds `format`, `version`, `source {kind, name, host}`, `created` (RFC 3339 UTC),
`consistent`, `plan`, `tool`, `entries`, `contentBytes` and `complete`.

Each manifest line is a JSON object:

| Field | Meaning |
|---|---|
| `name` | tar name; a byte string (see below) |
| `type` | `file`, `dir`, `symlink`, `hardlink`, `char`, `block`, `fifo` |
| `mode`, `uid`, `gid`, `uname`, `gname` | as in the tar header |
| `mtime` | RFC 3339 with nanoseconds |
| `linkname`, `devmajor`, `devminor` | for links and devices |
| `size`, `sha256`, `stored` | regular files: content size, SHA-256 (hex) and path under `files/` |
| `pax` | PAX records (xattrs), values as byte strings |

A byte string is a JSON string when it is valid UTF-8, otherwise `{"base64": "…"}`.

`stored` paths encode each component for Windows: control characters, `<>:"\|?*%`, invalid UTF-8
and a trailing dot or space become `%XX`; reserved device names (`CON`, `NUL`, `COM1`…) get a
leading `%`; a path differing from an earlier one only in case gets the suffix `%~N`.

Restoring rebuilds the tar in manifest order, reading content from `files/` and checking each
SHA-256. A backup whose `complete` is false is not offered for restore.

## Archive backups (format `wslpilot-archive`, version 1)

One file `<kind>-<name>-<time>.tar`, `.tar.gz` or `.tar.zst`, written as `.partial` and renamed
when complete, next to a sidecar `<archive>.wpmeta.json`:

| Field | Meaning |
|---|---|
| `format`, `version` | `wslpilot-archive`, 1 |
| `source`, `created`, `consistent`, `plan`, `tool` | as for folders |
| `compression` | `none`, `gzip`, `zstd` (the content is also detected by magic bytes) |
| `size`, `sha256` | of the archive file |
| `entries`, `contentBytes` | of the tar inside |

Checking an archive compares the SHA-256 and the entry count with the sidecar.

## Compose project backups (layout version 2)

A Compose project backup is one tar, stored like any other source in a folder, an archive or a
repository. Its first entry is a manifest; the other entries are laid out as follows:

```
wslpilot-project.json   manifest (first entry)
compose.yaml, .env      the project's files, at the root
folders/<id>/…          each bind folder's content; a bound single file as folders/<id>/<name>
volumes/<name>/…        each named volume's tar, every header kept (owners, modes, links)
```

| Manifest field | Meaning |
|---|---|
| `format`, `version` | `wslpilot-compose`, 2 |
| `project`, `workingDir` | the Compose project name and the Windows folder it ran from |
| `files` | names of the files at the root |
| `folders` | `id`, `source` (Windows path), `relative` (slash-separated path inside the project folder, or absent when outside it), `file` (a bound single file), `services` |
| `volumes` | `key` (Compose key), `name` (WSLc volume), `services` |
| `external` | volumes the project uses but did not create; they are not in the backup |

Windows files and folders get mode 0644 and 0755; links inside a bind folder are not followed and
are listed in the job log. A volume's entries are its export under the prefix `volumes/<name>/`
(the export's `./` root becomes the prefix itself), and hard link targets get the same prefix. To
restore a volume by hand, take the entries under that prefix, strip it (also from hard link
targets) and unpack them as root with `tar -xpf - --numeric-owner`.

Version 1 backups (WSLPilot 0.1.0 and 0.1.1) hold only the files at the root, with no manifest;
their named volumes were separate `wslc-volume` items. Both versions are read.

## Catalog: opening any backup

`backup.Scan(location)` lists what a location holds, two folder levels deep:

| Found | Item kind | Restores as |
|---|---|---|
| `.wslpilot/folder.json` | `folder` | distribution, volume, or files into a distribution |
| `config.json` + `snapshots/` (wpbk) | `repo`, one item per snapshot | same |
| an encrypted wpbk repository without its password | `repo`, one item with the note `encrypted` | nothing until unlocked: the password typed in the app is kept in memory for the session, or comes from a plan writing there |
| `*.tar*` with a sidecar | `archive` | same |
| `*.tar*` without a sidecar | `tar` (outside archive) | same; the interface says source and consistency are unknown |
| `*.vhdx`, `*.vhd` | `vhdx` | new distribution only (`wsl --import --vhd`) |

Locations other than the default backup folder are remembered in the app data
(`locations.json`) and are **read-only**: nothing is locked, pruned or deleted there, and
repositories are opened with `ReadOnly`. The user can take one over as this PC's location.
`GroupBySource` gathers versions of the same source across all locations.

## Restore

Every restore first runs `Verify` on the item and stops if it fails.

| Mode | What happens |
|---|---|
| New distribution | `wsl --import <name> <folder> -` with the tar on stdin (a VHDX item: `--import --vhd`) |
| Over a distribution | a safety backup of the current one into `<backup folder>\Safety`, then unregister, then import under the same name and folder |
| Files into a distribution | `tar -xpf - --numeric-owner -C <dir>` as root, optionally only one path |
| New volume | volume created, tar unpacked by the helper |
| Over a volume | safety backup, volume emptied, tar unpacked |
| Project | files and bind folders written into an empty or new Windows folder (folders that were inside the project folder at their relative path, others under `outside-folders<id>`); each chosen volume into a new volume (under its own name with the project's labels again), or over an existing one after a safety backup and its typed name |
| One volume of a project | the volume's entries only, into a new or an existing volume as above |

Restoring over an existing object needs the user to type its name, and is written to the audit
log.

A version can also be deleted (the step card's Delete, or right-click on it), only in a location of
this PC and after confirmation; it is written to the audit log. An archive is removed with its
sidecar and a folder backup with its folder. In a repository the snapshot is forgotten and the
repository pruned (`DeleteVersion`): data that later versions also hold stays with them, so the
version is in effect merged into the next one, and only what it alone held is freed. Deleting the
last snapshot removes the repository (`config.json` and its four folders).

## Plans and scheduling

`internal/scheduler` stores plans; a plan only produces backups, which belong to the destination.

| Field | Meaning |
|---|---|
| `targets` | sources to back up |
| `destination` | `kind` (`folder`, `archive`, `repo`), `path`, `compression`, `protectedPassword` (an encrypted repository's password, protected with DPAPI for the current Windows user; never stored in plain text) |
| `schedule` | `every` (`hourly`, `daily`, `weekly`, `monthly`, `manual`), `at` (HH:MM), `weekday`, `day` (1–28) |
| `retention` | `keepLast`, `keepDaily`, `keepWeekly`, `keepMonthly`, per source |
| `retry` | `max` attempts after a failure; the delay starts at `initial` and doubles up to `limit` |
| `live` | back up running sources without stopping them |
| `notify`, `notifySuccess` | notifications |

Retention removes only backups the plan made (`plan` field or `plan:<id>` tag); in a repository it
forgets snapshots and prunes. A run appends a record to the plan's history.

Plans run while the window is open (checked every minute) and, when the agent is installed, from
`wslpilot-agent run-due`, which a per-user Task Scheduler task (`WSLPilot\RunDue`, every 15
minutes, "Interactive only", no elevation) starts. The lock file `locks/scheduler.lock` (process ID
and time; stale when the process is gone or after 24 hours) keeps the two from running plans at
the same time.

## Restart policies and autostart

WSLc has no `--restart`. A container carries its policy in the label `io.wslpilot.restart`
(`no`, `always`, `unless-stopped`, `on-failure[:N]`). `wslpilot-agent watch`, started at sign-in
from `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, follows `wslc events` and starts a
container again after `die` when its policy says so, waiting 1 s, 2 s, 4 s… up to 1 minute
between restarts. A `stop` or `kill` before the death counts as the user stopping it; a later
`start` re-arms the policy. The same process brings Compose projects marked autostart up and runs
due plans every 5 minutes. Policies do not apply while no WSLPilot process runs; the interface
says so.
