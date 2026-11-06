package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// The runtime sets the machine up in the link folders its plugin loader looks at, with its own safe, and RUNTIME_ENV_SETUP=off
// keeps it from starting.
func TestEnvSetupRunnersAndTheSafe(t *testing.T) {
	data, home, other := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("RUNTIME_SECRETS_KEY_FILE", filepath.Join(t.TempDir(), "safe.key"))
	t.Setenv("RUNTIME_SECRETS_FILES_DIR", t.TempDir())
	d := deps{DataDir: data, Home: home, CLI: &gate.CLIToken{DataDir: data, Headers: []string{"x-cli-token"}}}

	rs := envSetupRunners(d, filepath.Join(other, "machine.json"), "t")
	var dirs []string
	for _, r := range rs {
		dirs = append(dirs, r.LinkDir)
		if r.Home != home {
			t.Errorf("home %q", r.Home)
		}
	}
	if want := []string{data, filepath.Join(data, "hub"), other}; !slices.Equal(dirs, want) {
		t.Fatalf("%v, want %v", dirs, want)
	}
	t.Setenv("RUNTIME_ENV_SETUP", "off")
	if got := envSetupRunners(d, "", "t"); len(got) != 0 {
		t.Fatalf("switched off, yet %d runners", len(got))
	}
	t.Setenv("RUNTIME_ENV_SETUP", "")

	// The safe: an ask opens on the person's page once per name; a value put by the person is revealed to the setup.
	sec := safeSecrets{safeFor(d)}
	if sec.Has("npm-token") {
		t.Fatal("empty safe has a secret")
	}
	if err := sec.Ask("npm-token", "Окружение команды: Сборка"); err != nil {
		t.Fatal(err)
	}
	if err := sec.Ask("npm-token", "Окружение команды: Сборка"); err != nil {
		t.Fatal(err)
	}
	asks := safeFor(d).asks.Pending()
	if len(asks) != 1 || asks[0].Name != "npm-token" || asks[0].Why != "Окружение команды: Сборка" {
		t.Fatalf("asks %+v", asks)
	}
	if _, _, err := safeFor(d).store.Put("npm-token", "value-1234"); err != nil {
		t.Fatal(err)
	}
	vals, err := sec.Reveal([]string{"npm-token"})
	if err != nil || vals["npm-token"] != "value-1234" || !sec.Has("npm-token") {
		t.Fatalf("%v %v", vals, err)
	}
	if _, err := sec.Reveal([]string{"nope"}); secrets.ReasonOf(err) != secrets.ReasonNotFound {
		t.Fatalf("reveal of a missing one: %v", err)
	}
}

// A runner started the way the runtime starts it picks the list up and writes its account, and stops with its context.
func TestEnvSetupRunsFromTheLinkFolder(t *testing.T) {
	data, home := t.TempDir(), t.TempDir()
	t.Setenv("RUNTIME_SECRETS_KEY_FILE", filepath.Join(t.TempDir(), "safe.key"))
	t.Setenv("RUNTIME_SECRETS_FILES_DIR", t.TempDir())
	d := deps{DataDir: data, Home: home, CLI: &gate.CLIToken{DataDir: data, Headers: []string{"x-cli-token"}}}
	items := []hubproto.EnvItem{{ItemID: "s1", Kind: hubproto.EnvKindScript, Name: "Метка", Order: 1,
		Source: json.RawMessage(`{"from":"text","body":"x","run":"echo done > \"$HOME/mark.txt\""}`)}}
	raw, _ := json.Marshal(map[string]any{"rev": 1, "hash": hubproto.EnvHash(items), "items": items})
	if err := os.WriteFile(filepath.Join(data, "environment.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, hubproto.EnvAllowFile), []byte(`{"setup":true,"at":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rs := envSetupRunners(d, "", "t")
	rs[0].Poll = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	g := &runtimeGate{envSetup: rs[:1]}
	g.startEnvSetup(ctx)
	defer cancel()
	deadline := time.Now().Add(10 * time.Second)
	for {
		raw, err := os.ReadFile(filepath.Join(data, hubproto.EnvSetupFile))
		var st hubproto.EnvSetupState
		if err == nil && json.Unmarshal(raw, &st) == nil && st.Hash == hubproto.EnvHash(items) && len(st.Items) == 1 {
			if st.Items[0].Status != hubproto.EnvOK {
				t.Fatalf("%+v", st.Items)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the list was not worked on")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if b, err := os.ReadFile(filepath.Join(home, "mark.txt")); err != nil || string(b) != "done\n" {
		t.Fatalf("the script did not run in the home: %q %v", b, err)
	}
}
