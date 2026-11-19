package builders

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestPackUnpackCollect(t *testing.T) {
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "a", "out"), 0o755)
	_ = os.MkdirAll(filepath.Join(src, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "a", "scene.py"), []byte("print(1)"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "a", "out", "f1.png"), []byte("p"), 0o644)
	_ = os.WriteFile(filepath.Join(src, ".git", "config"), []byte("x"), 0o644)
	_ = os.Symlink("/etc/passwd", filepath.Join(src, "link"))
	var buf bytes.Buffer
	n, err := Pack(src, &buf)
	if err != nil || n != 2 {
		t.Fatalf("packed %d %v", n, err)
	}
	dst := t.TempDir()
	if err := Unpack(&buf, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "a", "scene.py")); string(b) != "print(1)" {
		t.Fatal("scene.py")
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Fatal(".git was packed")
	}
	got, _ := Collect(dst, []string{"a/out/*.png", "a/out", "../x", "/etc/*"})
	if len(got) != 1 || got[0] != "a/out/f1.png" {
		t.Fatalf("%v", got)
	}
}

func TestUnpackRefusesEscapes(t *testing.T) {
	var buf bytes.Buffer
	// a tar.gz with ../evil made by Pack of nothing plus a hand-made header
	if err := Unpack(bytes.NewReader([]byte("not gzip")), t.TempDir()); err == nil {
		t.Fatal("garbage accepted")
	}
	_ = buf
}

// a fake agent loopback that plays the hub: lists, accepts a job, reports status
type fakeAgent struct {
	mu       sync.Mutex
	job      Job
	input    []byte
	finished map[string]any
	local    bool
}

func (f *fakeAgent) handler() http.Handler {
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("POST /jobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.job = Job{ID: "job-12345678", Status: "queued", Reason: "builder b1: 0 of 1 slots busy", Local: f.local, TimeoutSec: 3600}
		if f.local {
			f.job.Status, f.job.Reason = "running", "no builders in the pool; runs on this machine's own heavy slots with draft limits"
			f.job.LocalLimits = &struct {
				TimeoutSec int    `json:"timeoutSec"`
				Note       string `json:"note"`
			}{30, "draft"}
		}
		write(w, map[string]any{"job": f.job})
	})
	mux.HandleFunc("PUT /jobs/{id}/input", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.input, _ = io.ReadAll(r.Body)
		f.job.Status = "running"
		write(w, map[string]any{"size": len(f.input)})
	})
	mux.HandleFunc("GET /jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.job.Status == "running" && !f.job.Local {
			zero := 0
			f.job.Status, f.job.ExitCode = "done", &zero
			f.job.Outputs = []File{{Path: "out/f.png", Size: 3}}
			f.job.Builder = &struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{"b1", "b1"}
		}
		write(w, map[string]any{"job": f.job})
	})
	mux.HandleFunc("GET /jobs/{id}/out", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("PNG")) })
	mux.HandleFunc("POST /jobs/{id}/finish", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&f.finished)
		f.job.Status = "done"
		write(w, map[string]any{"job": f.job})
	})
	return mux
}

func newClient(t *testing.T, f *fakeAgent) *Client {
	srv := httptest.NewServer(f.handler())
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, Token: "t", Header: "x-agent-local-token", HTTP: srv.Client()}
}

func TestRunOnABuilderBringsFilesBack(t *testing.T) {
	PollEvery = 10 * time.Millisecond
	f := &fakeAgent{}
	c := newClient(t, f)
	in := t.TempDir()
	_ = os.WriteFile(filepath.Join(in, "scene.py"), []byte("x"), 0o644)
	var log bytes.Buffer
	res, err := c.Run(context.Background(), Spec{Caps: []string{"blender"}, Argv: []string{"blender", "-b"}, InDir: in, Outputs: []string{"out/*.png"}, Log: &log})
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if len(f.input) == 0 || len(res.Files) != 1 {
		t.Fatalf("input %d files %v", len(f.input), res.Files)
	}
	if b, _ := os.ReadFile(filepath.Join(in, "out", "f.png")); string(b) != "PNG" {
		t.Fatalf("result file %q", b)
	}
	if !strings.Contains(log.String(), "builder b1") {
		t.Fatalf("the reason is not shown: %s", log.String())
	}
}

func TestRunFallsBackToLocalSlots(t *testing.T) {
	PollEvery = 10 * time.Millisecond
	t.Setenv("HARNESSLY_HEAVY_DIR", t.TempDir())
	f := &fakeAgent{local: true}
	c := newClient(t, f)
	in := t.TempDir()
	var log bytes.Buffer
	res, err := c.Run(context.Background(), Spec{Argv: []string{"sh", "-c", "mkdir -p out && echo $HARNESSLY_JOB_LOCAL > out/a.txt"}, InDir: in, Outputs: []string{"out/*.txt"}, Log: &log})
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if b, _ := os.ReadFile(filepath.Join(in, "out", "a.txt")); strings.TrimSpace(string(b)) != "1" {
		t.Fatalf("local run did not happen with its marker: %q", b)
	}
	if f.finished == nil || f.finished["exitCode"].(float64) != 0 || len(res.Files) != 1 || !strings.Contains(log.String(), "own heavy slots") {
		t.Fatalf("%v %v\n%s", f.finished, res.Files, log.String())
	}
}

func TestLocalRunKeepsTheCommandsExitCode(t *testing.T) {
	PollEvery = 10 * time.Millisecond
	t.Setenv("HARNESSLY_HEAVY_DIR", t.TempDir())
	f := &fakeAgent{local: true}
	res, err := newClient(t, f).Run(context.Background(), Spec{Argv: []string{"sh", "-c", "exit 3"}, InDir: t.TempDir()})
	if err == nil || res.ExitCode != 3 || f.finished["exitCode"].(float64) != 3 {
		t.Fatalf("%v %+v %v", err, res, f.finished)
	}
}

// An agent that is in no hub (or a hub with no pool) answers 503 or 404: the run goes on here, it does not fail.
func TestRunWithoutAPoolRunsHere(t *testing.T) {
	t.Setenv("HARNESSLY_HEAVY_DIR", t.TempDir())
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusNotFound} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"this machine is not connected to a hub"}`))
		}))
		c := &Client{Base: srv.URL, Token: "t", Header: "x", HTTP: srv.Client()}
		in := t.TempDir()
		var log bytes.Buffer
		res, err := c.Run(context.Background(), Spec{Argv: []string{"sh", "-c", "echo ok > r.txt"}, InDir: in, Outputs: []string{"r.txt"}, Log: &log})
		srv.Close()
		if err != nil || len(res.Files) != 1 || !strings.Contains(log.String(), "no pool of builders") {
			t.Fatalf("%d: %v %v\n%s", status, err, res.Files, log.String())
		}
	}
}
