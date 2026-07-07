// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-fsctl/cowclone authors

//go:build darwin

package cowclone

import (
	"errors"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// swapClonefile replaces the clonefile(2) seam for the duration of a test and
// restores it on cleanup, so cloneCoW's errno branches can be driven
// deterministically from a tmp dir that is always APFS on the same volume
// (where the real syscall only ever succeeds).
func swapClonefile(t *testing.T, fn func(src, dst string, flags int) error) {
	t.Helper()
	orig := clonefile
	clonefile = fn
	t.Cleanup(func() { clonefile = orig })
}

// TestCloneCoW_RealClonefileSucceeds exercises the genuine APFS clonefile(2) on
// the runner's temp dir (a real reflink) — the success branch, unfaked.
func TestCloneCoW_RealClonefileSucceeds(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("cloned via APFS"))
	dst := filepath.Join(dir, "dst.img")

	if err := cloneCoW(src, dst); err != nil {
		t.Fatalf("cloneCoW: want nil on real APFS clonefile, got %v", err)
	}
	assertContent(t, dst, []byte("cloned via APFS"))
}

// TestCloneCoW_UnsupportedErrnos verifies the errnos that mean "can't share
// blocks here" (non-APFS volume / cross-volume) collapse to errCoWUnsupported.
func TestCloneCoW_UnsupportedErrnos(t *testing.T) {
	for _, errno := range []unix.Errno{unix.ENOTSUP, unix.EXDEV} {
		t.Run(errno.Error(), func(t *testing.T) {
			swapClonefile(t, func(_, _ string, _ int) error { return errno })
			dir := t.TempDir()
			src := writeFile(t, dir, "src.img", []byte("x"))
			if err := cloneCoW(src, filepath.Join(dir, "dst.img")); !errors.Is(err, errCoWUnsupported) {
				t.Fatalf("cloneCoW(%v): want errCoWUnsupported, got %v", errno, err)
			}
		})
	}
}

// TestCloneCoW_SurfacesRealErrno verifies a non-"unsupported" errno (here
// EACCES) from clonefile is surfaced rather than swallowed as a fallback signal.
func TestCloneCoW_SurfacesRealErrno(t *testing.T) {
	swapClonefile(t, func(_, _ string, _ int) error { return unix.EACCES })
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))
	if err := cloneCoW(src, filepath.Join(dir, "dst.img")); !errors.Is(err, unix.EACCES) {
		t.Fatalf("cloneCoW: want EACCES surfaced, got %v", err)
	}
}
