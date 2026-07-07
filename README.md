<p align="center"><img src="https://raw.githubusercontent.com/go-fsctl/brand/main/social/go-fsctl.png" alt="go-fsctl/cowclone" width="720"></p>

# go-fsctl/cowclone

[![Go Reference](https://pkg.go.dev/badge/github.com/go-fsctl/cowclone.svg)](https://pkg.go.dev/github.com/go-fsctl/cowclone)
[![License: BSD-3-Clause](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![CI](https://github.com/go-fsctl/cowclone/actions/workflows/ci.yml/badge.svg)](https://github.com/go-fsctl/cowclone/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/badge/coverage-100%25-brightgreen)](https://github.com/go-fsctl/cowclone/actions/workflows/ci.yml)

Pure-Go copy-on-write file cloning: share a file's blocks with a new copy in
O(metadata), falling back to a full byte copy when the host filesystem can't —
no cgo, and no shelling out to `cp --reflink` or `clonefile`.

This is the file-level clone sibling in the `go-fsctl` family. Where
[`go-fsctl/btrfs`](https://github.com/go-fsctl/btrfs) and
[`go-fsctl/zfs`](https://github.com/go-fsctl/zfs) drive a specific filesystem's
kernel ioctls, `cowclone` speaks the two portable reflink primitives directly
and degrades gracefully everywhere else:

- **darwin** — APFS `clonefile(2)` (`golang.org/x/sys/unix.Clonefile`).
- **linux** — the `FICLONE` ioctl (`unix.IoctlFileClone`): reflink on btrfs,
  XFS (`reflink=1`), and OpenZFS ≥ 2.2 block cloning.
- **everywhere else**, across filesystems, or on a non-CoW filesystem — a
  streaming byte copy with identical observable semantics.

On a copy-on-write filesystem the clone is near-instant and shares storage with
the source until one side is written (then that side's touched blocks are
copied). The fallback produces the same result — a full, independent `dst` — at
the cost of copying every byte and not sharing space. Either way the caller gets
an independent `dst`.

## Install

```sh
go get github.com/go-fsctl/cowclone
```

## API

```go
import "github.com/go-fsctl/cowclone"

// Make dst a copy-on-write clone of src (replacing dst if it exists).
// Tries a real reflink first; transparently falls back to a byte copy when
// the filesystem can't share blocks or src/dst are on different filesystems.
err := cowclone.Clone("base.img", "instance.img")
```

`Clone(src, dst)` is the whole surface:

1. It removes any pre-existing `dst` (a CoW clone fails if the target already
   exists), returning an error only if that removal genuinely fails.
2. It attempts the platform reflink primitive. On success `dst` is a
   block-sharing clone and `Clone` returns `nil`.
3. If the primitive reports the filesystem can't share blocks
   (`ENOTSUP`/`EOPNOTSUPP`), or that `src` and `dst` live on different
   filesystems (`EXDEV`), or that the ioctl is unimplemented (`ENOSYS`), it
   falls back to a streaming byte copy.
4. Any other error (`ENOENT`, `EACCES`, `ENOSPC`, …) is surfaced, wrapped, and
   *not* silently retried as a copy.

The result is observable-equivalent across all paths: after `Clone`, writing to
one file never affects the other.

## How it works

- **`clone.go`** — the platform-independent orchestration: `Clone` (remove
  stale dst → try reflink → classify the error → fall back or surface) and the
  `copyFile` byte-copy fallback.
- **`clone_darwin.go`** — `cloneCoW` via APFS `clonefile(2)`, mapping
  `ENOTSUP`/`EXDEV` to the internal "unsupported" sentinel.
- **`clone_linux.go`** — `cloneCoW` via the `FICLONE` ioctl into a freshly
  created dst fd, mapping the reflink-unsupported errnos to the sentinel and
  surfacing the rest.
- **`clone_other.go`** — the non-darwin/non-linux stub: always "unsupported", so
  `Clone` takes the byte-copy path.

### Testing without a reflink-capable mount

The kernel reflink calls only fail (or, on plain ext4/tmpfs, only ever return
"unsupported") in situations that are impractical to provoke against a live
filesystem without root or a dedicated btrfs/xfs mount. Following the `go-fsctl`
house style, the OS primitives are reached through indirection **seams**
(`seams_linux.go`, `seams_darwin.go`) — a var over `unix.Clonefile` /
`unix.IoctlFileClone` and the file opens — so tests fault-inject each success
and errno branch deterministically. Every branch, including the reflink success
path and each error errno, is covered on every platform; the byte-copy fallback
runs everywhere. The suite needs neither root nor a special filesystem, so it
runs identically on the native and QEMU CI lanes.

## Platforms

Built and tested on the six 64-bit Go architectures — `amd64`, `arm64`,
`riscv64`, `loong64`, `ppc64le`, `s390x` (big-endian) — plus native macOS
(`darwin/amd64`, `darwin/arm64`) for the real APFS `clonefile` path, and a
cross-build check of the fallback stub on `windows` and `freebsd`.

## License

BSD-3-Clause. See `LICENSE`.
