package env_test

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/env"
)

func TestCleanSourceOfAPack(t *testing.T) {
	for _, src := range []map[string]any{
		{"repoUrl": "https://git.example.org/a/env.git", "branch": "main", "path": "packs/acme"},
		{"from": "repo", "repo": "item-1", "path": "packs/acme"},
	} {
		if _, err := env.CleanSource("pack", raw(src)); err != nil {
			t.Errorf("%v: %v", src, err)
		}
	}
	for name, src := range map[string]map[string]any{
		"no path":      {"repoUrl": "https://git.example.org/a/env.git"},
		"path outside": {"repoUrl": "https://git.example.org/a/env.git", "path": "../x"},
		"http":         {"repoUrl": "http://git.example.org/a/env.git", "path": "p"},
		"script key":   {"repoUrl": "https://git.example.org/a/env.git", "path": "p", "run": "bash x"},
	} {
		if _, err := env.CleanSource("pack", raw(src)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
