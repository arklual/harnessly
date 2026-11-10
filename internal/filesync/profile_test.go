package filesync

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func settingsOf(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return m
}

// the profile's settings leave the machine without its secrets and its machine-only keys, sealed with the
// room key; what comes down keeps the machine's own secrets and never takes one from the room.
func TestTheProfileCodecKeepsSecretsOnTheMachine(t *testing.T) {
	p := &ProfileCodec{Home: "/home/coder", Workspace: "/home/coder/workspace/proj",
		LocalKeys: append(append([]string(nil), DefaultLocalKeys...), "env.MY_ROOT")}
	if p.Ready() {
		t.Fatal("ready without a key")
	}
	p.SetKey(bytes.Repeat([]byte{7}, RoomKeySize))
	local := []byte(`{"model":"opus","statusLine":{"command":"/home/coder/bin/s"},"env":{"GITLAB_TOKEN":"x","MY_VAR":"glpat-abc",` +
		`"EDITOR":"vim","MY_ROOT":"/home/coder/workspace/proj"},"permissions":{"additionalDirectories":["/home/coder/workspace/proj/a"]}}`)
	c := p.Canonical(".claude/settings.json", local)
	doc := settingsOf(t, c)
	env, _ := doc["env"].(map[string]any)
	if _, ok := env["GITLAB_TOKEN"]; ok || env["MY_VAR"] != nil || env["EDITOR"] != "vim" || env["MY_ROOT"] != nil || doc["statusLine"] != nil {
		t.Fatalf("canonical = %s", c)
	}
	if !strings.Contains(string(c), "@@WORKSPACE@@/a") {
		t.Fatalf("a machine path left: %s", c)
	}
	// Down on another machine: its own token stays, the room's old secret is not taken.
	room := []byte(`{"model":"sonnet","env":{"EDITOR":"vim","OLD_API_KEY":"leaked"}}`)
	other := []byte(`{"env":{"GITLAB_TOKEN":"mine"},"hooks":{"Stop":[]}}`)
	got := settingsOf(t, p.Local(".claude/settings.json", room, other))
	genv, _ := got["env"].(map[string]any)
	if got["model"] != "sonnet" || genv["GITLAB_TOKEN"] != "mine" || genv["OLD_API_KEY"] != nil || got["hooks"] == nil {
		t.Fatalf("local = %v", got)
	}
	fresh := settingsOf(t, p.Local(".claude/settings.json", room, nil))
	if fenv, _ := fresh["env"].(map[string]any); fenv["OLD_API_KEY"] != nil {
		t.Fatalf("a secret came down: %v", fresh)
	}
	// Sealed with the path bound in.
	blob, err := p.Seal(".claude/CLAUDE.md", []byte("rules"))
	if err != nil {
		t.Fatal(err)
	}
	if out, err := p.Open(".claude/CLAUDE.md", blob); err != nil || string(out) != "rules" {
		t.Fatalf("open = %q %v", out, err)
	}
	if _, err := p.Open(".claude/rules/x.md", blob); err == nil {
		t.Fatal("a blob opened as another file")
	}
	if p.Tag([]byte("a")) == (&ProfileCodec{key: bytes.Repeat([]byte{8}, RoomKeySize)}).Tag([]byte("a")) {
		t.Fatal("the tag does not depend on the key")
	}
}
