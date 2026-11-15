package realtime

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWakeOffByDefaultAndSavedAlone(t *testing.T) {
	s := New(Config{Dir: t.TempDir()})
	if w := s.State().Wake; w.On || w.Silence != WakeSilenceDefault {
		t.Fatalf("default wake = %+v", w)
	}
	// Fields this version does not know stay on the disk.
	raw := `{"subscription":true,"head":"face","about":"kept","notes":["x"]}`
	if err := os.WriteFile(s.settingsPath(), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWake(Wake{On: true, Silence: 1}); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(s.settingsPath())
	for _, want := range []string{`"about":"kept"`, `"notes":["x"]`, `"head":"face"`, `"wake":{"on":true,"silence":5}`} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("settings %s lack %s", got, want)
		}
	}
	if st := s.State(); !st.Wake.On || st.Wake.Silence != WakeSilenceMin || !st.Subscription {
		t.Fatalf("state = %+v", st)
	}
}

func TestFullSaveKeepsWake(t *testing.T) {
	s := New(Config{Dir: t.TempDir()})
	_ = s.SaveWake(Wake{On: true, Silence: 30})
	h := s.Handler("/v", func(http.ResponseWriter, *http.Request) bool { return true })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v/settings", strings.NewReader(`{"subscription":true,"talk":"voice"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	var st State
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if !st.Wake.On || st.Wake.Silence != 30 || st.Talk != TalkVoice {
		t.Fatalf("state = %+v", st)
	}
}

func TestWakeModelFetchedCheckedRepacked(t *testing.T) {
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	f, _ := zw.Create("model-x/am/final.mdl")
	_, _ = f.Write([]byte("acoustic"))
	_ = zw.Close()
	sum := sha256.Sum256(zbuf.Bytes())
	hits := 0
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write(zbuf.Bytes())
	}))
	defer src.Close()
	dir := t.TempDir()
	s := New(Config{Dir: dir, WakeModelURL: src.URL, WakeModelSHA256: hex.EncodeToString(sum[:])})
	h := s.Handler("/v", func(http.ResponseWriter, *http.Request) bool { return true })
	for range 2 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v/wake/model", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d %s", rec.Code, rec.Body)
		}
		gz, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		tr := tar.NewReader(gz)
		hd, err := tr.Next()
		if err != nil || hd.Name != "model-x/am/final.mdl" {
			t.Fatalf("tar entry %v %v", hd, err)
		}
		if b, _ := io.ReadAll(tr); string(b) != "acoustic" {
			t.Fatalf("content %q", b)
		}
	}
	if hits != 1 {
		t.Fatalf("fetched %d times", hits)
	}
	if _, err := os.Stat(filepath.Join(dir, "wake-model.zip.part")); !os.IsNotExist(err) {
		t.Fatal("zip left behind")
	}
}

func TestWakeModelBadDigestRefused(t *testing.T) {
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("evil")) }))
	defer src.Close()
	s := New(Config{Dir: t.TempDir(), WakeModelURL: src.URL})
	h := s.Handler("/v", func(http.ResponseWriter, *http.Request) bool { return true })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v/wake/model", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
	if _, err := os.Stat(s.wakeModelPath()); !os.IsNotExist(err) {
		t.Fatal("bad model kept")
	}
}
