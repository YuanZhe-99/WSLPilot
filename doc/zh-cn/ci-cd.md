# CI/CD

工作流位于 `.github/workflows/`，在 GitHub 上运行。Action 以提交 SHA 固定，注释中写明发布标签。

## 工作流

| 工作流 | 触发 | 作用 |
|---|---|---|
| `ci.yml` | 推送到 `main`、Pull Request、手动 | Ubuntu 上做检查；Windows x64 与 ARM64 上测试并打包 |
| `release.yml` | 标签 `v*`、手动 | 构建两个 Windows 安装包；标签触发时还会发布 GitHub Release |
| `upstream.yml` | 每周、手动 | MyGo 或 WSL 有比固定版本更新的发布时创建 Issue |

## ci.yml 任务

| 任务 | Runner | 步骤 |
|---|---|---|
| Changes | `ubuntu-latest` | 判断是否有 `doc/`、`design/` 与 Markdown 以外的变更 |
| Checks | `ubuntu-latest` | gofmt、windows/amd64 与 windows/arm64 的 `go vet`、目录、文档镜像、govulncheck、gitleaks、上游固定版本（仅提示） |
| Test | `windows-latest`、`windows-11-arm` | `go vet`、开启与关闭 cgo 的 `go test`、`mygo vet`、竞态检测（x64）、`scripts/build.sh -skip-installer` |

托管 Runner 上没有 WSL，因此实时适配器测试（`WSLPILOT_LIVE=1`）从不在 CI 中运行；它们在发布前于开发机上运行（见 `development.md`）。

## 打包

`scripts/build.sh [-skip-installer] windows/amd64 windows/arm64` 把除 GUI 以外的每个 `cmd/*` 程序编译到
`resources/windows-<arch>/`（git 忽略），然后运行 `go tool mygo build`，后者把这些文件放在 `WSLPilot.exe` 旁边，
并生成 NSIS 安装程序 `wslpilot-setup-<version>-<arch>.exe`。

发布任务把每个 `build/windows-<arch>/` 目录压缩为便携版下载，附上安装程序与 `SHA256SUMS.txt`；
标签与 `internal/version.Version` 不一致时拒绝发布。

| Secret | 用途 | 未设置时 |
|---|---|---|
| `MYGO_UPDATER_PRIVATE_KEY` | 为更新包与 `update-<platform>.json` 签名 | 不发布更新 |

## 上游更新流程

`go run ./tools/deps check` 输出 MyGo 与 WSL 的固定版本和最新版本。

1. MyGo 有新版本：按 AGENTS.md「Upstream versions」操作，然后把 `internal/upstream/tested.json` 中的 `mygo`
   改为新版本。
2. WSL 有新版本：在开发机上安装，重新录制 fixture 到 `internal/<adapter>/testdata/<version>/`，运行实时测试，
   通过后才把该版本追加到 `internal/upstream/tested.json` 的 `wsl` 与 `upstream.md` 的表格中。
