// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package selfupdate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"code.byted.org/lark_search/larksuite-cli/internal/vfs"
)

const (
	MemoryDefaultBranch = "jhn_memory"

	memoryUpdateGitTimeout   = 2 * time.Minute
	memoryUpdateBuildTimeout = 10 * time.Minute
	memoryModulePath         = "code.byted.org/lark_search/larksuite-cli"
	memoryDefaultRepoURL     = "https://github.com/arnold9672/cli.git"
	memoryManagedSkillsFile  = "memory-managed-skills.txt"
	memoryMinimumGoMajor     = 1
	memoryMinimumGoMinor     = 23
)

var (
	memoryGoReadDir        = vfs.ReadDir
	memoryGoVersionPattern = regexp.MustCompile(`\bgo(\d+)\.(\d+)`)
	memorySkillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	memoryRetiredSkills    = []string{"memory-graph-query"}
)

// MemoryUpdateOptions describes a lark-memory-cli source-install update.
type MemoryUpdateOptions struct {
	SourceDir   string
	AppDir      string
	WrapperPath string
	Branch      string
	Check       bool
	Force       bool
}

// MemoryUpdateResult is the observable result of a source-install update.
type MemoryUpdateResult struct {
	Branch           string
	SourceDir        string
	BinaryPath       string
	WrapperPath      string
	ControlPath      string
	PreviousRevision string
	CurrentRevision  string
	RemoteRevision   string
	Version          string
	Updated          bool
	SkillsSynced     []string
	SkillsArchived   []string
	SkillsWarning    string
}

// UpdateMemorySource updates a lark-memory-cli install created from the source
// checkout used by the README installer.
func (u *Updater) UpdateMemorySource(opts MemoryUpdateOptions) (*MemoryUpdateResult, error) {
	if u.MemoryUpdateOverride != nil {
		return u.MemoryUpdateOverride(opts)
	}
	if opts.Branch == "" {
		opts.Branch = MemoryDefaultBranch
	}
	if opts.SourceDir == "" {
		dir, err := memorySourceDir()
		if err != nil {
			return nil, fmt.Errorf("resolve memory source dir: %w", err)
		}
		opts.SourceDir = dir
	}
	if opts.AppDir == "" {
		return nil, fmt.Errorf("memory app dir is empty")
	}
	if opts.WrapperPath == "" {
		return nil, fmt.Errorf("memory wrapper path is empty")
	}
	opts.SourceDir = filepath.Clean(opts.SourceDir)
	opts.AppDir = filepath.Clean(opts.AppDir)
	opts.WrapperPath = filepath.Clean(opts.WrapperPath)

	if err := requireDir(opts.SourceDir, "memory source dir"); err != nil {
		return nil, err
	}
	if err := requireDir(filepath.Join(opts.SourceDir, ".git"), "memory source git metadata"); err != nil {
		return nil, err
	}
	if err := migrateMemoryOrigin(opts.SourceDir); err != nil {
		return nil, err
	}

	previous, err := gitOutput(opts.SourceDir, memoryUpdateGitTimeout, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	previous = strings.TrimSpace(previous)

	result := &MemoryUpdateResult{
		Branch:           opts.Branch,
		SourceDir:        opts.SourceDir,
		BinaryPath:       filepath.Join(opts.AppDir, "lark-cli"),
		WrapperPath:      opts.WrapperPath,
		PreviousRevision: previous,
		CurrentRevision:  previous,
	}

	if opts.Check {
		remote, err := remoteBranchRevision(opts.SourceDir, opts.Branch)
		if err != nil {
			return nil, err
		}
		result.RemoteRevision = remote
		result.Updated = remote != "" && remote != previous
		return result, nil
	}

	if _, err := gitOutput(opts.SourceDir, memoryUpdateGitTimeout, "fetch", "origin", opts.Branch); err != nil {
		return nil, err
	}
	if _, err := gitOutput(opts.SourceDir, memoryUpdateGitTimeout, "checkout", opts.Branch); err != nil {
		return nil, err
	}
	if _, err := gitOutput(opts.SourceDir, memoryUpdateGitTimeout, "pull", "--ff-only", "origin", opts.Branch); err != nil {
		return nil, err
	}
	if _, err := gitOutput(opts.SourceDir, memoryUpdateGitTimeout, "fetch", "--tags", "--force", "origin"); err != nil {
		return nil, err
	}

	current, err := gitOutput(opts.SourceDir, memoryUpdateGitTimeout, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	result.CurrentRevision = strings.TrimSpace(current)
	result.Updated = opts.Force || result.CurrentRevision != result.PreviousRevision

	version, err := gitOutput(opts.SourceDir, memoryUpdateGitTimeout, "describe", "--tags", "--always", "--dirty")
	if err != nil {
		version = result.CurrentRevision
	}
	result.Version = strings.TrimSpace(version)
	if result.Version == "" {
		result.Version = "dev"
	}

	modulePath := memoryModulePath
	if mod, err := goListModule(opts.SourceDir); err == nil && strings.TrimSpace(mod) != "" {
		modulePath = strings.TrimSpace(mod)
	}

	if err := buildMemoryBinary(opts.SourceDir, opts.AppDir, result.BinaryPath, strings.TrimSpace(modulePath), result.Version); err != nil {
		return nil, err
	}
	controlPath, err := syncMemoryControl(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	result.ControlPath = controlPath
	wrapperPath, err := syncMemoryWrapperPreservingState(opts.AppDir, opts.WrapperPath)
	if err != nil {
		return nil, err
	}
	result.WrapperPath = wrapperPath
	synced, archived, warning := syncMemorySkillsPreservingState(opts.SourceDir)
	result.SkillsSynced = synced
	result.SkillsArchived = archived
	result.SkillsWarning = warning
	return result, nil
}

func migrateMemoryOrigin(sourceDir string) error {
	origin, err := gitOutput(sourceDir, memoryUpdateGitTimeout, "remote", "get-url", "origin")
	if err != nil {
		return fmt.Errorf("read memory source origin: %w", err)
	}
	if !isLegacyMemoryRepoURL(origin) {
		return nil
	}
	if _, err := gitOutput(sourceDir, memoryUpdateGitTimeout, "remote", "set-url", "origin", memoryDefaultRepoURL); err != nil {
		return fmt.Errorf("migrate memory source origin to %q: %w", memoryDefaultRepoURL, err)
	}
	return nil
}

func isLegacyMemoryRepoURL(rawURL string) bool {
	normalized := strings.TrimSuffix(strings.TrimSpace(rawURL), ".git")
	switch normalized {
	case "git@code.byted.org:lark_search/larksuite-cli",
		"ssh://git@code.byted.org/lark_search/larksuite-cli",
		"https://code.byted.org/lark_search/larksuite-cli":
		return true
	default:
		return false
	}
}

func requireDir(path, label string) error {
	info, err := vfs.Stat(path)
	if err != nil {
		return fmt.Errorf("%s %q is not accessible: %w", label, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s %q is not a directory", label, path)
	}
	return nil
}

func remoteBranchRevision(sourceDir, branch string) (string, error) {
	out, err := gitOutput(sourceDir, memoryUpdateGitTimeout, "ls-remote", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("remote branch origin/%s not found", branch)
	}
	return fields[0], nil
}

func gitOutput(dir string, timeout time.Duration, args ...string) (string, error) {
	return commandOutput(dir, timeout, "git", args...)
}

func goListModule(dir string) (string, error) {
	goBin, err := selectGoBinary()
	if err != nil {
		return "", err
	}
	return commandOutputWithEnv(dir, memoryUpdateGitTimeout, envForGoCommand(), goBin, "list", "-m")
}

func buildMemoryBinary(sourceDir, appDir, binaryPath, modulePath, version string) error {
	goBin, err := selectGoBinary()
	if err != nil {
		return err
	}
	if err := vfs.MkdirAll(appDir, 0o755); err != nil {
		return fmt.Errorf("create app dir %q: %w", appDir, err)
	}
	tmp := filepath.Join(appDir, fmt.Sprintf(".lark-cli.new.%d", os.Getpid()))
	_ = vfs.Remove(tmp)
	defer vfs.Remove(tmp)

	buildDate := time.Now().Format("2006-01-02")
	ldflags := fmt.Sprintf("-s -w -X %s/internal/build.Version=%s -X %s/internal/build.Date=%s", modulePath, version, modulePath, buildDate)
	if _, err := commandOutputWithEnv(sourceDir, memoryUpdateBuildTimeout, envForGoCommand(), goBin,
		"build", "-trimpath", "-ldflags", ldflags, "-o", tmp, "."); err != nil {
		return err
	}
	if err := vfs.Rename(tmp, binaryPath); err != nil {
		return fmt.Errorf("replace memory binary %q: %w", binaryPath, err)
	}
	return nil
}

func writeMemoryWrapper(appDir, wrapperPath string) error {
	if err := vfs.MkdirAll(filepath.Dir(wrapperPath), 0o755); err != nil {
		return fmt.Errorf("create wrapper dir %q: %w", filepath.Dir(wrapperPath), err)
	}
	binary := filepath.Join(appDir, "lark-cli")
	content := strings.Join([]string{
		"#!/usr/bin/env bash",
		`export LARKSUITE_CLI_REMOTE_META="${LARKSUITE_CLI_REMOTE_META:-off}"`,
		fmt.Sprintf("exec %q \"$@\"", binary),
		"",
	}, "\n")
	tmp := wrapperPath + fmt.Sprintf(".tmp.%d", os.Getpid())
	_ = vfs.Remove(tmp)
	if err := vfs.WriteFile(tmp, []byte(content), 0o755); err != nil {
		return fmt.Errorf("write wrapper %q: %w", tmp, err)
	}
	if err := vfs.Rename(tmp, wrapperPath); err != nil {
		_ = vfs.Remove(tmp)
		return fmt.Errorf("replace wrapper %q: %w", wrapperPath, err)
	}
	return nil
}

func syncMemoryControl(sourceDir string) (string, error) {
	src := filepath.Join(sourceDir, "scripts", "memoryctl.sh")
	info, err := vfs.Stat(src)
	if err != nil {
		return "", fmt.Errorf("copy memoryctl source %q: %w", src, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("copy memoryctl source %q: not a file", src)
	}
	dst := filepath.Join(sourceDir, "bin", "memoryctl")
	if err := vfs.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("create memoryctl dir %q: %w", filepath.Dir(dst), err)
	}
	data, err := vfs.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("read memoryctl source %q: %w", src, err)
	}
	tmp := dst + fmt.Sprintf(".tmp.%d", os.Getpid())
	_ = vfs.Remove(tmp)
	if err := vfs.WriteFile(tmp, data, 0o755); err != nil {
		return "", fmt.Errorf("write memoryctl %q: %w", tmp, err)
	}
	if err := vfs.Rename(tmp, dst); err != nil {
		_ = vfs.Remove(tmp)
		return "", fmt.Errorf("replace memoryctl %q: %w", dst, err)
	}
	return dst, nil
}

func syncMemoryWrapperPreservingState(appDir, wrapperPath string) (string, error) {
	disabledPath := disabledSiblingPath(wrapperPath)
	state, err := activeDisabledState(wrapperPath, disabledPath)
	if err != nil {
		return "", err
	}
	switch state {
	case "active":
		return wrapperPath, writeMemoryWrapper(appDir, wrapperPath)
	case "disabled":
		return disabledPath, writeMemoryWrapper(appDir, disabledPath)
	case "missing":
		return wrapperPath, writeMemoryWrapper(appDir, wrapperPath)
	case "conflict":
		return "", fmt.Errorf("wrapper exists in both active and disabled locations: %q and %q", wrapperPath, disabledPath)
	default:
		return "", fmt.Errorf("unknown wrapper state %q", state)
	}
}

func syncMemorySkillsPreservingState(sourceDir string) ([]string, []string, string) {
	home, err := vfs.UserHomeDir()
	if err != nil {
		return nil, nil, fmt.Sprintf("resolve home dir: %v", err)
	}
	managedSkills, err := readManagedMemorySkills(sourceDir)
	if err != nil {
		return nil, nil, err.Error()
	}
	root := filepath.Join(home, ".agents", "skills")
	legacyPrimaryState, legacyStateErr := activeDisabledState(
		filepath.Join(home, ".codex", "skills", "lark-memory"),
		filepath.Join(home, ".codex", "skills", ".disabled", "lark-memory"),
	)
	var warnings []string
	if legacyStateErr != nil {
		warnings = append(warnings, legacyStateErr.Error())
		legacyPrimaryState = "missing"
	}
	synced, rootWarnings := syncMemorySkillRootPreservingState(sourceDir, root, managedSkills, legacyPrimaryState)
	warnings = append(warnings, rootWarnings...)
	archived, retiredWarnings := archiveRetiredAgentMemorySkills(home, memoryRetiredSkills)
	warnings = append(warnings, retiredWarnings...)
	duplicateSkills := append(append([]string(nil), managedSkills...), "graph-search")
	duplicateSkills = append(duplicateSkills, memoryRetiredSkills...)
	codexArchived, archiveWarnings := archiveCodexMemorySkillDuplicates(home, duplicateSkills)
	archived = append(archived, codexArchived...)
	warnings = append(warnings, archiveWarnings...)
	return synced, archived, strings.Join(warnings, "; ")
}

func archiveCodexMemorySkillDuplicates(home string, skills []string) ([]string, []string) {
	root := filepath.Join(home, ".codex", "skills")
	archiveRoot := filepath.Join(root, ".disabled", "lark-memory-cli-duplicates")
	return archiveMemorySkillCopies(root, archiveRoot, skills)
}

func archiveRetiredAgentMemorySkills(home string, skills []string) ([]string, []string) {
	root := filepath.Join(home, ".agents", "skills")
	archiveRoot := filepath.Join(root, ".disabled", "lark-memory-cli-retired")
	return archiveMemorySkillCopies(root, archiveRoot, skills)
}

func archiveMemorySkillCopies(root, archiveRoot string, skills []string) ([]string, []string) {
	seen := make(map[string]struct{}, len(skills))
	var archived []string
	var warnings []string
	for _, skill := range skills {
		if _, ok := seen[skill]; ok {
			continue
		}
		seen[skill] = struct{}{}
		active := filepath.Join(root, skill)
		exists, err := pathExists(active)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("check duplicate skill %q: %v", active, err))
			continue
		}
		if !exists {
			continue
		}
		target, err := nextMemorySkillArchivePath(archiveRoot, skill)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		if err := vfs.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			warnings = append(warnings, fmt.Sprintf("create duplicate skill archive %q: %v", filepath.Dir(target), err))
			continue
		}
		if err := vfs.Rename(active, target); err != nil {
			warnings = append(warnings, fmt.Sprintf("archive duplicate skill %q to %q: %v", active, target, err))
			continue
		}
		archived = append(archived, target)
	}
	return archived, warnings
}

func nextMemorySkillArchivePath(archiveRoot, skill string) (string, error) {
	for suffix := 0; suffix < 1000; suffix++ {
		name := skill
		if suffix > 0 {
			name = fmt.Sprintf("%s.%d", skill, suffix)
		}
		candidate := filepath.Join(archiveRoot, name)
		exists, err := pathExists(candidate)
		if err != nil {
			return "", fmt.Errorf("check duplicate skill archive %q: %w", candidate, err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no available duplicate skill archive path under %q for %q", archiveRoot, skill)
}

func readManagedMemorySkills(sourceDir string) ([]string, error) {
	path := filepath.Join(sourceDir, "skills", memoryManagedSkillsFile)
	data, err := vfs.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read managed Memory skills manifest %q: %w", path, err)
	}
	seen := make(map[string]struct{})
	managed := make([]string, 0, 4)
	for lineNumber, raw := range strings.Split(string(data), "\n") {
		name := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if name == "" {
			continue
		}
		if !memorySkillNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid managed Memory skill %q at %s:%d", name, path, lineNumber+1)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("duplicate managed Memory skill %q at %s:%d", name, path, lineNumber+1)
		}
		if info, statErr := vfs.Stat(filepath.Join(sourceDir, "skills", name)); statErr != nil || !info.IsDir() {
			if statErr != nil {
				return nil, fmt.Errorf("managed Memory skill source %q is unavailable: %w", name, statErr)
			}
			return nil, fmt.Errorf("managed Memory skill source %q is not a directory", name)
		}
		seen[name] = struct{}{}
		managed = append(managed, name)
	}
	if len(managed) == 0 {
		return nil, fmt.Errorf("managed Memory skills manifest %q is empty", path)
	}
	if _, ok := seen["lark-memory"]; !ok {
		return nil, fmt.Errorf("managed Memory skills manifest %q must include lark-memory", path)
	}
	return managed, nil
}

func syncMemorySkillRootPreservingState(sourceDir, root string, managedSkills []string, legacyPrimaryState string) ([]string, []string) {
	var synced []string
	var warnings []string
	primaryActive := filepath.Join(root, "lark-memory")
	primaryDisabled := filepath.Join(root, ".disabled", "lark-memory")
	primaryState, err := activeDisabledState(primaryActive, primaryDisabled)
	if err != nil {
		warnings = append(warnings, err.Error())
		primaryState = "missing"
	}
	if primaryState == "missing" && legacyPrimaryState == "disabled" {
		primaryState = "disabled"
	}

	for _, skill := range managedSkills {
		src := filepath.Join(sourceDir, "skills", skill)
		active := filepath.Join(root, skill)
		disabled := filepath.Join(root, ".disabled", skill)
		state, err := activeDisabledState(active, disabled)
		if err != nil {
			warnings = append(warnings, err.Error())
			continue
		}

		target := active
		switch state {
		case "active":
		case "disabled":
			target = disabled
		case "missing":
			// A newly introduced managed skill follows the existing Memory
			// feature state instead of silently re-enabling a disabled install.
			if primaryState == "disabled" {
				target = disabled
			}
		case "conflict":
			warnings = append(warnings, fmt.Sprintf("skill exists in both active and disabled locations: %q and %q", active, disabled))
			continue
		default:
			warnings = append(warnings, fmt.Sprintf("unknown skill state %q for %q", state, active))
			continue
		}

		if err := copyDirAtomic(src, target); err != nil {
			warnings = append(warnings, err.Error())
			continue
		}
		synced = append(synced, target)
	}

	shared := filepath.Join(root, "lark-shared")
	if _, err := vfs.Stat(shared); os.IsNotExist(err) {
		if err := copyDirAtomic(filepath.Join(sourceDir, "skills", "lark-shared"), shared); err != nil {
			warnings = append(warnings, err.Error())
		} else {
			synced = append(synced, shared)
		}
	} else if err != nil {
		warnings = append(warnings, fmt.Sprintf("check %q: %v", shared, err))
	}
	return synced, warnings
}

func activeDisabledState(active, disabled string) (string, error) {
	activeExists, err := pathExists(active)
	if err != nil {
		return "", fmt.Errorf("check active path %q: %w", active, err)
	}
	disabledExists, err := pathExists(disabled)
	if err != nil {
		return "", fmt.Errorf("check disabled path %q: %w", disabled, err)
	}
	switch {
	case activeExists && disabledExists:
		return "conflict", nil
	case activeExists:
		return "active", nil
	case disabledExists:
		return "disabled", nil
	default:
		return "missing", nil
	}
}

func pathExists(path string) (bool, error) {
	if _, err := vfs.Stat(path); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

func disabledSiblingPath(path string) string {
	return filepath.Join(filepath.Dir(path), ".disabled", filepath.Base(path))
}

func copyDirAtomic(src, dst string) error {
	info, err := vfs.Stat(src)
	if err != nil {
		return fmt.Errorf("copy skill source %q: %w", src, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("copy skill source %q: not a directory", src)
	}
	if err := vfs.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("create skill root %q: %w", filepath.Dir(dst), err)
	}
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+fmt.Sprintf(".tmp.%d", os.Getpid()))
	_ = vfs.RemoveAll(tmp)
	if err := copyDirContents(src, tmp); err != nil {
		_ = vfs.RemoveAll(tmp)
		return err
	}
	if err := vfs.RemoveAll(dst); err != nil {
		_ = vfs.RemoveAll(tmp)
		return fmt.Errorf("remove old skill %q: %w", dst, err)
	}
	if err := vfs.Rename(tmp, dst); err != nil {
		_ = vfs.RemoveAll(tmp)
		return fmt.Errorf("replace skill %q: %w", dst, err)
	}
	return nil
}

func copyDirContents(src, dst string) error {
	if err := vfs.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create dir %q: %w", dst, err)
	}
	entries, err := vfs.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read dir %q: %w", src, err)
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", srcPath, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("copy %q: symbolic links are not supported", srcPath)
		}
		if info.IsDir() {
			if err := copyDirContents(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := vfs.ReadFile(srcPath)
		if err != nil {
			return fmt.Errorf("read file %q: %w", srcPath, err)
		}
		if err := vfs.WriteFile(dstPath, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("write file %q: %w", dstPath, err)
		}
	}
	return nil
}

func selectGoBinary() (string, error) {
	return selectGoBinaryFromCandidates(memoryGoBinaryCandidates(strings.TrimSpace(os.Getenv("GO_BIN"))))
}

func memoryGoBinaryCandidates(explicit string) []string {
	candidates := []string{explicit,
		"/opt/homebrew/opt/go/libexec/bin/go",
		"/usr/local/opt/go/libexec/bin/go",
		"/usr/local/go/bin/go",
		"/usr/local/bytesuite-box/pkg/go/1.24.1/bin/go",
	}
	for _, cellar := range []string{
		"/opt/homebrew/Cellar/go",
		"/usr/local/Cellar/go",
	} {
		candidates = append(candidates, memoryGoCellarCandidates(cellar)...)
	}
	candidates = append(candidates, "/opt/homebrew/bin/go", "/usr/local/bin/go")
	if path, err := execLookPath("go"); err == nil {
		candidates = append(candidates, path)
	}
	return candidates
}

func memoryGoCellarCandidates(cellar string) []string {
	entries, err := memoryGoReadDir(cellar)
	if err != nil {
		return nil
	}
	candidates := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidates = append(candidates, filepath.Join(cellar, entry.Name(), "libexec", "bin", "go"))
	}
	return candidates
}

func selectGoBinaryFromCandidates(candidates []string) (string, error) {
	seen := map[string]bool{}
	var failures []string
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		versionOutput, err := commandOutputWithEnv("", 10*time.Second, envForGoCommand(), candidate, "version")
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate, err))
			continue
		}
		if !memoryGoVersionSupported(versionOutput) {
			failures = append(failures, fmt.Sprintf("%s: %s is older than required Go %d.%d", candidate,
				strings.TrimSpace(versionOutput), memoryMinimumGoMajor, memoryMinimumGoMinor))
			continue
		}
		return candidate, nil
	}
	if len(failures) > 0 {
		return "", fmt.Errorf("no usable Go %d.%d+ toolchain found; tried %s; set GO_BIN to an absolute Go binary path",
			memoryMinimumGoMajor, memoryMinimumGoMinor, strings.Join(failures, "; "))
	}
	return "", fmt.Errorf("no usable Go %d.%d+ toolchain found; set GO_BIN to an absolute Go binary path",
		memoryMinimumGoMajor, memoryMinimumGoMinor)
}

func memoryGoVersionSupported(output string) bool {
	match := memoryGoVersionPattern.FindStringSubmatch(output)
	if len(match) != 3 {
		return false
	}
	major, majorErr := strconv.Atoi(match[1])
	minor, minorErr := strconv.Atoi(match[2])
	if majorErr != nil || minorErr != nil {
		return false
	}
	return major > memoryMinimumGoMajor || major == memoryMinimumGoMajor && minor >= memoryMinimumGoMinor
}

func commandOutput(dir string, timeout time.Duration, name string, args ...string) (string, error) {
	return commandOutputWithEnv(dir, timeout, os.Environ(), name, args...)
}

func commandOutputWithEnv(dir string, timeout time.Duration, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%s timed out after %s", commandLine(name, args), timeout)
	}
	if err != nil {
		detail := strings.TrimSpace(stdout.String() + stderr.String())
		if detail != "" {
			return "", fmt.Errorf("%s failed: %w: %s", commandLine(name, args), err, Truncate(detail, 2000))
		}
		return "", fmt.Errorf("%s failed: %w", commandLine(name, args), err)
	}
	return stdout.String(), nil
}

func commandLine(name string, args []string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func envForGoCommand() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "GOROOT=") || strings.HasPrefix(item, "GOTOOLCHAIN=") || strings.HasPrefix(item, "GO111MODULE=") {
			continue
		}
		env = append(env, item)
	}
	env = append(env, "GOTOOLCHAIN=local", "GO111MODULE=on")
	return env
}
