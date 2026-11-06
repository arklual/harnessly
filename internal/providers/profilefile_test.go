package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/ext"
	"github.com/ly-solutions/harnessly/internal/pricing"
)

func resetExtra(t *testing.T) {
	t.Helper()
	extraMu.Lock()
	extra = nil
	extraMu.Unlock()
	t.Cleanup(func() {
		extraMu.Lock()
		extra = nil
		extraMu.Unlock()
	})
}

const corpFile = `{"profiles":[
 {"id":"glm-byok","label":"GLM · свой ключ","vendor":"glm","api":"anthropic","base_url":"https://api.z.ai/api/anthropic",
  "key_ref":"glm-api-key","env":{"API_TIMEOUT_MS":"3000000"},
  "models":[{"id":"glm-5.3","label":"GLM 5.3","window":1048576,"efforts":["low","high"],"default_effort":"high","price":{"input":0.6,"output":2.2}}]},
 {"id":"corp-llm","label":"Corp LLM","vendor":"gpt","api":"anthropic","base_url":"https://corp-llm-proxy.example.com/anthropic",
  "key_ref":"corp-llm-key","models":[{"id":"corp-large","window":200000,"price":{"input":3,"output":15,"cached_input":0.3}}]}
]}`

func TestNoFileChangesNothing(t *testing.T) {
	resetExtra(t)
	if got, want := len(Presets()), len(presets); got != want {
		t.Fatalf("presets = %d, want %d", got, want)
	}
}

func TestProfilesFileAddsAndOverrides(t *testing.T) {
	resetExtra(t)
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(path, []byte(corpFile), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadProfileFile(path); err != nil {
		t.Fatal(err)
	}
	if got := len(Presets()); got != len(presets)+2 {
		t.Fatalf("presets = %d, want %d", got, len(presets)+2)
	}
	p, ok := Find("corp-llm")
	if !ok || p.Host != "corp-llm-proxy.example.com" || p.KeyRef != "corp-llm-key" || p.Small != "corp-large" {
		t.Fatalf("corp-llm = %+v, %v", p, ok)
	}
	// Overriding by id keeps the place of the built-in preset.
	over := `{"profiles":[{"id":"glm","label":"GLM mine","vendor":"glm","api":"anthropic","base_url":"https://glm.example.com","models":[{"id":"m"}]}]}`
	if err := AddProfiles([]byte(over)); err != nil {
		t.Fatal(err)
	}
	var idx int
	for i, q := range Presets() {
		if q.ID == "glm" {
			idx = i
			if q.Label != "GLM mine" {
				t.Fatalf("label = %q", q.Label)
			}
		}
	}
	if idx != 2 {
		t.Fatalf("glm moved to %d", idx)
	}
	if c, ok := pricing.OpenAICost("corp-large", 1_000_000, 0, 1_000_000); !ok || c != 18 {
		t.Fatalf("cost = %v, %v", c, ok)
	}
}

func TestProfilesFileRefusals(t *testing.T) {
	resetExtra(t)
	base := func(mod string) string {
		return `{"profiles":[{"id":"x","label":"X","vendor":"glm","api":"anthropic","base_url":"https://x.example.com",` + mod + `"models":[{"id":"m"}]}]}`
	}
	for name, doc := range map[string]string{
		"a key in key_ref":     base(`"key_ref":"sk-abcdefghijklmnop",`),
		"unknown field":        base(`"api_key":"sk-abc",`),
		"plain http":           strings.Replace(base(""), "https://x.example.com", "http://x.example.com", 1),
		"credentials in url":   strings.Replace(base(""), "https://x.example.com", "https://u:p@x.example.com", 1),
		"auth header in env":   base(`"env":{"ANTHROPIC_API_KEY":"v"},`),
		"auto vendor":          strings.Replace(base(""), `"vendor":"glm"`, `"vendor":"auto"`, 1),
		"no models":            `{"profiles":[{"id":"x","label":"X","vendor":"glm","api":"anthropic","base_url":"https://x.example.com","models":[]}]}`,
		"codex without keyenv": strings.Replace(base(""), `"api":"anthropic"`, `"api":"openai-responses"`, 1),
	} {
		if err := AddProfiles([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(Presets()) != len(presets) {
		t.Fatalf("a refused file added profiles")
	}
	// One bad profile refuses the whole file: no half-loaded setup.
	mixed := `{"profiles":[{"id":"ok","label":"Ok","vendor":"glm","api":"anthropic","base_url":"https://ok.example.com","models":[{"id":"m"}]},{"id":"Bad"}]}`
	if err := AddProfiles([]byte(mixed)); err == nil || len(Presets()) != len(presets) {
		t.Fatalf("mixed file: %v, %d presets", err, len(Presets()))
	}
}

func TestKeyRefComesFromTheSafeAndFollowsRotation(t *testing.T) {
	resetExtra(t)
	if err := AddProfiles([]byte(corpFile)); err != nil {
		t.Fatal(err)
	}
	value := "corp-secret-value-1234"
	asked := 0
	st := &Store{Dir: t.TempDir(), Secrets: func(name string) (string, error) {
		asked++
		if name != "corp-llm-key" {
			t.Errorf("asked for %q", name)
		}
		return value + "\n", nil
	}}
	key, err := st.Key("corp-llm")
	if err != nil || key != value {
		t.Fatalf("key = %q, %v", key, err)
	}
	raw, _ := os.ReadFile(st.KeyFile("corp-llm"))
	if string(raw) != value {
		t.Fatalf("key file = %q", raw)
	}
	if _, err := st.Key("corp-llm"); err != nil || asked != 1 {
		t.Fatalf("asked %d times within the settle time", asked)
	}
	// A safe that is down leaves the last key in place.
	st.Secrets = func(string) (string, error) { return "", os.ErrNotExist }
	st.refreshed = nil
	if key, err := st.Key("corp-llm"); err != nil || key != value {
		t.Fatalf("key with the safe down = %q, %v", key, err)
	}
	// A profile with no key_ref never asks the safe.
	st.Secrets = func(string) (string, error) { t.Error("asked"); return "", nil }
	if _, err := st.Key("kimi"); err == nil {
		t.Fatal("kimi has a key")
	}
}

func TestAdapterOffersProfiles(t *testing.T) {
	resetExtra(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/describe", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol":"harnessly.model-profiles/v1","name":"corp","version":"1.0.0","capabilities":["profiles"]}`))
	})
	mux.HandleFunc("/v1/profiles", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(corpFile)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c, err := ext.NewClient(ext.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := LoadAdapter(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if _, ok := Find("corp-llm"); !ok {
		t.Fatal("corp-llm not loaded")
	}
	// An adapter of another protocol adds nothing.
	resetExtra(t)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"protocol":"harnessly.machine-provider/v1","name":"x","version":"1","capabilities":[]}`))
	}))
	defer other.Close()
	c2, _ := ext.NewClient(ext.Config{BaseURL: other.URL})
	if err := LoadAdapter(context.Background(), c2); err == nil || len(Presets()) != len(presets) {
		t.Fatalf("wrong protocol: %v", err)
	}
}

func TestShippedExampleLoads(t *testing.T) {
	resetExtra(t)
	if err := LoadProfileFile(filepath.Join("..", "..", "examples", "profiles", "profiles.json")); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"glm-byok", "corp-llm"} {
		if _, ok := Find(id); !ok {
			t.Errorf("%s missing", id)
		}
	}
}
