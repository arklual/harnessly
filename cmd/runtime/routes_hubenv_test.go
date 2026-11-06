package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadHubEnv(t *testing.T) {
	dir := t.TempDir()
	body := `{"rev":3,"hash":"h","items":[
	 {"itemId":"a","kind":"skill","name":"Review","layer":"org","pinned":true,"source":{"secretRefs":["X"]}},
	 {"itemId":"b","kind":"mcp","name":"Tracker","layer":"team","pinned":false},
	 {"itemId":"c","kind":"repo","name":"app","layer":"org"},
	 {"itemId":"d","kind":"mcp","name":"Wiki","layer":"tenant"}],
	 "applied":[{"itemId":"a","status":"ok"},{"itemId":"b","status":"error","message":"нет секрета"}]}`
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readHubEnv([]string{filepath.Join(dir, "none"), dir})
	if len(got) != 3 {
		t.Fatalf("repo must be left out: %+v", got)
	}
	want := []hubEnvItem{
		{ItemID: "a", Kind: "skill", Name: "Review", Layer: "org", Pinned: true, Status: "ok"},
		{ItemID: "b", Kind: "mcp", Name: "Tracker", Layer: "team", Status: "error", Message: "нет секрета"},
		{ItemID: "d", Kind: "mcp", Name: "Wiki", Layer: "tenant", Status: "pending"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestReadHubEnvNoFile(t *testing.T) {
	if got := readHubEnv([]string{t.TempDir()}); got == nil || len(got) != 0 {
		t.Fatalf("want an empty list, got %#v", got)
	}
}
