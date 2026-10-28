package engine

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoteServerGetsTheRecordingAndTheKey(t *testing.T) {
	key := "sk-test-" + strings.Repeat("0", 16) // built at run time: no provider-shaped literal in the tree
	var got struct{ auth, model, lang, file string }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			http.NotFound(w, r)
			return
		}
		got.auth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(8 << 20); err != nil {
			t.Errorf("multipart: %v", err)
		}
		got.model, got.lang = r.FormValue("model"), r.FormValue("language")
		if f, _, err := r.FormFile("file"); err == nil {
			b, _ := io.ReadAll(f)
			got.file = string(b[:4])
		}
		_, _ = w.Write([]byte(`{"text":"  привет мир "}`))
	}))
	defer srv.Close()

	// A machine whisper cannot run on (no build for it) still dictates through the person's server.
	e := New(Config{Dir: t.TempDir(), GOOS: "windows"})
	if e.Eligible() == nil {
		t.Fatal("the test machine should not run whisper")
	}
	e.SetRemote(&Remote{URL: srv.URL + "/v1/", Model: "whisper-1", Key: key})
	if err := e.Eligible(); err != nil {
		t.Fatalf("with a server: %v", err)
	}
	if !e.Ready() {
		t.Fatal("an engine with a server is ready at once")
	}
	text, err := e.Infer(context.Background(), make([]byte, sampleRate*2), "ru")
	if err != nil || text != "привет мир" {
		t.Fatalf("text %q, err %v", text, err)
	}
	if got.auth != "Bearer "+key || got.model != "whisper-1" || got.lang != "ru" || got.file != "RIFF" {
		t.Fatalf("request: %+v", got)
	}
	if _, err := e.Infer(context.Background(), make([]byte, 100), "auto"); err != nil || got.lang != "" {
		t.Fatalf("auto sends no language: %q %v", got.lang, err)
	}
	if n := e.Needs(); n.Binary || n.Model != nil {
		t.Fatalf("an engine with a server downloads nothing: %+v", n)
	}
}

func TestRemoteFailureNeverShowsTheKey(t *testing.T) {
	const key = "sk-secret-abcdefghijklmnop"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + key + `"}}`))
	}))
	defer srv.Close()
	e := New(Config{Dir: t.TempDir()})
	e.SetRemote(&Remote{URL: srv.URL, Model: "whisper-1", Key: key})
	_, err := e.Infer(context.Background(), make([]byte, 100), "en")
	if err == nil || strings.Contains(err.Error(), key) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("error: %v", err)
	}
}

func TestRemoteCheck(t *testing.T) {
	for _, r := range []Remote{
		{URL: "ftp://x", Model: "m"},
		{URL: "https://", Model: "m"},
		{URL: "https://user:pw@host/v1", Model: "m"},
		{URL: "https://host/v1", Model: ""},
		{URL: "https://host/v1", Model: "a b"},
		{URL: "https://host/v1", Model: "m", Key: "line\nbreak"},
	} {
		if r.Check() == nil {
			t.Errorf("%+v passed", r)
		}
	}
	ok := Remote{URL: "http://127.0.0.1:8000/v1", Model: "Systran/faster-whisper-large-v3"}
	if err := ok.Check(); err != nil {
		t.Fatal(err)
	}
	if ok.Endpoint() != "http://127.0.0.1:8000/v1/audio/transcriptions" {
		t.Fatal(ok.Endpoint())
	}
	full := Remote{URL: "https://api.openai.com/v1/audio/transcriptions/"}
	if full.Endpoint() != "https://api.openai.com/v1/audio/transcriptions" {
		t.Fatal(full.Endpoint())
	}
	if MaskKey("") != "" || MaskKey("short") != "••••" || MaskKey("sk-0123456789abcd") != "••••abcd" {
		t.Fatal("mask")
	}
}

func TestBackToBuiltinAfterRemote(t *testing.T) {
	e := New(Config{Dir: t.TempDir(), Model: "base"})
	e.SetRemote(&Remote{URL: "https://example.invalid/v1", Model: "m"})
	e.SetRemote(nil)
	if e.UsesRemote() || e.Status().State != "idle" {
		t.Fatalf("state %+v", e.Status())
	}
	if n := e.Needs(); n.Model == nil || n.Model.ID != "base" {
		t.Fatalf("needs %+v", n)
	}
}

func TestSameOrigin(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"https://api.openai.com/v1", "https://API.OpenAI.com:443/other", true},
		{"http://h/v1", "http://h:80", true},
		{"https://h/v1", "http://h/v1", false},
		{"https://h/v1", "https://h:8443/v1", false},
		{"https://h/v1", "https://h.evil/v1", false},
		{"https://h/v1", "not a url", false},
	} {
		if got := SameOrigin(c.a, c.b); got != c.same {
			t.Errorf("%s vs %s: %v", c.a, c.b, got)
		}
	}
}
