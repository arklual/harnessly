package hlcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesignKitConvertExportsAndImportsAndSaysWhatItDidNotCarry(t *testing.T) {
	work := t.TempDir()
	env := map[string]string{"RUNTIME_DESIGN_KITS_DIR": filepath.Join(work, "kits")}
	abs := filepath.Join(work, "kit")
	r := kitRun(t, env, work, "design", "kit", "init", abs, "--id", "acme-ui", "--namespace", "Acme")
	if r.code != 0 {
		t.Fatalf("init: %+v", r)
	}
	// to stdout: the text on stdout, the report on stderr
	r = kitRun(t, env, work, "design", "kit", "convert", "kit", "--to", "dtcg")
	if r.code != 0 || !strings.Contains(r.out, `"$value": "#f6f7f9"`) || !strings.Contains(r.errs, "not carried") {
		t.Fatalf("to dtcg: %+v", r)
	}
	// to a file: the report on stdout
	r = kitRun(t, env, work, "design", "kit", "convert", "kit", "--to", "css", "--out", "out/tokens.css")
	if r.code != 0 || !strings.Contains(r.out, "wrote:") || !strings.Contains(r.out, "not carried") {
		t.Fatalf("to css: %+v", r)
	}
	if raw, err := os.ReadFile(filepath.Join(work, "out", "tokens.css")); err != nil || !strings.Contains(string(raw), "--acme-ui-surface-0: #f6f7f9;") {
		t.Fatalf("css file: %v %s", err, raw)
	}
	// and back in: a DTCG file with a new token, then kit check says the kit is fine
	src := filepath.Join(work, "more.json")
	if err := os.WriteFile(src, []byte(`{"color":{"$type":"color","brand":{"$value":"#ff00ff","$description":"The brand."}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r = kitRun(t, env, work, "design", "kit", "convert", "kit", "--from", "dtcg", "more.json")
	if r.code != 0 || !strings.Contains(r.out, "1 added") || !strings.Contains(r.out, "tokens.css is not rewritten") || !strings.Contains(r.out, "kit check: clean") {
		t.Fatalf("from dtcg: %+v", r)
	}
	r = kitRun(t, env, work, "design", "kit", "check", abs)
	if r.code != 0 {
		t.Fatalf("check after import: %+v", r)
	}
}

func TestDesignKitConvertRefusesWhatItCannotRead(t *testing.T) {
	work := t.TempDir()
	env := map[string]string{"RUNTIME_DESIGN_KITS_DIR": filepath.Join(work, "kits")}
	kitRun(t, env, work, "design", "kit", "init", filepath.Join(work, "kit"), "--id", "acme-ui", "--namespace", "Acme")
	for _, args := range [][]string{
		{"design", "kit", "convert", "kit"},
		{"design", "kit", "convert", "kit", "--to", "sketch"},
		{"design", "kit", "convert", "kit", "--to", "css", "--from", "css", "x"},
		{"design", "kit", "convert", "kit", "--from", "dtcg"},
		{"design", "kit", "convert", "missing", "--to", "css"},
	} {
		if r := kitRun(t, env, work, args...); r.code == 0 {
			t.Errorf("%v should fail: %+v", args, r)
		}
	}
	bad := filepath.Join(work, "empty.json")
	_ = os.WriteFile(bad, []byte(`{"motion":{"fast":{"$type":"duration","$value":"120ms"}}}`), 0o644)
	r := kitRun(t, env, work, "design", "kit", "convert", "kit", "--from", "dtcg", "empty.json")
	if r.code == 0 || !strings.Contains(r.errs, "duration") {
		t.Errorf("a file with nothing the kit can hold names what it found: %+v", r)
	}
}
