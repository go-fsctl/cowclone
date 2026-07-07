// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-fsctl/cowclone authors

//go:build darwin

package cowclone

import "golang.org/x/sys/unix"

// clonefile is the indirection seam over the APFS clonefile(2) syscall cloneCoW
// drives on darwin. It exists so the errno branches of the clone — which only
// trigger on a non-APFS or cross-volume target that is impractical to provoke
// from the test host's tmp dir (always APFS, same volume) — can be exercised
// deterministically by a fault-injecting fake in tests. Production code uses
// unix.Clonefile assigned here; tests swap the var, run, and restore it. The
// real success path still runs against the macOS runner's APFS temp dir.
var clonefile = unix.Clonefile
