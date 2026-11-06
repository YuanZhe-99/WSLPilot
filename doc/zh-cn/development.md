# 开发

## 环境

- 已安装 WSL 的 Windows 11 x64 或 ARM64（`wsl --version` 必须可用）。
- Go 1.27.3 或更高（`go.mod` 中的 `go` 指令；较新的工具链会自动下载）。
- 无 cgo、无 C 工具链、无 Node 或 Bun：界面为 MyGo 原生 UI。

MyGo CLI 是模块工具：`go tool mygo` 运行 `go.mod` 固定的版本。

## 命令

```bash
go tool mygo dev        # build, run, and rebuild on change
go tool mygo build      # package into build/windows-<arch>/
go tool mygo generate   # regenerate locales/catalog_gen.go after editing locales/*.json
go run ./tools/icon     # re-render resources/icon.png after editing resources/icon.svg
go test ./...           # unit and view tests (no window, no WSL needed)
go run ./tools/catalog  # merge locales/messages.tsv into the catalogs, then run mygo generate
scripts/build.sh -skip-installer windows/arm64   # package the app, agent and CLI
WSLPILOT_LIVE=1 go test ./internal/...           # live tests on wslpilot-test-* objects
WSLPILOT_SHOTS=<folder> go test -run Screenshots ./internal/gui   # render every page to PNG
```

图标中的企鹅依据 Tux 重绘；修改图标时须保留 `THIRD_PARTY_NOTICES.md` 中的署名。

## 完整本地检查

```bash
gofmt -l cmd internal tools
go vet ./...
go test ./...
CGO_ENABLED=0 go test ./...
go tool mygo vet .
go tool mygo i18n check
go run ./tools/checkdocs
```

## 实机测试

运行真实 `wsl.exe` / `wslc.exe` 的测试需设置 `WSLPILOT_LIVE=1`，且只能创建和删除名为 `wslpilot-test-*` 的资源。

需要 WSL 3.0.3.0 或更高版本、WSL 容器以及 `alpine:3.20` 镜像。覆盖 Volume tar 往返（`internal/volume`）、发行版备份到每种类型并恢复为新发行版
（`internal/backup`，需要 `wslpilot-test-d1`），以及 Compose 项目的 up、更新与 down（`internal/compose`），还有 Compose 项目备份及其目录与 Volume 的还原
（`internal/core`，`wslpilot-test-pbk`）。

## 驱动窗口

带 `e2e` 标签的构建会链接 MyGo 的自动化插件（`cmd/wslpilot/automation_e2e.go`）；发布构建不包含它。用 `MYGO_AUTOMATION` 指定一个
socket 启动后，可以用 `mygo-automation` 以文本读取并操作窗口（snapshot、press、fill、screenshot）。把 `WSLPILOT_DATA` 设为一个空目录，
可使其设置与项目和你自己的分开；还要在该副本的设置中指定备份目录，因为默认备份目录无论如何都在你的用户目录中。

```bash
go build -tags e2e -o .dev/wslpilot-check.exe ./cmd/wslpilot
go build -o .dev/mygo-automation.exe github.com/egoist/mygo/plugins/automation/cmd/mygo-automation
```

## 录制 fixture

在 Git Bash 中录制以保留工具写出的原始字节，存放到 `internal/<adapter>/testdata/<上游版本>/`，并替换用户名：

```bash
wsl.exe --version > internal/wsl/testdata/3.0.3.0/version-en-utf16.txt
wslc.exe info --format json > internal/wslc/testdata/3.0.3.0/info.json
```
