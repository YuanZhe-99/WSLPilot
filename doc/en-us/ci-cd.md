# CI/CD

Workflows live in `.github/workflows/` and run on GitHub. Actions are pinned by commit SHA, with
the release tag in a comment.

## Workflows

| Workflow | Trigger | What it does |
|---|---|---|
| `ci.yml` | push to `main`, pull request, manual | Checks on Ubuntu; tests and a package on Windows x64 and ARM64 |
| `release.yml` | tag `v*`, manual | Builds both Windows packages; a tag also publishes the GitHub release |
| `upstream.yml` | weekly, manual | Opens an issue when MyGo or WSL has a newer release than the pins |

## ci.yml jobs

| Job | Runner | Steps |
|---|---|---|
| Changes | `ubuntu-latest` | Decides whether anything but `doc/`, `design/` or Markdown changed |
| Checks | `ubuntu-latest` | gofmt, `go vet` for windows/amd64 and windows/arm64, catalogs, docs mirror, govulncheck, gitleaks, upstream pins (informational) |
| Test | `windows-latest`, `windows-11-arm` | `go vet`, `go test` with and without cgo, `mygo vet`, race detector (x64), `scripts/build.sh -skip-installer` |

Hosted runners have no WSL, so live adapter tests (`WSLPILOT_LIVE=1`) never run in CI; they run on
a developer machine before a release (see `development.md`).

## Packaging

`scripts/build.sh [-skip-installer] windows/amd64 windows/arm64` compiles every `cmd/*` program
except the GUI into `resources/windows-<arch>/` (ignored by git), then runs `go tool mygo build`,
which ships those files next to `WSLPilot.exe` and makes the NSIS installer
`wslpilot-setup-<version>-<arch>.exe`.

The release job zips each `build/windows-<arch>/` folder as a portable download, adds the
installers and `SHA256SUMS.txt`, and refuses to publish when the tag differs from
`internal/version.Version`.

| Secret | Used for | When unset |
|---|---|---|
| `MYGO_UPDATER_PRIVATE_KEY` | Signing the update archives and `update-<platform>.json` | No updates are published |

## Upstream update procedure

`go run ./tools/deps check` prints the pinned and latest versions of MyGo and WSL.

1. MyGo moved: follow AGENTS.md "Upstream versions", then set `mygo` in
   `internal/upstream/tested.json` to the new version.
2. WSL moved: install it on a development machine, re-capture fixtures into
   `internal/<adapter>/testdata/<version>/`, run the live tests, and only then append the version
   to `wsl` in `internal/upstream/tested.json` and the table in `upstream.md`.
