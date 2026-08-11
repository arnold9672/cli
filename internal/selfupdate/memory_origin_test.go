// Copyright (c) 2026 Lark Technologies Pte. Ltd.
// SPDX-License-Identifier: MIT

package selfupdate

import (
	"strings"
	"testing"
)

func TestMigrateMemoryOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "legacy SSH URL",
			origin: "git@code.byted.org:lark_search/larksuite-cli.git",
			want:   memoryDefaultRepoURL,
		},
		{
			name:   "legacy SSH URL without suffix",
			origin: "git@code.byted.org:lark_search/larksuite-cli",
			want:   memoryDefaultRepoURL,
		},
		{
			name:   "legacy HTTPS URL",
			origin: "https://code.byted.org/lark_search/larksuite-cli.git",
			want:   memoryDefaultRepoURL,
		},
		{
			name:   "custom fork remains unchanged",
			origin: "https://github.com/example/cli.git",
			want:   "https://github.com/example/cli.git",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mustRunMemoryGit(t, dir, "init")
			mustRunMemoryGit(t, dir, "remote", "add", "origin", tt.origin)

			if err := migrateMemoryOrigin(dir); err != nil {
				t.Fatalf("migrateMemoryOrigin() error: %v", err)
			}
			got := strings.TrimSpace(mustRunMemoryGit(t, dir, "remote", "get-url", "origin"))
			if got != tt.want {
				t.Fatalf("origin = %q, want %q", got, tt.want)
			}
		})
	}
}

func mustRunMemoryGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitOutput(dir, memoryUpdateGitTimeout, args...)
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return out
}
