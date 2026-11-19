package codex

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// tuningHome is a Codex folder with a config.toml, a sign-in and the catalog Codex cached next to its config (the shape
// of models_cache.json); it returns the config.toml's path.
func tuningHome(t *testing.T, config string, auth string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("config.toml", config)
	if auth != "" {
		write("auth.json", auth)
	}
	write("models_cache.json", `{"models":[
	  {"slug":"gpt-6.2-sol","visibility":"list","context_window":500000,"max_context_window":872000},
	  {"slug":"gpt-6-luna","visibility":"list","context_window":272000,"max_context_window":872000},
	  {"slug":"hidden","visibility":"hide","context_window":1000,"max_context_window":2000}]}`)
	return filepath.Join(dir, "config.toml")
}

const chatgptAuth = `{"auth_mode":"chatgpt","tokens":{"id_token":"x"}}`

func TestTunedArgsDefaults(t *testing.T) {
	cfg := tuningHome(t, "", chatgptAuth)
	args := TunedArgs(cfg, "gpt-6.2-sol", []string{"rt", "rt", "conn"}, true)
	want := []string{
		"-c", "model_context_window=872000", "-c", "model_auto_compact_token_limit=697000",
		"-c", "project_doc_max_bytes=65536", "-c", "background_terminal_max_timeout=900000",
		"-c", "mcp_servers.rt.startup_timeout_sec=30", "-c", "mcp_servers.rt.tool_timeout_sec=300",
		"-c", "mcp_servers.conn.startup_timeout_sec=30", "-c", "mcp_servers.conn.tool_timeout_sec=300",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("got  %q\nwant %q", args, want)
	}
}

// A value of the person's own stays theirs: no flag is added for a key their config.toml has.
func TestTunedArgsKeepThePersonsKeys(t *testing.T) {
	cfg := tuningHome(t, `
model_context_window = 300000
project_doc_max_bytes = 1000
[mcp_servers.rt]
tool_timeout_sec = 5
`, chatgptAuth)
	args := strings.Join(TunedArgs(cfg, "gpt-6.2-sol", []string{"rt"}, true), " ")
	for _, key := range []string{"model_context_window", "project_doc_max_bytes", "mcp_servers.rt.tool_timeout_sec"} {
		if strings.Contains(args, key+"=") {
			t.Errorf("%s was set over the person's own: %s", key, args)
		}
	}
	for _, key := range []string{"model_auto_compact_token_limit=", "background_terminal_max_timeout=", "mcp_servers.rt.startup_timeout_sec="} {
		if !strings.Contains(args, key) {
			t.Errorf("%s is missing: %s", key, args)
		}
	}
}

// No model named: the smallest maximum of the visible models; a hidden one does not count.
func TestTunedWindowWithoutModel(t *testing.T) {
	cfg := tuningHome(t, `model = "gpt-6-luna"`, chatgptAuth)
	if w, at := TunedWindow(cfg, ""); w != 872000 || at != 697000 {
		t.Fatalf("model of config.toml: %d %d", w, at)
	}
	cfg = tuningHome(t, "", chatgptAuth)
	if w, _ := TunedWindow(cfg, ""); w != 872000 {
		t.Fatalf("unnamed: %d", w)
	}
	if w, _ := TunedWindow(cfg, "not-in-catalog"); w != 872000 {
		t.Fatalf("unknown model falls to the visible ones: %d", w)
	}
}

// An API key pays per token (past 200k some models cost double): the catalog's window stays. So does a vendor's backend,
// and a catalog that is missing or a config.toml that does not read.
func TestTunedWindowStays(t *testing.T) {
	if w, at := TunedWindow(tuningHome(t, "", `{"auth_mode":"apikey","OPENAI_API_KEY":"k"}`), "gpt-6.2-sol"); w != 0 || at != 0 {
		t.Fatalf("api key: %d %d", w, at)
	}
	if w, _ := TunedWindow(tuningHome(t, "", ""), "gpt-6.2-sol"); w != 0 {
		t.Fatalf("no sign-in: %d", w)
	}
	cfg := tuningHome(t, "", chatgptAuth)
	if args := strings.Join(TunedArgs(cfg, "gpt-6.2-sol", nil, false), " "); strings.Contains(args, "model_context_window") {
		t.Fatalf("backend profile: %s", args)
	}
	if err := os.Remove(filepath.Join(filepath.Dir(cfg), "models_cache.json")); err != nil {
		t.Fatal(err)
	}
	if w, _ := TunedWindow(cfg, "gpt-6.2-sol"); w != 0 {
		t.Fatalf("no catalog: %d", w)
	}
	if err := os.WriteFile(cfg, []byte("= broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := TunedArgs(cfg, "x", []string{"rt"}, true); got != nil {
		t.Fatalf("a config.toml that does not read gets nothing: %q", got)
	}
}
