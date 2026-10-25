package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func patchConfigRoute(t *testing.T) (*Store, func(body string) (int, string)) {
	t.Helper()
	dir := t.TempDir()
	store := &Store{DataDir: dir, ConfigFile: filepath.Join(dir, "config.json")}
	routes := Routes(Config{Store: store, LayoutKey: "layout", DefaultsKey: "launchDefaults"})
	return store, func(body string) (int, string) {
		r := httptest.NewRequest(http.MethodPatch, "/api/config", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		status, v, _ := routes[0].Compute(r)
		raw, _ := v.(json.RawMessage)
		return status, string(raw)
	}
}

func TestPatchConfigUIVersion(t *testing.T) {
	store, patch := patchConfigRoute(t)
	for _, version := range []string{"next", "legacy"} {
		if status, _ := patch(`{"uiVersion":"` + version + `"}`); status != 200 {
			t.Fatalf("%s: %d", version, status)
		}
		if got, _ := store.ConfigObject().Get("uiVersion"); got != version {
			t.Fatalf("stored %v, want %s", got, version)
		}
	}
	for _, bad := range []string{`"beta"`, `""`, `null`, `1`, `true`} {
		if status, body := patch(`{"uiVersion":` + bad + `}`); status != 400 || body != `{"error":"uiVersion must be one of: legacy, next."}` {
			t.Fatalf("%s: %d %s", bad, status, body)
		}
	}
	// A refused value leaves the saved one as it was.
	if got, _ := store.ConfigObject().Get("uiVersion"); got != "legacy" {
		t.Fatalf("after refusals %v", got)
	}
}
