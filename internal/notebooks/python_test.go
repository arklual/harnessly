package notebooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This is opt-in so a general Go test run does not start kernels or depend on a
// developer's Python installation. A configured but broken interpreter fails.
func TestNotebookRealJupyter(t *testing.T) {
	interpreter := os.Getenv("RUNTIME_NOTEBOOK_TEST_PYTHON")
	if interpreter == "" {
		t.Skip("set RUNTIME_NOTEBOOK_TEST_PYTHON to a Python interpreter with jupyter_client and ipykernel")
	}
	t.Setenv("RUNTIME_NOTEBOOK_PYTHON", interpreter)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("RUNTIME_TEST_TOKEN", "never-inherit")
	dir := t.TempDir()
	// These would run during imports without -I on both interpreters.
	for _, name := range []string{"json.py", "ipykernel_launcher.py"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("raise RuntimeError('cwd module imported during startup')\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := New()
	t.Cleanup(m.Close)
	key := Key{"owner", "tab-a", filepath.Join(dir, "book.ipynb")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	run := func(code string) Result {
		t.Helper()
		out, err := m.Execute(ctx, key, code)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	out := run("x = 40\nprint('hello')\nx + 2")
	if out.Status != "ok" || out.ExecutionCount == nil || *out.ExecutionCount != 1 || len(out.Outputs) != 2 {
		t.Fatalf("first cell: %+v", out)
	}
	out = run("import os\nassert os.getcwd() == " + pythonString(dir) + "\nassert 'RUNTIME_TEST_TOKEN' not in os.environ\nx + 3")
	if out.Status != "ok" || *out.ExecutionCount != 2 || !strings.Contains(string(out.Outputs[0]), "43") {
		t.Fatalf("persistent state/cwd/env: %+v", out)
	}
	out = run("from IPython.display import display, HTML\ndisplay(HTML('<b>ok</b>'))\nprint('problem', file=__import__('sys').stderr)\nraise ValueError('cell error')")
	if out.Status != "error" || len(out.Outputs) != 3 {
		t.Fatalf("rich outputs and exception: %+v", out)
	}
	var rich struct {
		Type string         `json:"output_type"`
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal(out.Outputs[0], &rich) != nil || rich.Type != "display_data" || rich.Data["text/html"] != "<b>ok</b>" {
		t.Fatalf("display: %s", out.Outputs[0])
	}
	// Wait for code to start before signaling, avoiding an interrupt of an idle
	// kernel just before the execute request arrives.
	done := make(chan struct {
		out Result
		err error
	}, 1)
	go func() {
		out, err := m.Execute(ctx, key, "import pathlib, time\npathlib.Path('running').write_text('yes')\ntime.sleep(30)")
		done <- struct {
			out Result
			err error
		}{out, err}
	}()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
wait:
	for {
		if _, err := os.Stat(filepath.Join(dir, "running")); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("kernel did not start interruptible cell")
		case <-tick.C:
			continue wait
		}
	}
	if _, err := m.Interrupt(ctx, key); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.out.Status != "error" {
			t.Fatalf("interrupt: %+v %v", result.out, result.err)
		}
	case <-ctx.Done():
		t.Fatal("interrupt did not stop execution")
	}
	if out, err := m.Restart(ctx, key); err != nil || out.State != "idle" {
		t.Fatalf("restart: %+v %v", out, err)
	}
	out = run("x")
	if out.Status != "error" || *out.ExecutionCount != 1 {
		t.Fatalf("restart retained variables: %+v", out)
	}
	m.mu.Lock()
	p := m.sessions[key].kernel.(*python)
	m.mu.Unlock()
	m.Shutdown(key)
	if p.Alive() || m.Status(key).State != "stopped" {
		t.Fatal("shutdown left helper alive")
	}
}

func pythonString(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestNotebookMissingPythonIsActionable(t *testing.T) {
	t.Setenv("RUNTIME_NOTEBOOK_PYTHON", filepath.Join(t.TempDir(), "missing-python"))
	m := New()
	defer m.Close()
	key := Key{"owner", "tab-a", "/project/book.ipynb"}
	if m.Status(key).State != "stopped" {
		t.Fatal("status probed Python")
	}
	_, err := m.Execute(context.Background(), key, "x")
	if err == nil || !strings.Contains(err.Error(), "pip install jupyter_client ipykernel") || !strings.Contains(err.Error(), "RUNTIME_NOTEBOOK_PYTHON") {
		t.Fatalf("dependency error: %v", err)
	}
	if m.Status(key).State != "stopped" {
		t.Fatal("failed startup kept a session")
	}
}
