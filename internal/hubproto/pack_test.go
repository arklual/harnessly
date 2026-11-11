package hubproto

import (
	"strings"
	"testing"
)

func TestParsePack(t *testing.T) {
	good := `{"schema":1,"id":"acme","name":"Acme","version":"1.2.3","hooks":[{"id":"g","event":"PreToolUse","command":"x"}],"safe":{"secrets":[{"name":"acme-token","why":"w"}],"never":["acme-token"]}}`
	if _, err := ParsePack([]byte(good)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"unknown key": strings.Replace(good, `"id":"acme"`, `"id":"acme","x":1`, 1),
		"schema":      strings.Replace(good, `"schema":1`, `"schema":2`, 1),
		"id":          strings.Replace(good, `"id":"acme"`, `"id":"Acme!"`, 1),
		"version":     strings.Replace(good, `1.2.3`, `1.2`, 1),
		"event":       strings.Replace(good, `PreToolUse`, `Nope`, 1),
		"secret name": strings.Replace(good, `"name":"acme-token"`, `"name":"a b"`, 1),
	}
	for name, raw := range bad {
		if _, err := ParsePack([]byte(raw)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, p := range []string{"", "../x", "/x", "a//b", "a/./b", `a\b`, "."} {
		if PackRelPath(p) == nil {
			t.Errorf("path %q accepted", p)
		}
	}
	if PackRelPath("tools/cli.sh") != nil {
		t.Error("good path refused")
	}
}
