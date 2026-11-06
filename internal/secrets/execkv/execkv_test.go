package execkv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// TestMain lets the test binary be a provider: with EXECKV_HELPER set it answers one request from a JSON file store.
func TestMain(m *testing.M) {
	if mode := os.Getenv("EXECKV_HELPER"); mode != "" {
		provider(mode)
		return
	}
	os.Exit(m.Run())
}

func provider(mode string) {
	switch mode {
	case "garbage":
		os.Stdout.WriteString("hello")
		return
	case "crash":
		os.Stderr.WriteString("the secret was s3cr3t\n")
		os.Exit(3)
	case "slow":
		time.Sleep(5 * time.Second)
		return
	}
	var req Request
	_ = json.NewDecoder(os.Stdin).Decode(&req)
	file := os.Getenv("EXECKV_STORE")
	store := map[string]map[string]string{}
	if raw, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(raw, &store)
	}
	answer := func(v any) { _ = json.NewEncoder(os.Stdout).Encode(v) }
	if req.Protocol != Protocol {
		answer(map[string]any{"error": map[string]string{"code": "failed", "message": "protocol"}})
		return
	}
	switch req.Op {
	case OpRead:
		d, ok := store[req.Path]
		if !ok {
			answer(map[string]any{"error": map[string]string{"code": "not_found"}})
			return
		}
		answer(map[string]any{"data": d})
		return
	case OpWrite:
		if strings.HasPrefix(req.Path, "locked/") {
			answer(map[string]any{"error": map[string]string{"code": "denied", "message": "policy"}})
			return
		}
		store[req.Path] = req.Data
	case OpDelete:
		delete(store, req.Path)
	case OpList:
		keys := []string{}
		for p := range store {
			if rest, ok := strings.CutPrefix(p, req.Path+"/"); ok && !strings.Contains(rest, "/") {
				keys = append(keys, rest)
			}
		}
		answer(map[string]any{"keys": keys})
		return
	}
	raw, _ := json.Marshal(store)
	_ = os.WriteFile(file, raw, 0o600)
	answer(map[string]any{})
}

func helper(t *testing.T, mode string) *KV {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	k, err := New(self)
	if err != nil {
		t.Fatal(err)
	}
	k.Env = []string{"EXECKV_HELPER=" + mode, "EXECKV_STORE=" + filepath.Join(t.TempDir(), "store.json")}
	k.Timeout = 2 * time.Second
	return k
}

func TestExecKVKeepsTheSafeInAnyStore(t *testing.T) {
	k := helper(t, "store")
	r := secrets.NewRemote(k, "harnessly", t.TempDir())
	r.RefPrefixes = []string{"shared"}
	if _, created, err := r.Put("gh-token", "v1"); err != nil || !created {
		t.Fatalf("put: %v", err)
	}
	views, err := r.List()
	if err != nil || len(views) != 1 || views[0].Name != "gh-token" {
		t.Fatalf("list: %+v %v", views, err)
	}
	values, err := r.Reveal([]string{"gh-token"}, secrets.Use{})
	if err != nil || values["gh-token"] != "v1" {
		t.Fatalf("reveal: %v", err)
	}
	if err := k.Write(context.Background(), "shared/ci", map[string]string{"token": "ci"}); err != nil {
		t.Fatal(err)
	}
	if v, err := r.ReadRef("shared/ci", "token"); err != nil || v != "ci" {
		t.Fatalf("ref: %q %v", v, err)
	}
	if err := r.Remove("gh-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Read(context.Background(), "harnessly/gh-token"); secrets.ReasonOf(err) != secrets.ReasonNotFound {
		t.Fatalf("after remove: %v", err)
	}
	if err := k.Write(context.Background(), "locked/x", map[string]string{"value": "v"}); secrets.ReasonOf(err) != secrets.ReasonDenied {
		t.Fatalf("denied: %v", err)
	}
}

func TestExecKVRefusalsSayNothingOfTheStore(t *testing.T) {
	for mode, want := range map[string]string{"garbage": secrets.ReasonFailed, "crash": secrets.ReasonUnavailable, "slow": secrets.ReasonUnavailable} {
		_, err := helper(t, mode).Read(context.Background(), "harnessly/x")
		if secrets.ReasonOf(err) != want {
			t.Errorf("%s: %v, want %s", mode, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%s: the command's stderr in the error: %v", mode, err)
		}
	}
	if _, err := New(""); err == nil {
		t.Fatal("no command")
	}
	if _, err := New("no-such-provider-command-xyz"); err == nil {
		t.Fatal("a missing command")
	}
}
