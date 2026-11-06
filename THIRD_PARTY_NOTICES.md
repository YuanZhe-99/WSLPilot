# Third-party notices

## MyGo

WSLPilot is built with [MyGo](https://github.com/egoist/mygo) (MIT License).

## Lucide icons

The page, action and window icons in `internal/gui/icons.go` are from
[Lucide](https://lucide.dev) (ISC License): Copyright (c) for portions of Lucide are held by Cole
Bemis 2013-2022 as part of Feather (MIT). All other copyright (c) for Lucide are held by Lucide
Contributors 2022. Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above copyright notice and this
permission notice appear in all copies.

## Go modules

Compiled into WSLPilot; each license is compatible with GPL-3.0.

| Module | License | Used for |
|---|---|---|
| `github.com/compose-spec/compose-go/v2` | Apache-2.0 | Loading and validating Compose files |
| `github.com/klauspost/compress` (zstd) | BSD-3-Clause, parts Apache-2.0 | Compression in `wpbk` repositories |
| `github.com/zeebo/blake3` | CC0-1.0 | Chunk hashes in `wpbk` repositories |
| `github.com/klauspost/cpuid/v2` | MIT | CPU feature detection for BLAKE3 |
| `golang.org/x/sys` | BSD-3-Clause | Windows registry, process and file APIs |
| `gopkg.in/yaml.v3` | MIT and Apache-2.0 | Validating `settings.yaml` |
| Modules required by compose-go: `github.com/distribution/reference`, `github.com/docker/go-connections`, `github.com/docker/go-units`, `github.com/opencontainers/go-digest`, `go.yaml.in/yaml/v4`, `github.com/santhosh-tekuri/jsonschema/v6` | Apache-2.0 | Compose loading |
| `github.com/go-viper/mapstructure/v2`, `github.com/mattn/go-shellwords`, `github.com/sirupsen/logrus` | MIT | Compose loading |
| `github.com/xhit/go-str2duration/v2`, `golang.org/x/sync` | BSD-3-Clause | Compose loading |

## Bundled binaries

| File | License | Source |
|---|---|---|
| `ghostty-vt.dll` (libghostty-vt, fetched by `mygo build` for MyGo's terminal plugin, SHA-256 pinned) | MIT | <https://github.com/ghostty-org/ghostty> |

## Helper image

Volume transfers run tar in a helper container from `alpine:3.20` (pulled on first use, not
bundled): Alpine Linux, busybox (GPL-2.0) and musl (MIT).
