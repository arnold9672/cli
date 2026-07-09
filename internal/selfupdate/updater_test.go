// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"code.byted.org/lark_search/larksuite-cli/internal/vfs"
)

// executableTestFS mocks vfs for tests that still need vfs.Executable.
type executableTestFS struct {
	vfs.OsFs
	exe      string
	resolved string
}

func (f executableTestFS) Executable() (string, error) { return f.exe, nil }
func (f executableTestFS) EvalSymlinks(path string) (string, error) {
	if f.resolved != "" {
		return f.resolved, nil
	}
	return f.OsFs.EvalSymlinks(path)
}

// lookPathMock patches execLookPath within VerifyBinary for controlled testing.
// Do not use t.Parallel() in tests that install this mock — it mutates a package-level var.
type lookPathMock struct {
	oldLookPath func(string) (string, error)
	result      string
	resultErr   error
}

func (m *lookPathMock) install(bin string) {
	m.oldLookPath = execLookPath
	execLookPath = func(name string) (string, error) {
		if name == bin {
			return m.result, m.resultErr
		}
		return m.oldLookPath(name)
	}
}

func (m *lookPathMock) restore() {
	execLookPath = m.oldLookPath
}

func TestResolveExe(t *testing.T) {
	u := New()
	p, err := u.resolveExe()
	if err != nil {
		t.Fatalf("resolveExe() error: %v", err)
	}
	if !filepath.IsAbs(p) {
		t.Errorf("expected absolute path, got: %s", p)
	}
}

func TestPrepareSelfReplace_ReturnsNoError(t *testing.T) {
	u := New()
	restore, err := u.PrepareSelfReplace()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	restore()
}

func TestCleanupStaleFiles_NoPanic(t *testing.T) {
	u := New()
	u.CleanupStaleFiles()
}

func TestDetectInstallMethodMemorySource(t *testing.T) {
	oldFS := vfs.DefaultFS
	t.Cleanup(func() { vfs.DefaultFS = oldFS })
	t.Setenv("LARK_CLI_MEMORY_DIR", "/home/me/src/lark-memory")

	exe := "/home/me/.local/libexec/lark-memory-cli/lark-cli"
	vfs.DefaultFS = executableTestFS{exe: exe, resolved: exe}

	got := New().DetectInstallMethod()
	if got.Method != InstallMemorySource {
		t.Fatalf("Method = %v, want InstallMemorySource", got.Method)
	}
	if got.SourceDir != "/home/me/src/lark-memory" {
		t.Fatalf("SourceDir = %q", got.SourceDir)
	}
	if got.AppDir != "/home/me/.local/libexec/lark-memory-cli" {
		t.Fatalf("AppDir = %q", got.AppDir)
	}
	if got.WrapperPath != "/home/me/.local/bin/lark-memory-cli" {
		t.Fatalf("WrapperPath = %q", got.WrapperPath)
	}
}

func TestSyncMemoryWrapperPreservingStateKeepsDisabledWrapper(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "lark-cli"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write app binary: %v", err)
	}

	wrapper := filepath.Join(dir, "bin", "lark-memory-cli")
	disabled := disabledSiblingPath(wrapper)
	if err := os.MkdirAll(filepath.Dir(disabled), 0o755); err != nil {
		t.Fatalf("mkdir disabled wrapper dir: %v", err)
	}
	if err := os.WriteFile(disabled, []byte("old wrapper"), 0o755); err != nil {
		t.Fatalf("write disabled wrapper: %v", err)
	}

	got, err := syncMemoryWrapperPreservingState(appDir, wrapper)
	if err != nil {
		t.Fatalf("syncMemoryWrapperPreservingState() error: %v", err)
	}
	if got != disabled {
		t.Fatalf("wrapper path = %q, want disabled path %q", got, disabled)
	}
	if _, err := os.Stat(wrapper); !os.IsNotExist(err) {
		t.Fatalf("active wrapper exists after disabled sync: %v", err)
	}
	data, err := os.ReadFile(disabled)
	if err != nil {
		t.Fatalf("read disabled wrapper: %v", err)
	}
	if strings.Contains(string(data), "open.feishu-pre.cn") {
		t.Fatalf("disabled wrapper should not force pre base URL: %s", data)
	}
	if !strings.Contains(string(data), filepath.Join(appDir, "lark-cli")) {
		t.Fatalf("disabled wrapper does not point at app binary: %s", data)
	}
}

func TestSyncMemorySkillsPreservingStateKeepsDisabledSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sourceDir := t.TempDir()
	writeTestSkill(t, sourceDir, "lark-memory", "memory-v2")
	writeTestSkill(t, sourceDir, "lark-shared", "shared-v2")

	agentsDisabled := filepath.Join(home, ".agents", "skills", ".disabled", "lark-memory")
	if err := os.MkdirAll(agentsDisabled, 0o755); err != nil {
		t.Fatalf("mkdir disabled agents skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentsDisabled, "SKILL.md"), []byte("memory-v1"), 0o644); err != nil {
		t.Fatalf("write old disabled agents skill: %v", err)
	}

	codexActive := filepath.Join(home, ".codex", "skills", "lark-memory")
	if err := os.MkdirAll(codexActive, 0o755); err != nil {
		t.Fatalf("mkdir active codex skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexActive, "SKILL.md"), []byte("memory-v1"), 0o644); err != nil {
		t.Fatalf("write old active codex skill: %v", err)
	}

	synced, warning := syncMemorySkillsPreservingState(sourceDir)
	if warning != "" {
		t.Fatalf("syncMemorySkillsPreservingState() warning = %q", warning)
	}
	assertFileContent(t, filepath.Join(agentsDisabled, "SKILL.md"), "memory-v2")
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "lark-memory")); !os.IsNotExist(err) {
		t.Fatalf("agents skill was re-enabled unexpectedly: %v", err)
	}
	assertFileContent(t, filepath.Join(codexActive, "SKILL.md"), "memory-v2")
	assertFileContent(t, filepath.Join(home, ".agents", "skills", "lark-shared", "SKILL.md"), "shared-v2")
	assertFileContent(t, filepath.Join(home, ".codex", "skills", "lark-shared", "SKILL.md"), "shared-v2")

	wantSynced := []string{
		agentsDisabled,
		filepath.Join(home, ".agents", "skills", "lark-shared"),
		codexActive,
		filepath.Join(home, ".codex", "skills", "lark-shared"),
	}
	for _, want := range wantSynced {
		if !containsString(synced, want) {
			t.Fatalf("synced = %#v, missing %q", synced, want)
		}
	}
}

func TestSyncMemoryControlInstallsMemoryctlCopy(t *testing.T) {
	sourceDir := t.TempDir()
	scriptDir := filepath.Join(sourceDir, "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "memoryctl.sh"), []byte("#!/bin/sh\necho ok\n"), 0o644); err != nil {
		t.Fatalf("write memoryctl source: %v", err)
	}

	got, err := syncMemoryControl(sourceDir)
	if err != nil {
		t.Fatalf("syncMemoryControl() error: %v", err)
	}
	want := filepath.Join(sourceDir, "bin", "memoryctl")
	if got != want {
		t.Fatalf("control path = %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("stat installed memoryctl: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("memoryctl mode = %v, want 0755", info.Mode().Perm())
	}
	assertFileContent(t, want, "#!/bin/sh\necho ok\n")
}

func TestVerifyBinaryLookPath(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "lark-cli")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"lark-cli version 2.1.0\"; exit 0; fi\nexit 12\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}

	mock := &lookPathMock{result: bin}
	mock.install("lark-cli")
	t.Cleanup(mock.restore)

	if err := New().VerifyBinary("2.1.0"); err != nil {
		t.Fatalf("VerifyBinary(2.1.0) error = %v, want nil", err)
	}

	if err := New().VerifyBinary("3.0.0"); err == nil {
		t.Fatal("VerifyBinary(mismatched) expected error, got nil")
	}

	// Regression: version must match exactly (not substring / prefix).
	if err := New().VerifyBinary("0.0"); err == nil {
		t.Fatal("VerifyBinary(substring-style mismatch) expected error, got nil")
	}
	if err := New().VerifyBinary("12.1.0"); err == nil {
		t.Fatal("VerifyBinary(prefix-style mismatch) expected error, got nil")
	}
}

func TestVerifyBinaryLookPathNotFound(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	mock := &lookPathMock{result: "", resultErr: fmt.Errorf("not found")}
	mock.install("lark-cli")
	t.Cleanup(mock.restore)

	oldFS := vfs.DefaultFS
	t.Cleanup(func() { vfs.DefaultFS = oldFS })
	// Without this, VerifyBinary would fall back to the real test binary, which
	// is not a lark-cli --version implementation.
	vfs.DefaultFS = executableTestFS{exe: filepath.Join(t.TempDir(), "missing-lark-cli")}

	if err := New().VerifyBinary("2.0.0"); err == nil {
		t.Fatal("VerifyBinary(not-found) expected error, got nil")
	}
}

func TestVerifyBinaryFallbackExecutableWhenNotOnPath(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "lark-cli-abs")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo \"lark-cli version 2.1.0\"; exit 0; fi\nexit 12\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}

	mock := &lookPathMock{result: "", resultErr: fmt.Errorf("not on PATH")}
	mock.install("lark-cli")
	t.Cleanup(mock.restore)

	oldFS := vfs.DefaultFS
	t.Cleanup(func() { vfs.DefaultFS = oldFS })
	vfs.DefaultFS = executableTestFS{exe: bin}

	if err := New().VerifyBinary("2.1.0"); err != nil {
		t.Fatalf("VerifyBinary(fallback executable) error = %v, want nil", err)
	}
}

func TestVerifyBinaryEmptyOutput(t *testing.T) {
	t.Setenv("LARKSUITE_CLI_CONFIG_DIR", t.TempDir())
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell script")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "lark-cli")
	script := "#!/bin/sh\necho\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}

	mock := &lookPathMock{result: bin}
	mock.install("lark-cli")
	t.Cleanup(mock.restore)

	if err := New().VerifyBinary("2.0.0"); err == nil {
		t.Fatal("VerifyBinary(empty output) expected error, got nil")
	}
}

func writeTestSkill(t *testing.T, sourceDir, name, content string) {
	t.Helper()
	dir := filepath.Join(sourceDir, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir test skill %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write test skill %s: %v", name, err)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestSkillsCommandsUseExpectedArgs(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Updater) *NpmResult
		want string
	}{
		{
			name: "list official primary",
			run: func(u *Updater) *NpmResult {
				return u.runSkillsListOfficial("https://open.feishu.cn")
			},
			want: "-y skills add https://open.feishu.cn --list",
		},
		{
			name: "list global",
			run: func(u *Updater) *NpmResult {
				return u.runSkillsListGlobal()
			},
			want: "-y skills ls -g",
		},
		{
			name: "list global json",
			run: func(u *Updater) *NpmResult {
				return u.ListGlobalSkillsJSON()
			},
			want: "-y skills ls -g --json",
		},
		{
			name: "install skill primary",
			run: func(u *Updater) *NpmResult {
				return u.runSkillsInstall("https://open.feishu.cn", []string{"lark-mail"})
			},
			want: "-y skills add https://open.feishu.cn -s lark-mail -g -y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("uses a POSIX shell script")
			}
			dir := t.TempDir()
			script := filepath.Join(dir, "npx")
			logPath := filepath.Join(dir, "npx.log")
			if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+logPath+"\"\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

			result := tt.run(New())
			if result.Err != nil {
				t.Fatalf("command err = %v, want nil", result.Err)
			}
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(raw)) != tt.want {
				t.Fatalf("args = %q, want %q", strings.TrimSpace(string(raw)), tt.want)
			}
		})
	}
}

func TestListOfficialSkillsIndexSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"skills":[{"name":"lark-calendar"}]}`)
	}))
	defer server.Close()

	oldURL := officialSkillsIndexURL
	officialSkillsIndexURL = server.URL
	t.Cleanup(func() { officialSkillsIndexURL = oldURL })

	result := New().ListOfficialSkillsIndex()
	if result.Err != nil {
		t.Fatalf("ListOfficialSkillsIndex() err = %v, want nil", result.Err)
	}
	if got := result.Stdout.String(); !strings.Contains(got, "lark-calendar") {
		t.Fatalf("ListOfficialSkillsIndex() stdout = %q, want skill JSON", got)
	}
}

func TestListOfficialSkillsIndexHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	oldURL := officialSkillsIndexURL
	officialSkillsIndexURL = server.URL
	t.Cleanup(func() { officialSkillsIndexURL = oldURL })

	result := New().ListOfficialSkillsIndex()
	if result.Err == nil || !strings.Contains(result.Err.Error(), "HTTP 404") {
		t.Fatalf("ListOfficialSkillsIndex() err = %v, want HTTP 404", result.Err)
	}
}

func TestListOfficialSkillsIndexBodyTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", skillsIndexMaxBodySize+1))
	}))
	defer server.Close()

	oldURL := officialSkillsIndexURL
	officialSkillsIndexURL = server.URL
	t.Cleanup(func() { officialSkillsIndexURL = oldURL })

	result := New().ListOfficialSkillsIndex()
	if result.Err == nil || !strings.Contains(result.Err.Error(), "exceeds") {
		t.Fatalf("ListOfficialSkillsIndex() err = %v, want exceeds", result.Err)
	}
	if result.Stdout.Len() != 0 {
		t.Fatalf("ListOfficialSkillsIndex() stdout len = %d, want 0", result.Stdout.Len())
	}
}

func TestListOfficialSkillsIndexTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, `{"skills":[{"name":"lark-calendar"}]}`)
	}))
	defer server.Close()

	oldURL := officialSkillsIndexURL
	oldTimeout := skillsIndexFetchTimeout
	officialSkillsIndexURL = server.URL
	skillsIndexFetchTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		officialSkillsIndexURL = oldURL
		skillsIndexFetchTimeout = oldTimeout
	})

	result := New().ListOfficialSkillsIndex()
	var netErr net.Error
	if result.Err == nil || (!errors.Is(result.Err, context.DeadlineExceeded) && !(errors.As(result.Err, &netErr) && netErr.Timeout())) {
		t.Fatalf("ListOfficialSkillsIndex() err = %v, want timeout error", result.Err)
	}
}

func TestListOfficialSkillsIndexRejectsNonHTTPSRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/skills.json", http.StatusFound)
	}))
	defer server.Close()

	oldURL := officialSkillsIndexURL
	officialSkillsIndexURL = server.URL
	t.Cleanup(func() { officialSkillsIndexURL = oldURL })

	result := New().ListOfficialSkillsIndex()
	if result.Err == nil || !strings.Contains(result.Err.Error(), "non-HTTPS") {
		t.Fatalf("ListOfficialSkillsIndex() err = %v, want non-HTTPS redirect", result.Err)
	}
}

func TestListOfficialSkillsIndexUsesOverride(t *testing.T) {
	result := (&Updater{SkillsIndexFetchOverride: func() *NpmResult {
		r := &NpmResult{}
		r.Stdout.WriteString(`{"skills":[{"name":"override-skill"}]}`)
		return r
	}}).ListOfficialSkillsIndex()
	if result.Err != nil {
		t.Fatalf("ListOfficialSkillsIndex() err = %v, want nil", result.Err)
	}
	if !strings.Contains(result.Stdout.String(), "override-skill") {
		t.Fatalf("ListOfficialSkillsIndex() stdout = %q, want override result", result.Stdout.String())
	}
}

func TestListOfficialSkillsFallsBack(t *testing.T) {
	called := []string{}
	updater := &Updater{
		SkillsCommandOverride: func(args ...string) *NpmResult {
			called = append(called, strings.Join(args, " "))
			r := &NpmResult{}
			if strings.Contains(strings.Join(args, " "), "https://open.feishu.cn") {
				r.Err = fmt.Errorf("primary failed")
				return r
			}
			r.Stdout.WriteString("lark-calendar\n")
			return r
		},
	}

	result := updater.ListOfficialSkills()
	if result.Err != nil {
		t.Fatalf("ListOfficialSkills() err = %v, want nil", result.Err)
	}
	if len(called) != 2 {
		t.Fatalf("called %d commands, want 2: %#v", len(called), called)
	}
	if !strings.Contains(called[1], "larksuite/cli --list") {
		t.Fatalf("fallback call = %q, want larksuite/cli --list", called[1])
	}
}
