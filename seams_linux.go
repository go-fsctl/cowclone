// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-fsctl/cowclone authors

//go:build linux

package cowclone

import (
	"os"

	"golang.org/x/sys/unix"
)

// Indirection seams over the operating-system and ioctl primitives cloneCoW
// drives on Linux. They exist so the branches of the FICLONE reflink path —
// the clone success, and each errno the kernel can return — can be exercised
// deterministically by fault-injecting fakes in tests. On a plain ext4 CI
// filesystem FICLONE only ever returns EOPNOTSUPP, so neither the success path
// nor the other errnos would otherwise be reachable without a btrfs/xfs mount
// or root. Production code uses the real implementations assigned here; tests
// swap a var, run, and restore it.
var (
	osOpen     = os.Open
	osOpenFile = os.OpenFile

	// ioctlFileClone is the seam over the raw FICLONE ioctl. It clones the
	// blocks of srcFd into destFd, returning the kernel's wrapped errno (or nil
	// on success). Tests swap it for a fake that returns a chosen errno,
	// covering the success branch and every errno branch without a real
	// reflink-capable filesystem.
	ioctlFileClone = unix.IoctlFileClone
)
