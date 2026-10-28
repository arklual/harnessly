package docsecret

import (
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	// Samples are assembled here so that nothing token-shaped sits in the source.
	rep := strings.Repeat
	cases := []struct {
		name, text string
		line       int
		kind       string
	}{
		{"clean", "# Plan\n\nnothing here, sha 3f2a9c0d1b7e4a5f8c6d\n", 0, ""},
		{"github on line 3", "# a\n\nuse " + "gh" + "p_" + rep("a", 36) + " now\n", 3, "github"},
		{"gitlab", "x\n" + "glp" + "at-" + rep("b", 22), 2, "gitlab"},
		{"private key", "intro\n" + rep("-", 5) + "BEGIN OPENSSH PRIVATE KEY" + rep("-", 5) + "\nabc", 2, "private-key"},
		{"connection string", "db: " + "post" + "gres://" + "app:" + "pw" + "@db:5432/x", 1, "conn-string"},
		{"bearer", "Authorization: " + "Bear" + "er " + rep("Z", 30), 1, "bearer"},
		{"first of two wins", "a\n" + "AK" + "IA" + rep("A", 16) + "\n" + "glp" + "at-" + rep("b", 22), 2, "aws"},
		{"short bearer is prose", "the Bearer scheme is simple", 0, ""},
		{"url without password", "see https://example.com/a@b", 0, ""},
	}
	for _, c := range cases {
		hit, ok := Find(c.text)
		if c.line == 0 {
			if ok {
				t.Errorf("%s: found %+v in clean text", c.name, hit)
			}
			continue
		}
		if !ok || hit.Line != c.line || hit.Kind != c.kind {
			t.Errorf("%s: got %+v %v, want line %d kind %s", c.name, hit, ok, c.line, c.kind)
		}
	}
}
