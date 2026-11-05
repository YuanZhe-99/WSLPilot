# WSLPilot documentation

**These docs are the authoritative description of the code.** `doc/en-us/` is the source;
`doc/zh-cn/` mirrors it file for file. Plans and open work are not documented here; they live only
in the repository's `PLAN.md`.

WSLPilot is a native Windows 11 desktop manager for WSL distributions and WSL Containers (WSLc),
written in Go with [MyGo](https://github.com/egoist/mygo)'s native UI. It drives the official
`wsl.exe` and `wslc.exe` command-line tools and never requires Docker Desktop or Docker Engine.

| Document | Covers |
|---|---|
| [architecture.md](architecture.md) | Processes, packages, layering, cross-cutting rules |
| [upstream.md](upstream.md) | Verified `wsl.exe`, `wslc.exe` and MyGo behavior and quirks |
| [platform-notes.md](platform-notes.md) | What is implemented and verified on each platform |
| [development.md](development.md) | Environment, commands, the dev loop |
| [ci-cd.md](ci-cd.md) | Workflows, packaging, the upstream update procedure |
| [translation-guide.md](translation-guide.md) | Mirror rules and the glossary |
| [version-history.md](version-history.md) | Releases and the upstream versions they were tested with |
| [functions/INDEX.md](functions/INDEX.md) | Per-package roles and key declarations |

## Pinned versions

| Component | Version | Source of truth |
|---|---|---|
| Go | 1.27.3 | `go.mod` |
| MyGo | v0.7.4 | `go.mod` |
| WSL (tested, not bundled) | 3.0.3.0 | `internal/upstream/tested.json`, `upstream.md` |
| WSLc (tested, not bundled) | 3.0.3.0 | `upstream.md` |
