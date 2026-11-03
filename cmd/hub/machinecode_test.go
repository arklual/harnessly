package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineCodeCommand(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "seed.yaml")
	if err := os.WriteFile(file, []byte("orgs:\n  - slug: alpha\n    name: Альфа\n    admins: [anna@example.com]\n    tenants:\n      - slug: alpha-main\n        name: Альфа\n        owners: [anna@example.com]\n        members: [boris@example.com]\n        teams:\n          - slug: general\n            name: Общая\n            members: [anna@example.com, boris@example.com]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := env(map[string]string{"HUB_PUBLIC_URL": "http://127.0.0.1:8080", "HUB_DATA_DIR": filepath.Join(dir, "data")})
	call := func(args ...string) (int, string, string) {
		var out, errOut bytes.Buffer
		code := run(context.Background(), args, e, &out, &errOut)
		return code, out.String(), errOut.String()
	}
	if code, out, errOut := call("seed", "--file", file); code != 0 {
		t.Fatalf("seed: %d %q %q", code, out, errOut)
	}
	if code, _, errOut := call("machine-code", "--space", "alpha-main"); code != 2 || !strings.Contains(errOut, "usage: hub machine-code") {
		t.Errorf("no owner: %d %q", code, errOut)
	}
	if code, _, errOut := call("machine-code", "--space", "alpha-main", "--owner", "anna@example.com", "--profile", "vm"); code != 2 {
		t.Errorf("bad profile: %d %q", code, errOut)
	}
	if code, _, errOut := call("machine-code", "--space", "nope", "--owner", "anna@example.com"); code != 1 || !strings.Contains(errOut, "no space") {
		t.Errorf("no space: %d %q", code, errOut)
	}
	if code, _, errOut := call("machine-code", "--space", "alpha-main", "--owner", "boris@example.com"); code != 1 || !strings.Contains(errOut, "not an owner") {
		t.Errorf("member: %d %q", code, errOut)
	}
	if code, _, errOut := call("machine-code", "--space", "alpha-main", "--owner", "anna@example.com", "--profile", "container"); code != 1 || !strings.Contains(errOut, "HUB_CONTAINER_IMAGE") {
		t.Errorf("container without image: %d %q", code, errOut)
	}
	code, out, errOut := call("machine-code", "--space", "alpha-main", "--owner", "Anna@Example.com")
	if code != 0 || !strings.HasPrefix(out, "hx_") || strings.Count(out, "\n") != 1 || !strings.Contains(errOut, "full code for") {
		t.Errorf("owner: %d %q %q", code, out, errOut)
	}
}
