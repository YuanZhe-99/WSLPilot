# AGENTS.md

Operating guide for agents working on **WSLPilot**. This file holds **only** rules about how to work
here. Everything describing what the code *is* or *does* lives in `doc/en-us/` — see
[Where to read what](#where-to-read-what).

WSLPilot is a native Windows 11 (x64 and ARM64) desktop manager for WSL distributions, WSL
Containers (WSLc), Compose projects, volumes and backups, written in Go with MyGo's native UI.
Treat the user's message as the change request: plan, implement, verify, report.

## Reading order

When you need to understand code, read in this order and stop as soon as you have what you need:

1. **`doc/en-us/`** — start here, always. `architecture.md` for shape and rules;
   `functions/INDEX.md` to find the right package; `upstream.md` for verified WSL / WSLc facts.
2. **Comments in the source** — the Function Explanation Layer on each declaration.
3. **The implementation** — only when the docs and comments are insufficient, or when you must
   confirm actual behavior before changing it.

Where docs and code disagree on something you are about to change, verify against the code, then
fix the docs in the same commit.

## Where to read what

| Question | Read |
|---|---|
| Current plan, design decisions not yet implemented, open questions | `PLAN.md` |
| Processes, packages, layering, cross-cutting rules | `doc/en-us/architecture.md` |
| Verified `wsl.exe` / `wslc.exe` / MyGo behavior and quirks | `doc/en-us/upstream.md` |
| What is implemented and verified per platform | `doc/en-us/platform-notes.md` |
| Environment, commands, dev loop | `doc/en-us/development.md` |
| English→Chinese terminology | `doc/en-us/translation-guide.md` |
| Releases and the upstream versions they were tested with | `doc/en-us/version-history.md` |
| Per-package roles and key declarations | `doc/en-us/functions/INDEX.md` |

## Required workflow

1. Treat the user's message as the modification request.
2. **Before any edit:** `git fetch` every configured remote and check whether the local branch is
   behind or diverged; resolve divergence first. Check for a newer MyGo release
   (`go list -m -u github.com/egoist/mygo`); if it moved, upgrade first per
   [Upstream versions](#upstream-versions).
3. Read per [Reading order](#reading-order). Multi-step plans go into `PLAN.md`.
4. **Research before relying on upstream behavior.** Never invent a `wsl.exe` / `wslc.exe` flag,
   output format or API. Confirm it with `--help`, a real run, or official source, and record the
   finding in `doc/en-us/upstream.md` (with the version it was observed on).
5. Keep changes scoped. Do not revert unrelated work in the tree.
6. Update documentation in the same change set — see
   [Documentation maintenance](#documentation-maintenance).
7. Verify — see [Verification](#verification).
8. Report briefly, in English and Chinese: what changed, what was verified (with evidence), the
   current version, and anything that could not be done.
9. Ask before pushing. The user decides when to release.

## Documentation maintenance

**Docs are the primary artifact. Update them first, and never let them drift.**

Any change to a behavior, a data format, a run mode or a user-visible feature updates, in the same
commit, the affected concept docs and the `functions/INDEX.md` row.

Every language directory under `doc/` (currently `en-us` and `zh-cn`) mirrors the others exactly —
same files, headings, tables, and code blocks. `doc/en-us/` is authoritative: any documentation
change updates **all** language directories in the same commit, translated per
`translation-guide.md`; new terms go into its glossary. `go run ./tools/checkdocs` must pass.

**Plans live only in `PLAN.md`** (written in Chinese, the user's working language) — never in
`doc/`. When a planned contract is implemented (Compose backend interface, backup format, volume
import pipeline…), move its description into `doc/` and delete it from `PLAN.md`. When the plan is
complete, delete `PLAN.md`.

Persisted formats (`wpbk` repositories, archive sidecars, folder-backup manifests, project
metadata) get a dedicated spec in `doc/` **before** their first release, versioned, with a test
proving a reader that follows only the spec can restore data.

Add a `doc/en-us/version-history.md` entry for each release. Documentation-only commits do not
bump versions or create tags.

## Authoring rules

**Function Explanation Layer.** Every function, method and significant type carries a Go doc
comment whose first sentence states its purpose (godoc style: "Probe reports …"). Non-trivial
declarations add short lines as needed:

- `Inputs:` important parameters only
- `Returns:` what the caller receives
- `Side effects:` state, file, process or UI effects
- `Notes:` assumptions, edge cases, invariants

Other conventions:

- MyGo UI code follows `.agents/skills/mygo-maintenance`: views are functions of state, elements
  are never stored, background results reach the view through `Window.Update`, controls get stable
  keys where state applies. No blocking I/O in a view.
- Layering is enforced by review: `gui` → domain packages → adapters (`wsl`, `wslc`) → process
  execution. Views never build CLI arguments.
- Adapter parsers are table-driven tests over `testdata/` fixtures captured from real runs,
  grouped by upstream version. Never parse localized labels; prefer `--format json`.
- Child processes are started with argument vectors, never through a shell string; paths with
  spaces, quotes, non-ASCII and `\\wsl.localhost\` prefixes are test cases.
- Interface copy is written for the person using the app: no raw exit codes, GUIDs or CLI dumps in
  the main interface (they belong in the details/log view).
- Interface strings go through the ARB catalogs (`locales/`, en and zh-Hans);
  `go tool mygo i18n check` must pass. Run `go tool mygo generate` after editing a catalog and
  commit `locales/catalog_gen.go`.
- Layout numbers and colors are named tokens, not literals scattered through views.

## Behavior contract

Do not change these without the user explicitly deciding to:

- **Upstream owns state.** WSL / WSLc objects are always read back from the CLI; WSLPilot stores
  only its own metadata. Changes the user makes with the CLI must show up correctly.
- **Never destroy data silently.** Delete, overwrite, unregister, prune, migrate and restore-over
  require confirmation, offer a backup first (on by default), and are logged.
- **Never touch what we did not create in tests.** Live tests and experiments only use resources
  named `wslpilot-test-*` and clean them up.
- **Compose backends never switch on their own.** A project's backend changes only through the
  migration flow after user confirmation — never because WSLPilot or WSLc was updated.
- **Unsupported Compose features are reported, never ignored.** Validation fails rather than
  dropping a key.
- **Linux metadata travels as tar.** Volume import/export and backups never unpack Linux trees into
  Windows folders as the source of truth.
- **Consistency over convenience.** Running distributions/containers are stopped (with
  confirmation) or snapshotted before backup; an inconsistent copy is only made when the user
  explicitly chooses it, and is labeled so.
- **The GUI never runs elevated.** Elevation happens in a separate, clearly announced child process
  only for operations that require it.
- **Configs are raw documents.** Edits rewrite only changed spans, saves are conflict-checked and
  backed up, invalid drafts are never applied.
- **Secrets never travel.** Registry credentials stay with `wslc login`; WSLPilot never logs or
  persists them.

## Upstream versions

Keep MyGo at its latest tag and Go at the version MyGo requires. When MyGo moves: `go get
github.com/egoist/mygo@latest`, `go get -tool github.com/egoist/mygo/cmd/mygo@latest`,
`go tool mygo migrate-ui .` (review by hand), full verification, and a `version-history.md` line.

WSL / WSLc are not bundled. `doc/en-us/upstream.md` lists the versions WSLPilot was tested with;
re-capture fixtures and re-run the live tests when a new WSL release appears, and add it to the
tested list only after they pass.

## Verification

```bash
gofmt -l cmd internal tools
go vet ./...
go test ./...
CGO_ENABLED=0 go test ./...
go tool mygo vet .
go tool mygo i18n check
go run ./tools/checkdocs
```

- Live adapter tests run with `WSLPILOT_LIVE=1` and only touch `wslpilot-test-*` resources.
- Inspect the native app (screenshots) when a change touches layout.
- Tests protect product contracts and error-prone logic; every fixed bug gets a regression test.
  Do not write tests for wording.
- **Never claim "verified" without an actual run;** record what was verified where
  (`platform-notes.md`).

## Release, version, commit, tag, push

The version lives in `internal/version` (`Version`); `mygo.json` must match (a test enforces it).

Do not bump versions or tag until the user confirms the release version and confirms pushing.
Patch bumps for fixes and small features; minor bumps (resetting patch) for a new feature area or a
reworked interface. Release commits include the `version-history.md` entry (and its mirror) and an
annotated tag `vX.Y.Z`; push the commit first, then the tag.

## Agent co-author attribution

An agent that made a real, material contribution to a commit may add its own accurate
`Co-authored-by:` trailer. Do not add an agent merely because it reviewed or continued another
agent's work, and never copy a trailer from an earlier commit. Use the agent's actual identity,
e.g. `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` with the real model name.

## Remotes and secrets

No remote is configured yet. If a private Gitea `origin` is added, keep its URL written as
`<local_gitea_address>` in every committed file. Determine the repository path from the runtime
workspace; never hardcode a machine-specific absolute path.

**Never commit:** secrets, signing keys, credentials, `.mygo/`, `build/`, `.dev/`, or
machine-specific state.

License: GPL-3.0 (`LICENSE`). Do not copy code from repositories with incompatible licenses; record
third-party code and bundled binaries with their license.
