// SPDX-License-Identifier: BSD-3-Clause
//
// Copyright (c) 2026, the go-fsctl/cowclone authors

//go:build !darwin && !linux

package cowclone

// cloneCoW always reports the clone as unsupported on platforms without a known
// copy-on-write clone primitive (windows, the BSDs, …), so Clone falls back to
// a byte copy. The signature matches the per-OS implementations; the arguments
// are unused here.
func cloneCoW(_, _ string) error { return errCoWUnsupported }
