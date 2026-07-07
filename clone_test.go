// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-fsctl/cowclone authors

package cowclone

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forceCloneResult swaps the CoW primitive for the duration of a test so the
// fallback/error orchestration in Clone is exercised regardless of the test
// host's filesystem. The original is restored on cleanup.
func forceCloneResult(t *testing.T, err error) {
	t.Helper()
	orig := cloneCoWFn
	cloneCoWFn = func(_, _ string) error { return err }
	t.Cleanup(func() { cloneCoWFn = orig })
}

// writeFile writes b to <dir>/<name> and returns the path.
func writeFile(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// assertContent fails unless the file at path has exactly want bytes.
func assertContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: content mismatch (got %d bytes, want %d bytes)", path, len(got), len(want))
	}
}

// TestClone_CopiesContents covers the happy path: dst becomes a byte-identical
// copy of src. On APFS/reflink filesystems this exercises the real CoW clone;
// elsewhere it exercises the byte-copy fallback. Either way dst must match src.
func TestClone_CopiesContents(t *testing.T) {
	dir := t.TempDir()
	want := []byte("the quick brown fox jumps over the lazy dog")
	src := writeFile(t, dir, "src.img", want)
	dst := filepath.Join(dir, "dst.img")

	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	assertContent(t, dst, want)
}

// TestClone_OverwritesExistingDst verifies Clone replaces a pre-existing dst
// (the CoW clone primitive fails if dst exists; Clone removes it first).
func TestClone_OverwritesExistingDst(t *testing.T) {
	dir := t.TempDir()
	want := []byte("fresh contents")
	src := writeFile(t, dir, "src.img", want)
	dst := writeFile(t, dir, "dst.img", []byte("stale contents that should be gone"))

	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	assertContent(t, dst, want)
}

// TestClone_EmptyFile covers a zero-byte source.
func TestClone_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", nil)
	dst := filepath.Join(dir, "dst.img")

	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	assertContent(t, dst, nil)
}

// TestClone_LargeRandom clones a multi-block random payload and compares
// hashes, so a truncated or block-misaligned clone/copy is caught.
func TestClone_LargeRandom(t *testing.T) {
	dir := t.TempDir()
	want := make([]byte, 4<<20+777) // 4 MiB + a non-block-aligned tail
	if _, err := rand.Read(want); err != nil {
		t.Fatalf("rand: %v", err)
	}
	src := writeFile(t, dir, "src.img", want)
	dst := filepath.Join(dir, "dst.img")

	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if sha256.Sum256(got) != sha256.Sum256(want) {
		t.Fatalf("dst hash mismatch (got %d bytes, want %d bytes)", len(got), len(want))
	}
}

// TestClone_Independence is the property the whole feature relies on: after a
// clone, writing to one file must not change the other. This holds for both a
// real CoW clone (the write breaks block sharing) and the byte-copy fallback.
func TestClone_Independence(t *testing.T) {
	dir := t.TempDir()
	base := bytes.Repeat([]byte("A"), 1024)
	src := writeFile(t, dir, "golden.img", base)
	dst := filepath.Join(dir, "instance.img")

	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}

	// Mutate dst; src must be untouched.
	if err := os.WriteFile(dst, bytes.Repeat([]byte("B"), 1024), 0o644); err != nil {
		t.Fatalf("rewrite dst: %v", err)
	}
	assertContent(t, src, base)

	// Mutate src; dst must keep the value it was given above.
	if err := os.WriteFile(src, bytes.Repeat([]byte("C"), 1024), 0o644); err != nil {
		t.Fatalf("rewrite src: %v", err)
	}
	assertContent(t, dst, bytes.Repeat([]byte("B"), 1024))
}

// TestClone_MissingSrc surfaces a real error (not a silent fallback success)
// when the source doesn't exist.
func TestClone_MissingSrc(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.img")

	if err := Clone(filepath.Join(dir, "nope.img"), dst); err == nil {
		t.Fatal("Clone: expected error for missing src, got nil")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("dst should not exist after a failed clone, stat err = %v", err)
	}
}

// TestClone_MissingDstParent surfaces an error when dst's directory is absent.
func TestClone_MissingDstParent(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))

	if err := Clone(src, filepath.Join(dir, "no-such-dir", "dst.img")); err == nil {
		t.Fatal("Clone: expected error for missing dst parent, got nil")
	}
}

// TestCopyFile_Fallback exercises the byte-copy fallback directly, so the
// fallback branch is covered even on platforms (APFS/reflink) where Clone takes
// the CoW path instead.
func TestCopyFile_Fallback(t *testing.T) {
	dir := t.TempDir()
	want := []byte("fallback payload")
	src := writeFile(t, dir, "src.img", want)
	dst := filepath.Join(dir, "dst.img")

	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	assertContent(t, dst, want)
}

// TestCopyFile_OpenSrcError covers the src-open failure branch (missing src).
func TestCopyFile_OpenSrcError(t *testing.T) {
	dir := t.TempDir()

	if err := copyFile(filepath.Join(dir, "nope.img"), filepath.Join(dir, "dst.img")); err == nil ||
		!strings.Contains(err.Error(), "open src") {
		t.Fatalf("copyFile: want an open-src error, got %v", err)
	}
}

// TestCopyFile_OpenDstError covers the dst-open failure branch (parent dir
// absent).
func TestCopyFile_OpenDstError(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))

	if err := copyFile(src, filepath.Join(dir, "no-such-dir", "dst.img")); err == nil ||
		!strings.Contains(err.Error(), "open dst") {
		t.Fatalf("copyFile: want an open-dst error, got %v", err)
	}
}

// TestCopyFile_ReadError covers the io.Copy failure branch: a directory opens
// fine but reading from its fd fails (EISDIR), so the copy errors out.
func TestCopyFile_ReadError(t *testing.T) {
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	if err := os.Mkdir(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	if err := copyFile(srcDir, filepath.Join(dir, "dst.img")); err == nil ||
		!strings.Contains(err.Error(), "copy") {
		t.Fatalf("copyFile: want a copy (read) error, got %v", err)
	}
}

// errWriteCloser writes to an embedded buffer but returns a chosen error from
// Close, so the "close dst" write-back-failure branch of copyFile can be
// exercised deterministically.
type errWriteCloser struct {
	closeErr error
}

func (w errWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (w errWriteCloser) Close() error                { return w.closeErr }

// TestCopyFile_CloseDstError covers the branch where the copy streams fine but
// the final dst Close fails (a flush/commit error that must not be swallowed).
func TestCopyFile_CloseDstError(t *testing.T) {
	boom := errors.New("close-dst boom")
	orig := openDstFile
	openDstFile = func(string) (io.WriteCloser, error) { return errWriteCloser{closeErr: boom}, nil }
	t.Cleanup(func() { openDstFile = orig })

	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("payload"))

	if err := copyFile(src, filepath.Join(dir, "dst.img")); err == nil ||
		!strings.Contains(err.Error(), "close dst") {
		t.Fatalf("copyFile: want a close-dst error, got %v", err)
	}
}

// TestClone_FallsBackWhenUnsupported drives Clone's fallback path: the CoW
// primitive reports the filesystem can't share blocks, so Clone must copy and
// still produce a byte-identical dst.
func TestClone_FallsBackWhenUnsupported(t *testing.T) {
	forceCloneResult(t, errCoWUnsupported)
	dir := t.TempDir()
	want := []byte("copied because reflink said no")
	src := writeFile(t, dir, "src.img", want)
	dst := filepath.Join(dir, "dst.img")

	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	assertContent(t, dst, want)
}

// TestClone_CoWSuccess covers Clone's fast path: the CoW primitive succeeds, so
// Clone returns immediately without touching the byte-copy fallback. Forced so
// the branch is covered on filesystems (ext4 CI, tmpfs, …) where a real reflink
// isn't available and the primitive would otherwise report unsupported.
func TestClone_CoWSuccess(t *testing.T) {
	forceCloneResult(t, nil)
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))
	dst := filepath.Join(dir, "dst.img")

	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: want nil on CoW success, got %v", err)
	}
}

// TestClone_SurfacesRealCloneError verifies a non-sentinel clone error is
// surfaced rather than silently retried as a copy.
func TestClone_SurfacesRealCloneError(t *testing.T) {
	boom := errors.New("ENOSPC-ish boom")
	forceCloneResult(t, boom)
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))
	dst := filepath.Join(dir, "dst.img")

	err := Clone(src, dst)
	if !errors.Is(err, boom) {
		t.Fatalf("Clone: want wrapped %v, got %v", boom, err)
	}
}

// TestClone_FallbackCopyError covers the branch where the CoW clone is
// unsupported and the byte-copy fallback then also fails (here: missing src).
func TestClone_FallbackCopyError(t *testing.T) {
	forceCloneResult(t, errCoWUnsupported)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.img")

	err := Clone(filepath.Join(dir, "missing.img"), dst)
	if err == nil || !strings.Contains(err.Error(), "copy") {
		t.Fatalf("Clone: want a wrapped copy error, got %v", err)
	}
}

// TestClone_DstRemoveError covers Clone's failure to clear a pre-existing dst:
// a non-empty directory can't be removed by os.Remove, so Clone reports it
// instead of clobbering.
func TestClone_DstRemoveError(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "src.img", []byte("x"))
	dstDir := filepath.Join(dir, "dst.img")
	if err := os.Mkdir(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	writeFile(t, dstDir, "occupant", []byte("keeps the dir non-empty"))

	if err := Clone(src, dstDir); err == nil || !strings.Contains(err.Error(), "remove dst") {
		t.Fatalf("Clone: want a remove-dst error, got %v", err)
	}
}
