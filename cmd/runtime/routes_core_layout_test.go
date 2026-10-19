package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCLITokenFirstHeaderDecides(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte("secret\n"), 0o600)
	ok := cliTokenFirst(dir, []string{"x-new-token", "x-old-token"})
	for _, c := range []struct {
		headers map[string]string
		want    bool
	}{
		{map[string]string{"x-new-token": "secret"}, true},
		{map[string]string{"x-old-token": "secret"}, true},
		{map[string]string{"x-new-token": "nope", "x-old-token": "secret"}, false},
		{map[string]string{"x-new-token": ""}, false},
		{map[string]string{}, false},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := ok(r); got != c.want {
			t.Errorf("%v: %v", c.headers, got)
		}
	}
	empty := cliTokenFirst(t.TempDir(), []string{"x-new-token"})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("x-new-token", "")
	if empty(r) {
		t.Fatal("no token file lets nobody in")
	}
}
