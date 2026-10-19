package gitread

import "testing"

func TestNodeDirname(t *testing.T) {
	for in, want := range map[string]string{"/a/b": "/a", "/a/b/": "/a", "/a": "/", "/": "/", "//a": "//", "/a/b//c": "/a/b/", "a": ".", "": "."} {
		if got := nodeDirname(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestParseSessionStart(t *testing.T) {
	got := ParseSessionStart("nope\n{\"timestamp\":\"x\"}\nnull\n{\"timestamp\":0}\n{\"timestamp\":1e400}\n  \n{\"timestamp\":1.5}\n")
	if got == nil || *got != 1.5 {
		t.Fatalf("%v", got)
	}
	if got := ParseSessionStart("{\"timestamp\":\"2025-10-09T10:00:00.000Z\"}"); got == nil || *got != 1760004000000 {
		t.Fatalf("%v", got)
	}
	if got := ParseSessionStart("[1]\n\"timestamp\"\n"); got != nil {
		t.Fatalf("%v", *got)
	}
}
