// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package openclaw

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/larksuite/cli/internal/vfs"
)

// AuditParams holds parameters for AssertSecurePath.
type AuditParams struct {
	TargetPath            string
	Label                 string // e.g. "secrets.providers.vault.command"
	TrustedDirs           []string
	AllowInsecurePath     bool
	AllowReadableByOthers bool
	AllowSymlinkPath      bool
}

// AssertSecurePath verifies that a file/command path is safe for use with
// OpenClaw SecretRef resolution. It returns the effective (possibly
// symlink-resolved) path on success.
func AssertSecurePath(params AuditParams) (string, error) {
	target := params.TargetPath
	label := params.Label

	// 1. Must be absolute path.
	if !filepath.IsAbs(target) {
		return "", fmt.Errorf("%s: path must be absolute, got %q", label, target)
	}

	// 2. Lstat the path — must exist, must not be a directory.
	linfo, err := vfs.Lstat(target)
	if err != nil {
		return "", fmt.Errorf("%s: cannot stat %q: %w", label, target, err)
	}
	if linfo.IsDir() {
		return "", fmt.Errorf("%s: path %q is a directory, not a file", label, target)
	}

	// 3. Symlink handling.
	effectivePath := target
	if linfo.Mode()&os.ModeSymlink != 0 {
		if !params.AllowSymlinkPath {
			return "", fmt.Errorf("%s: path %q is a symlink (not allowed)", label, target)
		}
		resolved, err := vfs.EvalSymlinks(target)
		if err != nil {
			return "", fmt.Errorf("%s: cannot resolve symlink %q: %w", label, target, err)
		}
		rinfo, err := vfs.Lstat(resolved)
		if err != nil {
			return "", fmt.Errorf("%s: cannot stat resolved path %q: %w", label, resolved, err)
		}
		if rinfo.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s: resolved path %q is still a symlink", label, resolved)
		}
		effectivePath = resolved
	}

	// 4. Trusted directory check.
	if len(params.TrustedDirs) > 0 {
		cleaned := filepath.Clean(effectivePath)
		trusted := false
		for _, dir := range params.TrustedDirs {
			cleanDir := filepath.Clean(dir)
			if cleaned == cleanDir || strings.HasPrefix(cleaned, cleanDir+"/") {
				trusted = true
				break
			}
		}
		if !trusted {
			return "", fmt.Errorf("%s: path %q is not inside any trusted directory", label, effectivePath)
		}
	}

	// 5. Permission checks (skip if AllowInsecurePath).
	if !params.AllowInsecurePath {
		info, err := vfs.Stat(effectivePath)
		if err != nil {
			return "", fmt.Errorf("%s: cannot stat %q: %w", label, effectivePath, err)
		}
		mode := info.Mode().Perm()

		if mode&0o002 != 0 {
			return "", fmt.Errorf("%s: path %q is world-writable (mode %04o)", label, effectivePath, mode)
		}
		if mode&0o020 != 0 {
			return "", fmt.Errorf("%s: path %q is group-writable (mode %04o)", label, effectivePath, mode)
		}
		if !params.AllowReadableByOthers {
			if mode&0o004 != 0 {
				return "", fmt.Errorf("%s: path %q is world-readable (mode %04o)", label, effectivePath, mode)
			}
			if mode&0o040 != 0 {
				return "", fmt.Errorf("%s: path %q is group-readable (mode %04o)", label, effectivePath, mode)
			}
		}

		// 6. Owner UID check (platform-specific; no-op on Windows).
		if err := checkOwnerUID(effectivePath, label); err != nil {
			return "", err
		}
	}

	return effectivePath, nil
}
