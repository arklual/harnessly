package webrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o755})
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

type fakeHub struct {
	version string
	archive []byte
	sum     string
	off     bool
}

func (f *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.off {
		http.NotFound(w, r)
		return
	}
	switch r.URL.Path {
	case "/dl/web.json":
		_ = json.NewEncoder(w).Encode(HubPage{Version: f.version, SHA256: f.sum, Size: int64(len(f.archive))})
	case "/dl/web.tar.gz":
		_, _ = w.Write(f.archive)
	default:
		http.NotFound(w, r)
	}
}

func newFakeHub(t *testing.T, version string, files map[string]string) (*fakeHub, string) {
	a := tarball(t, files)
	s := sha256.Sum256(a)
	f := &fakeHub{version: version, archive: a, sum: hex.EncodeToString(s[:])}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL
}

const pageContract = `{"schema":1,"version":"1.2.3","calls":[["api","x"]],"routes":[]}`

func TestHubSyncTakesACompatiblePage(t *testing.T) {
	_, url := newFakeHub(t, "1.2.3", map[string]string{"./index.html": "<p>", "./assets/a.js": "1", "./" + File: pageContract})
	h := Hub{URL: url, Dir: t.TempDir()}
	own := Manifest{Schema: 1, Routes: [][]string{{"api"}}}
	v, changed, err := h.Sync(context.Background(), &own, "")
	if err != nil || !changed || v != "1.2.3" {
		t.Fatalf("Sync = %q %v %v", v, changed, err)
	}
	if got := h.Current(); got != filepath.Join(h.Dir, "1.2.3") {
		t.Fatalf("Current = %q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(h.Current(), "assets", "a.js")); string(b) != "1" {
		t.Fatalf("assets/a.js = %q", b)
	}
	if _, changed, err := h.Sync(context.Background(), &own, h.Current()); err != nil || changed {
		t.Fatalf("again: changed=%v err=%v", changed, err)
	}
}

func TestHubSyncKeepsTheServedPage(t *testing.T) {
	f, url := newFakeHub(t, "1.0.0", map[string]string{"./index.html": "a", "./" + File: pageContract})
	h := Hub{URL: url, Dir: t.TempDir()}
	if _, _, err := h.Sync(context.Background(), nil, ""); err != nil {
		t.Fatal(err)
	}
	serving := h.Current()
	for _, v := range []string{"1.0.1", "1.0.2"} {
		f.version = v
		if _, changed, err := h.Sync(context.Background(), nil, serving); err != nil || !changed {
			t.Fatalf("%s: changed=%v err=%v", v, changed, err)
		}
	}
	if _, err := os.Stat(filepath.Join(serving, "index.html")); err != nil {
		t.Fatalf("the served page was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "1.0.1")); err != nil {
		t.Fatalf("the page taken before was removed: %v", err)
	}
}

func TestHubSyncRefuses(t *testing.T) {
	own := Manifest{Schema: 1, Routes: [][]string{{"other"}}}
	cases := map[string]struct {
		files map[string]string
		spoil func(*fakeHub)
		own   *Manifest
	}{
		"a page newer than the runtime": {files: map[string]string{"./index.html": "a", "./" + File: pageContract}, own: &own},
		"an archive the hub did not name": {
			files: map[string]string{"./index.html": "a", "./" + File: pageContract},
			spoil: func(f *fakeHub) { f.archive = append(f.archive, 0) },
		},
		"a path out of the page":   {files: map[string]string{"../evil": "x", "./index.html": "a"}},
		"no index.html":            {files: map[string]string{"./" + File: pageContract}},
		"a page without contract":  {files: map[string]string{"./index.html": "a"}},
		"a hub that hands nothing": {spoil: func(f *fakeHub) { f.off = true }},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, url := newFakeHub(t, "1.2.3", c.files)
			if c.spoil != nil {
				c.spoil(f)
			}
			h := Hub{URL: url, Dir: t.TempDir()}
			_, changed, err := h.Sync(context.Background(), c.own, "")
			if err == nil || changed {
				t.Fatalf("taken: changed=%v err=%v", changed, err)
			}
			if name == "a hub that hands nothing" && !errors.Is(err, ErrNoHubPage) {
				t.Fatalf("err = %v, want ErrNoHubPage", err)
			}
			if h.Current() != "" {
				t.Fatalf("serves %q", h.Current())
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(h.Dir), "evil")); err == nil {
				t.Fatal("wrote outside the folder")
			}
		})
	}
}
