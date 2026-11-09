# 增量备份仓库格式（wpbk v1）

本文是 `wpbk` 仓库格式第 1 版的权威规范。它的写法保证：只要有本文、一个 zstd 解码器和一个 BLAKE3
实现，就能恢复任何快照；加密仓库另外需要密码以及 Argon2id、HKDF-SHA256 和 AES-256-GCM。
`internal/backup/wpbk` 是其中一种实现；它的 `spec_test.go` 中包含一个只依据本文编写的独立读取器，覆盖未加密
和加密仓库，该测试必须持续通过。

状态：已在 `internal/backup/wpbk` 实现，尚未发布。格式在第一个写入它的版本发布时冻结。

## 模型

仓库存储 **tar 流** 的 **快照**。输入单位是 tar 流，因为 Linux 元数据（属主、权限、时间、链接、xattr、
设备号）以 tar 形式传递。

- 每个 tar 条目逐字段记录完整的头部，以及构成其内容的有序 **数据块** 列表。tar 流本身从不按字节存储；
  恢复时重新生成。因此可以单独列出和恢复某个条目。
- 内容由内容定义的 **分块器** 切分。数据块 ID 是其明文的 BLAKE3-256 哈希（加密仓库中为带密钥哈希）；
  仓库中已有的数据块不会再次存储，跨所有快照都成立。
- 数据块经 zstd 压缩，作为 **blob** 存入约 32 MiB 的 **包** 文件。每个包末尾带有自己的目录，因此
  **索引** 可以只凭包重建。
- 仓库可以 **加密**，在创建时选择。此时 blob、包尾部目录、索引文件和快照文件都用由 **主密钥** 派生的密钥
  封装，主密钥由 **密码** 解锁（见[加密](#加密)）。

## 约定

- **ID**：32 字节，写成 64 个小写十六进制字符。数据块、包和索引 ID 分别是数据块明文、按存储形式的整个
  包文件和整个索引文件的 BLAKE3-256（无密钥，32 字节输出）；加密仓库中的数据块 ID 改为带密钥哈希（见
  [加密](#加密)）。快照、仓库和锁的 ID 是随机值。
- **Base64** 指带填充的标准 base64（RFC 4648 第 4 节）。
- **JSON**：UTF-8，RFC 8259。读取器必须忽略不认识的字段。
- **时间**：RFC 3339 字符串，可带小数秒（纳秒精度），例如 `"2026-03-04T05:06:07.123456789Z"`。写入方
  使用 UTC。
- **字节串**：名称、链接目标、用户名、组名和 PAX 记录是字节串，而非文本。合法 UTF-8 的字节串写成 JSON
  字符串；其它值写成对象 `{"base64": "<带填充的标准 base64>"}`。
- 二进制结构中的 **整数** 为无符号小端序。
- **临时文件** 命名为 `tmp-*.partial`。读取器忽略它们以及所有不符合下列布局的名称。

## 目录布局

```text
<repository>/
  config.json
  packs/<first 2 hex digits of id>/<pack id>.pack
  index/<index id>.json
  snapshots/<snapshot id>.json
  locks/<lock id>.json
```

这五项必须全部存在；仅在只读访问时允许缺少 `locks/`。

## config.json

```json
{
  "format": "wpbk",
  "version": 1,
  "id": "5d0c…64 hex…",
  "created": "2026-11-05T10:00:00Z",
  "chunker": {"algorithm": "fastcdc-gear-v1", "min": 262144, "avg": 1048576, "max": 4194304},
  "compression": "zstd",
  "hash": "blake3-256",
  "encryption": null,
  "packSize": 33554432
}
```

| 字段 | 含义 |
|---|---|
| `format` | 固定为 `"wpbk"`。 |
| `version` | 格式主版本号。本文为第 1 版。 |
| `id` | 随机的仓库 ID。 |
| `created` | 创建时间。 |
| `chunker` | 写入方必须使用的分块参数（见[分块](#分块)）。 |
| `compression` | `"zstd"`，v1 唯一取值。 |
| `hash` | `"blake3-256"`，v1 唯一取值。 |
| `encryption` | 未加密仓库为 `null`；加密仓库为一个对象（见[加密](#加密)）。未实现加密的读取器必须拒绝 `null` 以外的任何值。 |
| `packSize` | 写入方的目标包大小（字节）。读取器不需要。 |

## 分块

只有写入方需要分块；读取器不需要本节。默认参数：`min` = 256 KiB，`avg` = 1 MiB，`max` = 4 MiB。
`avg` 必须是 2 的幂，且 `64 <= min < avg < max`。

gear 表有 256 个无符号 64 位条目。第 `i` 项是 `BLAKE3-256("wpbk gear v1" || byte(i))` 前 8 字节按小端序
解释的 `uint64`，其中前缀为 12 个 ASCII 字节，`byte(i)` 为一个字节。校验值：
`gear[0] = 0x72d1fab4f91743fc`，`gear[255] = 0x2dd732f8f06d09b8`，全部 256 项按小端序序列化后的
BLAKE3-256 为 `03f5267f6940558e57783e6bfe4ded7b3d75182a2a23f7a64f9c4e83227fe7ee`。

每个 tar 条目的内容单独分块（快照的条目列表也是如此）。从内容起点开始，下一个数据块的长度为
`cut(data)`，其中 `data` 是接下来的 `max` 字节；剩余不足时为全部剩余字节：

```text
cut(data):
  n = len(data)
  if n <= min: return n
  if n > max: n = max
  normal = min(avg, n)
  b = log2(avg)
  maskS = top (b + 2) bits set   // 0xFFFFFC0000000000 for avg = 1 MiB
  maskL = top (b - 2) bits set   // 0xFFFFC00000000000 for avg = 1 MiB
  fp = 0 (uint64, arithmetic wraps)
  for i in [min, normal): fp = (fp << 1) + gear[data[i]]; if fp & maskS == 0: return i + 1
  for i in [normal, n):   fp = (fp << 1) + gear[data[i]]; if fp & maskL == 0: return i + 1
  return n
```

空内容没有数据块。除内容流的最后一个数据块外，每个数据块至少 `min` 字节；没有数据块超过 `max`。

## 包文件

包是位于头部与尾部之间的一串 blob：

| 偏移 | 大小 | 内容 |
|---|---|---|
| 0 | 8 | ASCII `WPBKPAK1` |
| 8 | 可变 | 首尾相接的 blob；每个 blob 是一个数据块的完整 zstd 帧，加密仓库中经过封装 |
| `S - 16 - T` | `T` | 尾部目录：JSON 文档（UTF-8），加密仓库中经过封装 |
| `S - 16` | 8 | `T`，尾部目录长度，小端序 `uint64` |
| `S - 8` | 8 | ASCII `WPBKEND1` |

`S` 为文件大小。尾部目录为：

```json
{"version": 1, "blobs": [{"id": "<chunk id>", "offset": 8, "length": 51234, "rawLength": 1048576}]}
```

`offset` 与 `length` 定位存储的 blob 在文件中的位置，`rawLength` 是明文长度。包 ID 是整个文件的
BLAKE3-256。包中只有 blob；每个数据块用标准 zstd 解码器独立解码（加密仓库中先解封），结果的哈希必须等于
其 ID。

## 索引文件

索引文件把数据块映射到包。一个仓库可以有任意多个索引文件，它们的并集就是索引。文件名是按存储形式的文件
内容（加密仓库中为封装后的内容）的 BLAKE3-256。

```json
{"version": 1, "packs": [{"id": "<pack id>", "size": 33561020, "blobs": [{"id": "<chunk id>", "offset": 8, "length": 51234, "rawLength": 1048576}]}]}
```

每个 `blobs` 列表与该包的尾部目录相同。索引是缓存：索引丢失或损坏时，读取所有包的尾部目录即可得到相同
信息。同一数据块可以出现在多个包中；任何通过校验的副本都可以使用。

## 快照

### 快照文件

`snapshots/<id>.json`，其中 `<id>` 是随机快照 ID，与 `id` 字段相同。加密仓库中该文件保存的是下面文档
封装后的内容：

```json
{
  "version": 1,
  "id": "<snapshot id>",
  "time": "2026-11-05T10:00:00Z",
  "source": {"kind": "wsl-distro", "name": "Ubuntu", "host": "DESKTOP-1"},
  "tags": ["manual"],
  "tool": "WSLPilot 0.0.1",
  "parent": "<snapshot id>",
  "tree": ["<chunk id>", "<chunk id>"],
  "totals": {"entries": 11, "bytes": 10485880, "newChunks": 15, "newBytes": 10497730}
}
```

| 字段 | 含义 |
|---|---|
| `source` | 备份对象。`kind` 为 `wsl-distro`、`wslc-volume` 或 `compose-project`；其它情况自由填写。 |
| `tags` | 可选标签。 |
| `tool` | 写入程序及其版本。 |
| `parent` | 可选，同一来源的上一个快照（WSLPilot 按 `time` 选最新的）。仅供参考：它可能指向之后已被 forget 的快照，读取方恢复时从不需要它。 |
| `tree` | 明文拼接后构成条目列表的数据块。 |
| `totals` | 条目数、内容字节数，以及本次备份新增的数据块数与压缩后字节数。 |

### 条目列表

`tree` 中数据块拼接后是一个 JSON Lines 文档：每行一个 JSON 对象，每个 tar 条目一行，保持原 tar 顺序。
空 tar 的列表为空（`tree` 可以是 `[]`）。

| 字段 | 含义 |
|---|---|
| `name` | 字节串：tar 流中的条目路径。 |
| `type` | tar 类型标志，单字符字符串：`"0"` 文件，`"1"` 硬链接，`"2"` 符号链接，`"3"` 字符设备，`"4"` 块设备，`"5"` 目录，`"6"` FIFO，其它按原样。 |
| `mode` | tar 头部中的权限位（整数）。 |
| `uid`、`gid` | 数字属主。 |
| `uname`、`gname` | 字节串；为空时省略。 |
| `mtime` | 完整精度的修改时间。 |
| `atime`、`ctime` | 访问时间与状态变更时间；tar 中没有时省略。 |
| `linkname` | 字节串：符号链接或硬链接目标；为空时省略。 |
| `devmajor`、`devminor` | 设备号；为零时省略。 |
| `size` | 头部中的内容大小。 |
| `format` | 读取该条目时识别的 tar 头部格式，位集合：2 USTAR，4 PAX，8 GNU；未知时省略。 |
| `pax` | 上述字段尚未表达的 PAX 记录，按键排序：`[{"key": <字节串>, "value": <字节串>}]`。xattr 为 `SCHILY.xattr.<name>` 记录。 |
| `chunks` | 按顺序排列的内容数据块 ID；没有内容的条目省略。 |

键 `path`、`linkpath`、`size`、`uid`、`gid`、`uname`、`gname`、`mtime`、`atime`、`ctime` 以及
`GNU.sparse.*` 永远不会出现在 `pax` 中；以上字段为准。GNU 稀疏文件以展开形式存储为类型 `"0"`：空洞变为
零字节。硬链接存储为类型 `"1"` 的条目，恢复后仍是硬链接。

## 加密

加密在创建仓库时选择，之后不能开启或关闭；只能更改密码。`config.json` 中非 `null` 的 `encryption` 对象
标志着加密仓库：

```json
"encryption": {
  "algorithm": "aes-256-gcm",
  "kdf": {"name": "argon2id", "salt": "<base64 of 16 random bytes>", "time": 3, "memory": 65536, "threads": 4},
  "key": "<base64 of 60 bytes: nonce || AES-256-GCM(KEK, master key)>"
}
```

| 字段 | 含义 |
|---|---|
| `algorithm` | `"aes-256-gcm"`，v1 唯一取值。 |
| `kdf.name` | `"argon2id"`，v1 唯一取值。 |
| `kdf.salt` | Base64 盐值；写入方使用 16 个随机字节。 |
| `kdf.time`、`kdf.memory`、`kdf.threads` | Argon2id 迭代次数、内存（KiB）与并行度。写入方使用 3、65536（64 MiB）和 4。 |
| `key` | 包装后主密钥的 Base64：12 字节 nonce，随后是在 KEK 下 AES-256-GCM 的 32 字节密文和 16 字节标签，无附加数据。 |

### 密钥

```text
KEK        = Argon2id(password as UTF-8, kdf.salt, kdf.time, kdf.memory, kdf.threads, 32)  // RFC 9106, version 0x13
master key = AES-256-GCM-Open(KEK, nonce = key[0:12], ciphertext||tag = key[12:60], aad = empty)
data key   = HKDF-SHA256(master key, salt = empty, info = "wpbk v1 data key", 32)       // RFC 5869
id key     = HKDF-SHA256(master key, salt = empty, info = "wpbk v1 chunk id key", 32)
```

主密钥是创建仓库时生成的 32 个随机字节，永不改变。解开 `key` 时标签校验失败表示密码错误，因此读取器只凭
`config.json` 就能得知。更改密码会写入新的盐值和包装同一主密钥的新 `key`；仓库中的其它内容都不变。

### 封装数据

`seal(aad, data)` 是 12 字节随机 nonce，后接 AES-256-GCM(data key, nonce, data, aad)：先是密文，再是 16
字节标签，总长比 `data` 多 28 字节。解封是其逆过程；标签校验失败表示数据已损坏或被篡改。

| 对象 | 未加密 | 加密 |
|---|---|---|
| 数据块 ID | 明文的 BLAKE3-256 | 明文的 BLAKE3 带密钥哈希，密钥为 id key，32 字节输出 |
| Blob | zstd 帧 | `seal(chunk id as 32 raw bytes, zstd frame)` |
| 包尾部目录（`T` 字节） | JSON | `seal("wpbk trailer", JSON)` |
| 索引文件 | JSON | `seal("wpbk index", JSON)` |
| 快照文件 | JSON | `seal("wpbk snapshot", JSON)` |

附加数据字符串为不带终止符的 ASCII。其它一切保持上文所述：布局、文件名、包的头部与尾部标记，以及封装数据
内部的 JSON 文档。blob 的 `length` 是封装后的长度；`rawLength` 仍是明文长度。包和索引 ID 仍是存储字节的
无密钥 BLAKE3-256，因此无需密码即可检查文件完整性。条目列表以数据块形式存储，因而像其它 blob 一样被封装。
快照文件保留 `.json` 名称，但内容是二进制数据。`config.json` 和锁文件不加密；它们不含任何备份数据（锁只
记录主机名、进程 ID 和时间）。

### 威胁模型

- 保护静态存储的备份内容与元数据（名称、大小、属主、xattr、来源、标签、时间），防范获得仓库文件的任何人，并能发现对存储数据的修改。
- 带密钥的数据块 ID 使他人无法通过哈希确认某个已知文件在仓库中；仓库内的去重照常工作。
- 不隐藏仓库大小、包/索引/快照文件的数量和大小，也不隐藏备份写入的时间。
- 没有恢复手段：丢失密码即丢失数据。

## 锁

所有写入方通过 `locks/` 中的文件协调：

```json
{"version": 1, "exclusive": true, "host": "DESKTOP-1", "pid": 4242, "created": "2026-11-05T10:00:00Z", "refreshed": "2026-11-05T10:05:00Z"}
```

1. 写入自己的锁文件 `locks/<随机 ID>.json`（临时文件加重命名）。
2. 读取其它所有锁文件。`refreshed` 早于 30 分钟之前，或 `host` 为本机且进程 `pid` 不在运行时，该锁为
   **过期** 锁。删除过期锁。
3. 若要获取独占锁而仍有其它锁，或要获取共享锁而仍有独占锁，则删除自己的锁并失败（"locked"）。两个同时
   冲突的写入方都会退让；稍后重试。
4. 工作期间每 5 分钟重写一次 `refreshed`；完成后删除文件。

备份、forget、保留策略、清理和索引重建获取 **独占** 锁。恢复、列出条目和检查获取 **共享** 锁。以
**只读** 方式打开的仓库（例如来自其它电脑的仓库）不写入任何内容，连锁文件也不写；读取方接受并发清理可能
导致读取失败，并且从不修改任何内容。

## 写入规则

- 所有文件先写入目标目录中的临时文件，同步后再重命名。因此可见的包、索引或快照文件总是完整的。
- 一次备份按以下顺序写入：它的包、一个列出这些包的索引文件、快照文件。若中途停止，不会出现快照；已完成的
  包作为未索引的包保留。
- 写入方只对所在包存在且长度足以容纳它们的数据块去重。
- **清理** 计算所有快照引用的数据块，重写含有未引用 blob 的包（校验后复制存活的 blob，不重新压缩），写入一个
  列出所有保留包的新索引，然后删除旧索引文件，最后删除废弃的包。只要有快照或条目列表无法读取，就拒绝执行。
- **重建索引** 读取每个包的尾部目录，写入一个包含全部包的索引（尾部目录损坏的包保留其旧索引条目），然后
  删除旧索引文件。
- **检查** 校验布局、每个已索引的包存在且其尾部目录与索引一致、每个被引用的数据块都有副本。启用数据校验时，
  还会解封（加密仓库中校验 GCM 标签）、解压并重新哈希每个 blob，并重新哈希每个包文件。检查会指出无法恢复的
  快照。
- **更改密码**（加密仓库）获取独占锁，且只重写 `config.json`。

## 保留策略

策略有四个计数：保留最近 N 个快照，以及最近 N 天、N 个 ISO 周、N 个月（本地时间）中每个周期最新的快照。
策略按来源（kind、name 与 host）分别应用。任一规则保留的快照都会保留；四个计数全为零的策略保留全部快照。
forget 一个快照只删除其快照文件；空间由清理回收。

## 兼容性

- `config.json` 中的 `version` 是格式主版本号。读取器必须拒绝不认识的版本，并提示需要更新的 WSLPilot。
  本实现读取第 1 版。
- 同一主版本内只允许增量修改（新增可选 JSON 字段）。读取器忽略未知字段，因此旧读取器仍可工作。
- 索引、包尾部目录、快照和锁文档各自带有 `version`；读取器拒绝高于自身所知的版本。
- 加密在第 1 版中已预留，不改变 `version`：标志加密仓库的是非 `null` 的 `encryption` 对象。未实现
  [加密](#加密) 的读取器，或不认识其 `algorithm` 或 `kdf.name` 的读取器，必须拒绝该仓库而不是尝试读取。
  读取器可以拒绝超出自身资源的 KDF 参数；本实现接受 `time` 最大 64、`memory` 最大 4 GiB。
- 之后所有 WSLPilot 版本都必须继续读取第 1 版。新的主版本只能通过明确且经确认的仓库升级引入，绝不会隐式
  写入。

## 手工恢复

1. 读取 `config.json`；检查 `format`、`version`、`compression`、`hash` 与 `encryption`。若 `encryption`
   不是 `null`，用密码派生 data key 和 id key（见[密钥](#密钥)），并在使用下面的每个索引文件、包尾部目录、
   快照文件和 blob 之前先解封。
2. 建立数据块映射：读取所有 `index/*.json`；若索引丢失，则读取所有包的尾部目录（读最后 16 字节，检查
   `WPBKEND1`，再读取其前面的 `T` 字节）。
3. 读取 `snapshots/<id>.json`。对 `tree` 中的每个 ID：打开 `packs/<id[0:2]>/<pack>.pack`，在 `offset`
   处读取 `length` 字节，zstd 解码，检查结果的 BLAKE3-256（加密仓库中用 id key 带密钥计算）等于数据块 ID。
   把结果拼接起来。
4. 将结果按行拆分，每行解析为一个条目。
5. 对每个条目，用其字段（类型、名称、权限、属主、时间、链接目标、设备号、大小、PAX 记录）写出 tar 头部，
   再写出其 `chunks` 的明文拼接。类型不是 `"0"` 且没有数据块的条目以大小 0 写出。记录的 `format`
   能表达该头部时使用它，否则使用 PAX；支持 PAX 的 tar 写入器能保留所有字段。最后以两个全零块结束归档。

```text
for line in concat(chunk(id) for id in snapshot.tree).split("\n"):
    e = json(line)
    tar.write_header(e)
    for id in e.chunks:
        tar.write(zstd_decode(unseal(pack(id).read(offset, length))))  // unseal = identity when unencrypted
```

## 依赖库

| 库 | 用途 | 许可证 |
|---|---|---|
| `github.com/klauspost/compress/zstd` | zstd 压缩 | BSD-3-Clause（部分为 Apache-2.0） |
| `github.com/klauspost/cpuid/v2` | BLAKE3 使用的 CPU 特性检测 | MIT |
| `github.com/zeebo/blake3` | BLAKE3-256 与带密钥 BLAKE3 | CC0-1.0 |
| `golang.org/x/crypto/argon2` | Argon2id 密码密钥派生 | BSD-3-Clause |

四者均与 GPL-3.0 兼容。HKDF-SHA256 与 AES-256-GCM 来自 Go 标准库。所有库都是纯 Go 实现，可选汇编加速；
不使用 cgo。
