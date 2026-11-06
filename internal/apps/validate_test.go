package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validatorsOK = `"agent": {
    "validators": [
      { "id": "lint", "on": "PostToolUse", "match": "Edit", "run": "checks/lint.py" },
      { "id": "done", "on": "Stop", "run": "checks/done.sh", "maxRetries": 5 }
    ],`

func TestValidatorsValid(t *testing.T) {
	raw := strings.Replace(strings.Replace(cashback, `"agent": {`, validatorsOK, 1), `"trust": "sandboxed"`, `"trust": "trusted"`, 1)
	m, ps := ParseManifest([]byte(raw))
	for _, p := range ps {
		if !p.Warning && strings.HasPrefix(p.Path, "agent.validators") {
			t.Errorf("valid validators refused: %v", p)
		}
	}
	vs := ValidatorsOf(m)
	if len(vs) != 2 || vs[0].MaxRetries != ValidatorDefaultRetries || vs[1].MaxRetries != 5 {
		t.Errorf("defaults: %+v", vs)
	}
}

func TestValidatorsInvalid(t *testing.T) {
	cases := []struct{ name, v, path string }{
		{"bad id", `{"id":"Lint","on":"Stop","run":"a.sh"}`, "agent.validators[0].id"},
		{"bad event", `{"id":"a","on":"PreToolUse","run":"a.sh"}`, "agent.validators[0].on"},
		{"no event", `{"id":"a","run":"a.sh"}`, "agent.validators[0].on"},
		{"match on Stop", `{"id":"a","on":"Stop","match":"Edit","run":"a.sh"}`, "agent.validators[0].match"},
		{"bad match", `{"id":"a","on":"PostToolUse","match":"Ed*it","run":"a.sh"}`, "agent.validators[0].match"},
		{"no run", `{"id":"a","on":"Stop"}`, "agent.validators[0].run"},
		{"run leaves the package", `{"id":"a","on":"Stop","run":"../a.sh"}`, "agent.validators[0].run"},
		{"absolute run", `{"id":"a","on":"Stop","run":"/bin/true"}`, "agent.validators[0].run"},
		{"too many retries", `{"id":"a","on":"Stop","run":"a.sh","maxRetries":11}`, "agent.validators[0].maxRetries"},
		{"negative retries", `{"id":"a","on":"Stop","run":"a.sh","maxRetries":-1}`, "agent.validators[0].maxRetries"},
		{"twice", `{"id":"a","on":"Stop","run":"a.sh"},{"id":"a","on":"Stop","run":"a.sh"}`, "agent.validators[1].id"},
	}
	for _, c := range cases {
		raw := strings.Replace(cashback, `"agent": {`, `"agent": { "validators": [`+c.v+`],`, 1)
		_, ps := ParseManifest([]byte(raw))
		if p := problemAt(ps, c.path); p == nil || p.Warning {
			t.Errorf("%s: want an error at %s, got %v", c.name, c.path, ps)
		}
	}
}

func TestValidatorApplies(t *testing.T) {
	v := Validator{On: ValidatorPostToolUse, Match: "Edit"}
	if !v.Applies("PostToolUse", "a.b", "Edit", nil) || v.Applies("PostToolUse", "a.b", "Read", nil) || v.Applies("Stop", "a.b", "Edit", nil) {
		t.Error("match")
	}
	if !(Validator{On: ValidatorPostToolUse}).Applies("PostToolUse", "a.b", "Anything", nil) {
		t.Error("no match is every tool")
	}
}

func TestRunValidator(t *testing.T) {
	dir := t.TempDir()
	script := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script("ok.sh", "cat >/dev/null; exit 0")
	script("fail.sh", "cat >/dev/null; echo fix the tests; exit 2")
	script("broken.sh", "exit 1")
	script("big.sh", "head -c 9000 /dev/zero | tr '\\0' x; exit 2")
	script("stdin.sh", "read l; echo \"$l\"; pwd; exit 2")
	run := func(name string) ValidatorResult {
		return RunValidator(t.Context(), dir, Validator{ID: "v", Run: name}, []byte("{\"a\":1}\n"))
	}
	if r := run("ok.sh"); r.Failed || r.Err != nil {
		t.Errorf("ok: %+v", r)
	}
	if r := run("fail.sh"); !r.Failed || r.Remark != "fix the tests" {
		t.Errorf("fail: %+v", r)
	}
	if r := run("broken.sh"); r.Failed || r.Err == nil {
		t.Errorf("another code is a broken validator: %+v", r)
	}
	if r := run("big.sh"); !r.Failed || len(r.Remark) != ValidatorReasonBytes {
		t.Errorf("remark is cut at 4 KB: %d", len(r.Remark))
	}
	if r := run("stdin.sh"); !strings.Contains(r.Remark, `{"a":1}`) || !strings.Contains(r.Remark, filepath.Base(dir)) {
		t.Errorf("stdin and cwd: %+v", r)
	}
	if r := run("../x.sh"); r.Err == nil {
		t.Errorf("outside the package: %+v", r)
	}
}

func TestGuardSettingsAddsValidatorHooks(t *testing.T) {
	path := t.TempDir() + "/s.json"
	if err := os.WriteFile(path, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"own"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snap := Snapshot{Apps: []SnapshotApp{{ID: "a.b", Dir: "/x", Validators: []Validator{{ID: "v", On: "Stop", Run: "a.sh"}}}}}
	body, err := guardSettings(path, "/bin/h hook app-guard --policy 'p'", snap)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Matcher string
			Hooks   []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	_ = json.Unmarshal(body, &s)
	for _, ev := range []string{"PostToolUse", "Stop"} {
		hs := s.Hooks[ev]
		if got := hs[len(hs)-1].Hooks[0].Command; got != "/bin/h hook app-validate --policy 'p' --event "+ev {
			t.Errorf("%s: %q", ev, got)
		}
	}
	if len(s.Hooks["Stop"]) != 2 {
		t.Errorf("the chat's own hook must stay: %+v", s.Hooks["Stop"])
	}
	// No validators, no hooks.
	body, _ = guardSettings(path, "/bin/h hook app-guard --policy 'p'", Snapshot{Apps: []SnapshotApp{{ID: "a.b"}}})
	if strings.Contains(string(body), "app-validate") {
		t.Error("hooks without validators")
	}
}

func TestValidatorsNeedTrusted(t *testing.T) {
	const trusted = `"trust": "trusted"`
	raw := strings.Replace(cashback, `"agent": {`, validatorsOK, 1)
	for _, trust := range []string{"declarative", "sandboxed"} {
		_, ps := ParseManifest([]byte(strings.Replace(raw, `"trust": "sandboxed"`, `"trust": "`+trust+`"`, 1)))
		if p := problemAt(ps, "agent.validators"); p == nil || p.Warning || !strings.Contains(p.Message, "trust trusted") {
			t.Errorf("%s: want the error, got %v", trust, ps)
		}
	}
	_, ps := ParseManifest([]byte(strings.Replace(raw, `"trust": "sandboxed"`, trusted, 1)))
	if p := problemAt(ps, "agent.validators"); p != nil {
		t.Errorf("trusted: %v", p)
	}
}

func TestLaunchDropsValidatorsOfNotTrusted(t *testing.T) {
	for trust, want := range map[string]bool{TrustDeclarative: false, TrustTrusted: true} {
		reg := &Registry{Root: filepath.Join(t.TempDir(), "apps")}
		pkg := filepath.Join(t.TempDir(), "review")
		if err := Scaffold(pkg, "acme.review", "Ревью", "chat"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := reg.Install(pkg, InstallOptions{Dev: true, HostVersion: "0.17.0"}); err != nil {
			t.Fatal(err)
		}
		// The install is stale: the manifest it recorded has validators the rule would not have let in.
		list, err := reg.read()
		if err != nil || len(list) != 1 {
			t.Fatal(err)
		}
		list[0].Manifest.Trust = trust
		list[0].Manifest.Agent = &Agent{Validators: []Validator{{ID: "v", On: "Stop", Run: "a.sh"}}}
		if err := reg.write(list); err != nil {
			t.Fatal(err)
		}
		settings := filepath.Join(t.TempDir(), "s.json")
		_ = os.WriteFile(settings, []byte(`{}`), 0o600)
		l := &Launcher{Registry: reg, GuardCommand: "/bin/h hook app-guard"}
		out, err := l.Claude([]string{"acme.review"}, settings)
		if err != nil {
			t.Fatalf("%s: %v", trust, err)
		}
		body, _ := os.ReadFile(out.Settings)
		if has := strings.Contains(string(body), "app-validate"); has != want {
			t.Errorf("%s: validate hook registered = %v, want %v", trust, has, want)
		}
	}
}

func TestValidatorEnv(t *testing.T) {
	got := ValidatorEnv([]string{"PATH=/bin", "HOME=/h", "LANG=C", "LC_ALL=C", "TMPDIR=/t", "HARNESSLY_URL=u", "GITLAB_TOKEN=x", "AWS_SECRET=y"})
	if strings.Join(got, " ") != "PATH=/bin HOME=/h LANG=C LC_ALL=C TMPDIR=/t HARNESSLY_URL=u" {
		t.Errorf("%v", got)
	}
}
