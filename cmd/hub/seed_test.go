package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedCommand(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "seed.yaml")
	if err := os.WriteFile(file, []byte("orgs:\n  - slug: alpha\n    name: Альфа\n    admins: [anna@example.com]\n    tenants:\n      - slug: alpha-main\n        name: Альфа\n        owners: [anna@example.com]\n        teams:\n          - slug: general\n            name: Общая\n            members: [anna@example.com]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := env(map[string]string{"HUB_PUBLIC_URL": "http://127.0.0.1:8080", "HUB_DATA_DIR": filepath.Join(dir, "data")})
	seed := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(context.Background(), append([]string{"seed"}, args...), e, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, _, errOut := seed(); code != 2 || !strings.Contains(errOut, "usage: hub seed") {
		t.Errorf("no file: %d %q", code, errOut)
	}
	if code, out, errOut := seed("--file", file, "--dry-run"); code != 0 || !strings.Contains(out, "+ org alpha") || !strings.Contains(out, "nothing written") {
		t.Errorf("dry run: %d %q %q", code, out, errOut)
	}
	if code, out, errOut := seed("--file", file); code != 0 || !strings.Contains(out, "changes written") {
		t.Errorf("run: %d %q %q", code, out, errOut)
	}
	for _, args := range [][]string{{"--file", file}, {"--file", file, "--dry-run"}} {
		if code, out, errOut := seed(args...); code != 0 || strings.TrimSpace(out) != "изменений нет" {
			t.Errorf("%v again: %d %q %q", args, code, out, errOut)
		}
	}
}
