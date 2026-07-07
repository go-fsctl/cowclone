// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-fsctl/cowclone authors

//go:build linux

package cowclone

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// swapIoctl replaces the FICLONE seam for the duration of a test and restores it
// on cleanup, so cloneCoW's reflink success and errno branches can be driven
// deterministically on a plain ext4 CI filesystem (where the real ioctl only
// ever returns EOPNOTSUPP).
func swapIoctl(t *testing.T, fn func(destFd, srcFd int) error) {
	t.Helper()
	orig := ioctlFileClone
	ioctlFileClone = fn
	t.Cleanup(func() { ioctlFileClone = orig })
}

// TestCloneCoW_ReflinkSuccess drives the FICLONE success branch: the ioctl
// reports blocks shared, so cloneCoW commits dst by closing it and returns nil.
func TestCloneCoW_ReflinkSuccess(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("reflinked"))
	dst := filepath.Join(dir, "dst.img")

	swapIoctl(t, func(_, _ int) error { return nil })
	if err := cloneCoW(src, dst); err != nil {
		t.Fatalf("cloneCoW: want nil on reflink success, got %v", err)
	}
	// dst must exist (it was created before the ioctl and committed on success).
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("dst should exist after a successful reflink: %v", err)
	}
}

// TestCloneCoW_UnsupportedErrnos verifies every errno the kernel uses to signal
// "this filesystem can't share blocks" collapses to errCoWUnsupported so Clone
// falls back to a byte copy, and that the empty dst is removed first.
func TestCloneCoW_UnsupportedErrnos(t *testing.T) {
	for _, errno := range []unix.Errno{
		unix.EOPNOTSUPP, unix.ENOTSUP, unix.EXDEV, unix.ENOSYS, unix.EINVAL,
	} {
		t.Run(errno.Error(), func(t *testing.T) {
			dir := t.TempDir()
			src := writeFile(t, dir, "src.img", []byte("x"))
			dst := filepath.Join(dir, "dst.img")

			swapIoctl(t, func(_, _ int) error { return errno })
			if err := cloneCoW(src, dst); !errors.Is(err, errCoWUnsupported) {
				t.Fatalf("cloneCoW(%v): want errCoWUnsupported, got %v", errno, err)
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("dst should be removed after an unsupported clone, stat err = %v", err)
			}
		})
	}
}

// TestCloneCoW_SurfacesRealErrno verifies a non-"unsupported" errno (here
// EACCES) is surfaced rather than swallowed as a fallback signal.
func TestCloneCoW_SurfacesRealErrno(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))
	dst := filepath.Join(dir, "dst.img")

	swapIoctl(t, func(_, _ int) error { return unix.EACCES })
	if err := cloneCoW(src, dst); !errors.Is(err, unix.EACCES) {
		t.Fatalf("cloneCoW: want EACCES surfaced, got %v", err)
	}
}

// TestCloneCoW_OpenSrcError covers the src-open failure branch via the osOpen
// seam.
func TestCloneCoW_OpenSrcError(t *testing.T) {
	boom := errors.New("open-src boom")
	orig := osOpen
	osOpen = func(string) (*os.File, error) { return nil, boom }
	t.Cleanup(func() { osOpen = orig })

	dir := t.TempDir()
	if err := cloneCoW(filepath.Join(dir, "src.img"), filepath.Join(dir, "dst.img")); !errors.Is(err, boom) {
		t.Fatalf("cloneCoW: want %v, got %v", boom, err)
	}
}

// TestCloneCoW_OpenDstError covers the dst-open failure branch via the
// osOpenFile seam (osOpen stays real so the source opens fine first).
func TestCloneCoW_OpenDstError(t *testing.T) {
	boom := errors.New("open-dst boom")
	orig := osOpenFile
	osOpenFile = func(string, int, os.FileMode) (*os.File, error) { return nil, boom }
	t.Cleanup(func() { osOpenFile = orig })

	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))
	if err := cloneCoW(src, filepath.Join(dir, "dst.img")); !errors.Is(err, boom) {
		t.Fatalf("cloneCoW: want %v, got %v", boom, err)
	}
}
