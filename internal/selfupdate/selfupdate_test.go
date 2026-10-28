package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// hub serves a manifest of files and the files; lie makes the manifest's checksum of runtime wrong.
func hub(t *testing.T, files map[string][]byte, lie bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl/manifest.json" {
			m := map[string]any{}
			for k, b := range files {
				s := sum(b)
				if lie && k == "runtime-linux-amd64" {
					s = sum([]byte("other"))
				}
				m[k] = map[string]any{"sha256": s, "size": len(b)}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "dev", "files": m})
			return
		}
		b, ok := files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setup(t *testing.T, srv *httptest.Server) (*Updater, string, *[]string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"harnessly-agent", "harnessly-runtime"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("old "+n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var applied []string
	u := &Updater{HubURL: srv.URL, GOOS: "linux", GOARCH: "amd64",
		Files: []File{
			{Name: "agent", Path: filepath.Join(dir, "harnessly-agent")},
			{Name: "runtime", Path: filepath.Join(dir, "harnessly-runtime")},
		},
		Applied: func(n []string) { applied = n }}
	return u, dir, &applied
}

func TestApplyReplacesWhatDiffers(t *testing.T) {
	srv := hub(t, map[string][]byte{
		"agent-linux-amd64":   []byte("old harnessly-agent"),
		"runtime-linux-amd64": []byte("new runtime"),
	}, false)
	u, dir, applied := setup(t, srv)
	st, err := u.Check(context.Background())
	if err != nil || st.UpToDate || !st.Checked || st.Mode != "auto" {
		t.Fatalf("check: %+v %v", st, err)
	}
	if err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "harnessly-runtime"))
	if string(b) != "new runtime" {
		t.Fatalf("runtime not replaced: %q", b)
	}
	if len(*applied) != 1 || (*applied)[0] != "runtime" {
		t.Fatalf("applied %v", *applied)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "harnessly-runtime")); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", fi.Mode())
	}
	if st := u.Status(); !st.UpToDate || st.Error != "" {
		t.Fatalf("after: %+v", st)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".*"))
	if len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

func TestApplyRefusesAWrongChecksum(t *testing.T) {
	srv := hub(t, map[string][]byte{
		"agent-linux-amd64":   []byte("new agent"),
		"runtime-linux-amd64": []byte("new runtime"),
	}, true)
	u, dir, applied := setup(t, srv)
	if err := u.Apply(context.Background()); err == nil {
		t.Fatal("a wrong checksum was taken")
	}
	for _, n := range []string{"harnessly-agent", "harnessly-runtime"} {
		b, _ := os.ReadFile(filepath.Join(dir, n))
		if string(b) != "old "+n {
			t.Fatalf("%s changed to %q", n, b)
		}
	}
	if *applied != nil {
		t.Fatal("restarted after a failed update")
	}
	if st := u.Status(); st.Error == "" || st.UpToDate {
		t.Fatalf("status %+v", st)
	}
}

func TestApplyAddsAnOptionalFileTheMachineLacks(t *testing.T) {
	srv := hub(t, map[string][]byte{
		"agent-linux-amd64":     []byte("old harnessly-agent"),
		"runtime-linux-amd64":   []byte("old harnessly-runtime"),
		"harnessly-linux-amd64": []byte("cli"),
	}, false)
	u, dir, applied := setup(t, srv)
	u.Files = append(u.Files, File{Name: "harnessly", Path: filepath.Join(dir, "harnessly"), Optional: true})
	if err := u.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "harnessly")); string(b) != "cli" {
		t.Fatalf("cli %q", b)
	}
	if len(*applied) != 1 || (*applied)[0] != "harnessly" {
		t.Fatalf("applied %v", *applied)
	}
}

func TestCommand(t *testing.T) {
	u := &Updater{HubURL: "https://hub.example/", Sudo: true}
	if got := u.Command(); got != "curl -fsSL https://hub.example/install | sudo sh -s -- --update" {
		t.Fatal(got)
	}
}
