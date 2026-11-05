# Incremental backup repository format (wpbk v1)

This is the authoritative specification of the `wpbk` repository format, version 1. It is written
so that a reader who has only this document, a zstd decoder and a BLAKE3 implementation can
restore every snapshot. `internal/backup/wpbk` is one implementation; its `spec_test.go` contains
an independent reader written from this document and must keep passing.

Status: implemented in `internal/backup/wpbk`, not yet released. The format becomes frozen with the
first release that writes it.

## Model

A repository stores **snapshots** of **tar streams**. The unit of input is a tar stream because
Linux metadata (owner, mode, times, links, xattrs, device numbers) travels as tar.

- Each tar entry is recorded with its complete header, field by field, plus the ordered list of the
  **chunks** that make up its content. The tar stream itself is never stored byte for byte; it is
  regenerated on restore. Single entries can therefore be listed and restored.
- Content is split by a content-defined **chunker**. A chunk's id is the BLAKE3-256 hash of its
  plaintext; a chunk already in the repository is never stored again, across all snapshots.
- Chunks are compressed with zstd and stored as **blobs** inside **pack** files of about 32 MiB.
  Each pack ends with its own table of contents, so the **index** can be rebuilt from packs alone.

## Conventions

- **Id**: 32 bytes written as 64 lowercase hexadecimal characters. Chunk, pack and index ids are
  the BLAKE3-256 (unkeyed, 32-byte output) of the chunk plaintext, the whole pack file and the whole
  index file respectively. Snapshot, repository and lock ids are random.
- **JSON**: UTF-8, RFC 8259. Readers must ignore fields they do not know.
- **Time**: an RFC 3339 string with optional fractional seconds (nanosecond precision), for example
  `"2026-03-04T05:06:07.123456789Z"`. Writers use UTC.
- **Byte string**: names, link targets, user and group names and PAX records are byte strings,
  not text. A byte string that is valid UTF-8 is a JSON string; any other value is an object
  `{"base64": "<standard base64 with padding>"}`.
- **Integers** in binary structures are unsigned little-endian.
- **Temporary files** are named `tmp-*.partial`. Readers ignore them and every other name that
  does not match the layout below.

## Directory layout

```text
<repository>/
  config.json
  packs/<first 2 hex digits of id>/<pack id>.pack
  index/<index id>.json
  snapshots/<snapshot id>.json
  locks/<lock id>.json
```

All five entries must exist; `locks/` may be absent only for read-only access.

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

| Field | Meaning |
|---|---|
| `format` | Always `"wpbk"`. |
| `version` | Format major version. This document is version 1. |
| `id` | Random repository id. |
| `created` | Creation time. |
| `chunker` | Chunking parameters writers must use (see [Chunking](#chunking)). |
| `compression` | `"zstd"`, the only v1 value. |
| `hash` | `"blake3-256"`, the only v1 value. |
| `encryption` | `null` in v1. Any other value means the repository cannot be read by a v1 reader. |
| `packSize` | Target pack size in bytes for writers. Readers do not need it. |

## Chunking

Only writers chunk; a reader never needs this section. Parameters: `min` = 256 KiB,
`avg` = 1 MiB, `max` = 4 MiB by default. `avg` must be a power of two, `64 <= min < avg < max`.

The gear table has 256 unsigned 64-bit entries. Entry `i` is the little-endian `uint64` of the first
8 bytes of `BLAKE3-256("wpbk gear v1" || byte(i))`, where the prefix is the 12 ASCII bytes and
`byte(i)` one byte. Check values: `gear[0] = 0x72d1fab4f91743fc`, `gear[255] = 0x2dd732f8f06d09b8`,
and the BLAKE3-256 of all 256 entries serialized little-endian is
`03f5267f6940558e57783e6bfe4ded7b3d75182a2a23f7a64f9c4e83227fe7ee`.

The content of each tar entry is chunked separately (and so is the entry list of a snapshot).
Starting at the beginning of the content, the next chunk length is `cut(data)`, where `data` is the
next `max` bytes, or everything left if fewer remain:

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

Empty content has no chunks. Every chunk except the last of a content stream is at least `min`
bytes; no chunk is longer than `max`.

## Pack files

A pack is a sequence of blobs between a header and a trailer:

| Offset | Size | Content |
|---|---|---|
| 0 | 8 | ASCII `WPBKPAK1` |
| 8 | variable | blobs, back to back; each blob is one complete zstd frame of one chunk |
| `S - 16 - T` | `T` | trailer: JSON document (UTF-8) |
| `S - 16` | 8 | `T`, the trailer length, `uint64` little-endian |
| `S - 8` | 8 | ASCII `WPBKEND1` |

`S` is the file size. The trailer is:

```json
{"version": 1, "blobs": [{"id": "<chunk id>", "offset": 8, "length": 51234, "rawLength": 1048576}]}
```

`offset` and `length` locate the compressed blob within the file, `rawLength` is the plaintext
length. The pack id is the BLAKE3-256 of the whole file. A pack contains only blobs; each chunk is
decoded independently with a standard zstd decoder, and the result must hash to its id.

## Index files

An index file maps chunks to packs. A repository may have any number of index files; their union
is the index. The file name is the BLAKE3-256 of the file content.

```json
{"version": 1, "packs": [{"id": "<pack id>", "size": 33561020, "blobs": [{"id": "<chunk id>", "offset": 8, "length": 51234, "rawLength": 1048576}]}]}
```

Each `blobs` list equals that pack's trailer. The index is a cache: when it is missing or damaged,
reading every pack trailer gives the same information. A chunk may appear in more than one pack;
any copy that verifies may be used.

## Snapshots

### Snapshot file

`snapshots/<id>.json`, where `<id>` is the random snapshot id and equals the `id` field:

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

| Field | Meaning |
|---|---|
| `source` | What was backed up. `kind` is `wsl-distro`, `wslc-volume` or `compose-project`; free-form for others. |
| `tags` | Optional labels. |
| `tool` | The writing program and version. |
| `parent` | Optional previous snapshot of the same source. Informational only. |
| `tree` | Chunks whose concatenated plaintext is the entry list. |
| `totals` | Entry count, content bytes, and the chunks and compressed bytes this backup added. |

### Entry list

The concatenated `tree` chunks form a JSON Lines document: one JSON object per line, one line per
tar entry, in the original tar order. An empty tar has an empty list (`tree` may be `[]`).

| Field | Meaning |
|---|---|
| `name` | Byte string: entry path as in the tar stream. |
| `type` | Tar type flag as a one-character string: `"0"` file, `"1"` hard link, `"2"` symlink, `"3"` char device, `"4"` block device, `"5"` directory, `"6"` FIFO, others as found. |
| `mode` | Mode bits as stored in the tar header (integer). |
| `uid`, `gid` | Numeric owner. |
| `uname`, `gname` | Byte strings; omitted when empty. |
| `mtime` | Modification time with full precision. |
| `atime`, `ctime` | Access and change time; omitted when the tar had none. |
| `linkname` | Byte string: symlink or hard link target; omitted when empty. |
| `devmajor`, `devminor` | Device numbers; omitted when zero. |
| `size` | Content size from the header. |
| `format` | Tar header format the entry was read as, a bit set: 2 USTAR, 4 PAX, 8 GNU; omitted when unknown. |
| `pax` | PAX records not already expressed by the fields above, sorted by key: `[{"key": <byte string>, "value": <byte string>}]`. Xattrs are `SCHILY.xattr.<name>` records. |
| `chunks` | Content chunk ids in order; omitted for entries without content. |

The keys `path`, `linkpath`, `size`, `uid`, `gid`, `uname`, `gname`, `mtime`, `atime`, `ctime`
and `GNU.sparse.*` never appear in `pax`; the fields above are authoritative. GNU sparse files are
stored expanded, as type `"0"`: holes become zero bytes. Hard links are stored as type `"1"`
entries and stay hard links on restore.

## Locks

Every writer coordinates through files in `locks/`:

```json
{"version": 1, "exclusive": true, "host": "DESKTOP-1", "pid": 4242, "created": "2026-11-05T10:00:00Z", "refreshed": "2026-11-05T10:05:00Z"}
```

1. Write your own lock file `locks/<random id>.json` (temporary file and rename).
2. Read every other lock file. A lock is **stale** when `refreshed` is more than 30 minutes ago, or
   when `host` is this host and no process `pid` runs. Remove stale locks.
3. If you want an exclusive lock and any other lock remains, or you want a shared lock and an
   exclusive one remains, remove your own lock and fail ("locked"). Two writers that collide both
   back off; retry later.
4. Rewrite `refreshed` every 5 minutes while working; remove the file when done.

Backup, forget, retention, prune and index rebuild take an **exclusive** lock. Restore, listing
entries and check take a **shared** lock. A repository opened **read-only** (for example one from
another PC) writes nothing, not even a lock file; the reader accepts that a concurrent prune could
make it fail, and never changes anything.

## Writing rules

- Every file is written to a temporary file in the destination directory, synced, then renamed.
  A visible pack, index or snapshot file is therefore always complete.
- A backup writes, in this order: its packs, one index file listing them, the snapshot file. If it
  stops early, no snapshot appears; completed packs stay as unindexed packs.
- A writer deduplicates only against chunks whose pack exists and is long enough to hold them.
- **Prune** computes the chunks referenced by all snapshots, rewrites packs that hold unreferenced
  blobs (copying live blobs without recompression, after verifying them), writes one new index
  listing all surviving packs, then deletes the old index files, then the obsolete packs. It
  refuses to run while any snapshot or entry list is unreadable.
- **Rebuild index** reads every pack trailer, writes one index with all of them (keeping the old
  index entry of a pack whose trailer is damaged), then deletes the old index files.
- **Check** verifies the layout, that every indexed pack exists and its trailer matches the index,
  and that every referenced chunk has a copy. With data verification it also decompresses and
  rehashes every blob and rehashes every pack file. It names the snapshots that cannot be restored.

## Retention policy

A policy has four counts: keep the last N snapshots, and the newest snapshot of each of the last N
days, ISO weeks and months (local time). It applies separately to each source (kind, name and
host). A snapshot kept by any rule is kept; a policy with every count zero keeps everything.
Forgetting a snapshot only removes its snapshot file; prune reclaims the space.

## Compatibility

- `version` in `config.json` is the format major version. A reader must refuse a repository whose
  version it does not know, with a message that a newer WSLPilot is required. This implementation
  reads version 1.
- Within a major version, changes are only additive (new optional JSON fields). Readers ignore
  unknown fields, so older readers keep working.
- Index, pack trailer, snapshot and lock documents carry their own `version`; a reader rejects a
  higher version than it knows.
- Every later WSLPilot release must keep reading version 1. A new major version is introduced only
  by an explicit, confirmed upgrade of a repository; it is never written implicitly.

## Restoring by hand

1. Read `config.json`; check `format`, `version`, `compression`, `hash` and `encryption`.
2. Build the chunk map: from every `index/*.json`, or, if the index is lost, from every pack
   trailer (read the last 16 bytes, check `WPBKEND1`, read `T` bytes before them).
3. Read `snapshots/<id>.json`. For each id in `tree`: open `packs/<id[0:2]>/<pack>.pack`, read
   `length` bytes at `offset`, zstd-decode, check that the BLAKE3-256 of the result is the chunk id.
   Concatenate the results.
4. Split the result into lines and parse each line as an entry.
5. For each entry write a tar header from its fields (type, name, mode, owner, times, link target,
   device numbers, size, PAX records), then the concatenated plaintext of its `chunks`. An entry
   of a type other than `"0"` without chunks is written with size 0. Use the recorded `format`
   when it can express the header, otherwise PAX; a tar writer that honours PAX keeps every field.
   Finish the archive with two zero blocks.

```text
for line in concat(chunk(id) for id in snapshot.tree).split("\n"):
    e = json(line)
    tar.write_header(e)
    for id in e.chunks:
        tar.write(zstd_decode(pack(id).read(offset, length)))
```

## Libraries

| Library | Use | License |
|---|---|---|
| `github.com/klauspost/compress/zstd` | zstd compression | BSD-3-Clause (parts Apache-2.0) |
| `github.com/klauspost/cpuid/v2` | CPU feature detection for BLAKE3 | MIT |
| `github.com/zeebo/blake3` | BLAKE3-256 | CC0-1.0 |

All three are compatible with GPL-3.0. Both libraries are pure Go with optional assembly; no cgo.
