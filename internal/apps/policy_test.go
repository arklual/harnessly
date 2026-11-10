package apps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecide(t *testing.T) {
	cashback := &Manifest{ID: "acme.cashback", Agent: &Agent{Tools: &ToolPolicy{
		Allow:   []string{"bank-cashback.*", "app.*", "Read"},
		Deny:    []string{"Bash", "WebFetch", "mcp:*"},
		Confirm: []string{"bank-cashback.create"},
	}}}
	review := &Manifest{ID: "acme.mr-review", Agent: &Agent{Tools: &ToolPolicy{
		Deny: []string{"Edit", "Write", "Bash(git push*)"},
	}}}
	bash := func(cmd string) Call { return Call{Tool: "Bash", Input: map[string]any{"command": cmd}} }

	cases := []struct {
		name string
		apps []*Manifest
		call Call
		want Decision
		app  string
	}{
		{"own server allowed over mcp:*", []*Manifest{cashback}, Call{Tool: "mcp__plugin_acme-cashback_bank-cashback__list"}, DecideAllow, "acme.cashback"},
		{"own server by bare name", []*Manifest{cashback}, Call{Tool: "mcp__bank-cashback__list"}, DecideAllow, "acme.cashback"},
		{"exact confirm beats the mask", []*Manifest{cashback}, Call{Tool: "mcp__plugin_acme-cashback_bank-cashback__create"}, DecideConfirm, "acme.cashback"},
		{"other MCP denied", []*Manifest{cashback}, Call{Tool: "mcp__github__create_issue"}, DecideDeny, "acme.cashback"},
		{"host tools", []*Manifest{cashback}, Call{Tool: "mcp__harnessly-app__flow_go"}, DecideAllow, "acme.cashback"},
		{"host tool by its dotted rule", []*Manifest{{ID: "x.y", Agent: &Agent{Tools: &ToolPolicy{Confirm: []string{"app.data.set"}}}}}, Call{Tool: "mcp__harnessly-app__data_set"}, DecideConfirm, "x.y"},
		{"built-in denied", []*Manifest{cashback}, bash("ls"), DecideDeny, "acme.cashback"},
		{"built-in allowed", []*Manifest{cashback}, Call{Tool: "Read"}, DecideAllow, "acme.cashback"},
		{"no rule", []*Manifest{review}, Call{Tool: "Read"}, DecideNone, ""},
		{"bash rule matches", []*Manifest{review}, bash("git push origin main"), DecideDeny, "acme.mr-review"},
		{"bash rule misses", []*Manifest{review}, bash("git status"), DecideNone, ""},
		{"claude's colon form", []*Manifest{{ID: "x.y", Agent: &Agent{Tools: &ToolPolicy{Deny: []string{"Bash(rm:*)"}}}}}, bash("rm -rf /"), DecideDeny, "x.y"},
		{"allow of one app", []*Manifest{cashback, review}, Call{Tool: "Read"}, DecideAllow, "acme.cashback"},
		{"deny of the other app", []*Manifest{cashback, review}, Call{Tool: "Edit"}, DecideDeny, "acme.mr-review"},
		{"no apps", nil, bash("ls"), DecideNone, ""},
		{"push after another command", []*Manifest{review}, bash("git status && git push"), DecideDeny, "acme.mr-review"},
		{"push with a path and a variable", []*Manifest{review}, bash("GIT_TRACE=1 /usr/bin/git push"), DecideDeny, "acme.mr-review"},
		{"push in a subshell", []*Manifest{review}, bash("(cd x; git push)"), DecideDeny, "acme.mr-review"},
		{"a line it cannot read", []*Manifest{review}, bash("eval \"$CMD\""), DecideConfirm, "acme.mr-review"},
		{"a wrapper's flags", []*Manifest{review}, bash("nice -n 10 git push"), DecideConfirm, "acme.mr-review"},
		{"a wrapper's arguments", []*Manifest{review}, bash("timeout 5 git push"), DecideConfirm, "acme.mr-review"},
		{"a wrapper by its path", []*Manifest{review}, bash("/usr/bin/timeout 5 git push"), DecideConfirm, "acme.mr-review"},
		{"a program under a variable's folder", []*Manifest{review}, bash("$D/git push"), DecideDeny, "acme.mr-review"},
		{"a program in a variable", []*Manifest{review}, bash("G=git; $G push"), DecideConfirm, "acme.mr-review"},
		{"a quoted program", []*Manifest{review}, bash(`"git" push`), DecideConfirm, "acme.mr-review"},
		{"command substitution", []*Manifest{review}, bash("echo $(git push)"), DecideDeny, "acme.mr-review"},
		{"another plugin's server", []*Manifest{cashback}, Call{Tool: "mcp__plugin_evil-app_bank-cashback__list"}, DecideDeny, "acme.cashback"},
		{"host tools only unprefixed", []*Manifest{cashback}, Call{Tool: "mcp__plugin_evil-app_harnessly-app__flow_go"}, DecideDeny, "acme.cashback"},
	}
	for _, c := range cases {
		got := Decide(c.apps, c.call)
		if got.Decision != c.want || got.App != c.app {
			t.Errorf("%s: got %+v", c.name, got)
		}
	}
}

func TestGlob(t *testing.T) {
	for _, c := range []struct {
		p, s string
		want bool
	}{
		{"git push*", "git push --force", true},
		{"git push*", "git pull", false},
		{"*secret*", "cat my-secret.txt", true},
		{"a*b*c", "a-x-b-y-c", true},
		{"a*b*c", "a-x-c", false},
		{"exact", "exact", true},
	} {
		if got := glob(c.p, c.s); got != c.want {
			t.Errorf("glob(%q, %q) = %v", c.p, c.s, got)
		}
	}
}

func TestSnapshotKeepsTheAppsOn(t *testing.T) {
	s := Snapshot{Apps: []SnapshotApp{{ID: "a.b", Name: "AB"}}, Protected: []string{"/data/apps", "/home/me/dev-app"}}
	for _, c := range []struct {
		call Call
		want Decision
	}{
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app disable a.b"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "ls; harnessly app remove a.b"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app list"}}, DecideNone},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app docs export ./hl-docs"}}, DecideNone},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app docs backend"}}, DecideNone},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app public list"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app off a.b"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": `harnessly -w ws1 apps "of"f a.b`}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": `"harness"ly ap\p disable a.b`}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "sh -c 'harnessly app off a.b'"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "/usr/bin/setsid /opt/bin/harnessly app off a.b"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app $'\\x6f'ff a.b"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "echo x > /data/apps/packages/a.b/harnessly/app.json"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly --json app rm a.b"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "/usr/local/bin/harnessly app uninstall a.b"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel beta"}}, DecideNone},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel dev ."}}, DecideNone},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app eval ."}}, DecideNone},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel stable"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel=stable"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel beta --team"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --team=x"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --space=x"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --space x"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel stable"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel=prod"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel \"stable\""}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --channel beta --channel stable"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app update --yes"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app update"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app promote 1.3.0 --channel stable"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app rollback --channel stable"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app team install acme.demo"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app share acme.demo --ttl 7d"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app public install --yes acme.demo"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app public publish acme.demo 1.0.0"}}, DecideDeny},
		{Call{Tool: "Bash", Input: map[string]any{"command": "harnessly app publish --public"}}, DecideDeny},
		{Call{Tool: "Edit", Input: map[string]any{"file_path": "/home/me/dev-app/harnessly/app.json"}}, DecideDeny},
		{Call{Tool: "Edit", Input: map[string]any{"file_path": "/home/me/dev-app/../dev-app/x"}}, DecideDeny},
		{Call{Tool: "Edit", Input: map[string]any{"file_path": "/home/me/dev-application/x"}}, DecideNone},
	} {
		if v, _ := s.Decide(c.call); v.Decision != c.want {
			t.Errorf("%+v: %s", c.call, v.Decision)
		}
	}
}

// A skill's own script runs from its package, and the package may be read; nothing writes into it.
func TestSnapshotLetsThePackageBeReadAndRun(t *testing.T) {
	pkg := "/data/apps/packages/a.b/1.0.0"
	s := Snapshot{Apps: []SnapshotApp{{ID: "a.b", Name: "AB"}}, Protected: []string{"/data/apps", pkg}}
	for _, c := range []struct {
		line string
		want Decision
	}{
		{"python3 -I " + pkg + "/skills/x/pulse.py --show HRN-1", DecideNone},
		{"python3 -I " + pkg + "/skills/x/pulse.py HRN-1 > /tmp/pulse.json 2>&1", DecideNone},
		{"harnessly secret run --env GITLAB_TOKEN=gitlab-token -- python3 -I " + pkg + "/pulse.py K || python3 -I " + pkg + "/pulse.py K", DecideNone},
		{"cat " + pkg + "/SKILL.md | head -20; ls " + pkg, DecideNone},
		{"grep -n x " + pkg + "/a.txt", DecideNone},
		{"python3 -c 'open(\"" + pkg + "/x\",\"w\")'", DecideDeny},
		{"python3 /tmp/mine.py " + pkg, DecideDeny},
		{"python3 -I " + pkg + "/pulse.py K > " + pkg + "/out.json", DecideDeny},
		{"echo x>" + pkg + "/f", DecideDeny},
		{"echo x >> " + pkg + "/f", DecideDeny},
		{"cat " + pkg + "/a | tee " + pkg + "/b", DecideDeny},
		{"ls " + pkg + " && rm -rf " + pkg, DecideDeny},
		{"S=" + pkg + "; python3 -I $S/pulse.py K", DecideDeny},
		{"python3 -I " + pkg + "/pulse.py $(id)", DecideDeny},
		{"sed -i s/a/b/ " + pkg + "/x", DecideDeny},
		{"bash " + pkg + "/run.sh", DecideDeny},
		{"nice python3 -I " + pkg + "/pulse.py K", DecideDeny},
		{"harnessly secret run --env A=b -- cp /tmp/x " + pkg + "/y", DecideDeny},
		{"harnessly secret run --env A=b -- harnessly app off a.b", DecideDeny},
		{"cd " + pkg + " && cp /tmp/x y", DecideDeny},
		{"python3 -I " + pkg + "/pulse.py K 2>&1 >&2", DecideNone},
		{"python3 " + pkg + "/pulse.py K", DecideDeny},
		{"node " + pkg + "/tool.mjs a", DecideDeny},
		{"./cat " + pkg + "/a", DecideDeny},
		{pkg + "/bin/cat " + pkg + "/a", DecideDeny},
		{"echo x >&" + pkg + "/f", DecideDeny},
		{"echo x >& " + pkg + "/f", DecideDeny},
		{"echo x &> " + pkg + "/f", DecideDeny},
		{"cat " + pkg + "/a 1<>" + pkg + "/f", DecideDeny},
		{"cat " + pkg + "/a <> " + pkg + "/f", DecideDeny},
		{"PYTHONSTARTUP=/tmp/x.py python3 -I " + pkg + "/pulse.py K", DecideDeny},
		{"export LD_PRELOAD=/tmp/x.so; python3 -I " + pkg + "/pulse.py K", DecideDeny},
		{"python3 -I " + pkg + "/pulse.py K; sleep 60", DecideNone},
		{"echo x >&2\nrm " + pkg + "/f", DecideDeny},
		{"echo x 2>&1;rm " + pkg + "/f", DecideDeny},
		{"echo x >&2\ncat " + pkg + "/f", DecideNone},
	} {
		if v, _ := s.Decide(Call{Tool: "Bash", Input: map[string]any{"command": c.line}}); v.Decision != c.want {
			t.Errorf("%s: %s", c.line, v.Decision)
		}
	}
}

// The agent's file tools and its shell reach the registry by the way it really leads, not by how it is written.
func TestSnapshotProtectsByRealPath(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data")
	registry := filepath.Join(data, "apps")
	if err := os.MkdirAll(filepath.Join(registry, "packages", "a.b"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(base, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	// The chat's own folder holds a link into the registry, and the data folder is also reached through a link.
	if err := os.Symlink(registry, filepath.Join(work, "reg")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(data, alias); err != nil {
		t.Fatal(err)
	}
	s := Snapshot{Protected: []string{alias + "/apps"}}
	edit := func(tool, path string) Call { return Call{Tool: tool, Input: map[string]any{"file_path": path}} }
	for _, c := range []struct {
		name string
		call Call
		want Decision
	}{
		{"edit through a link in the work folder", edit("Edit", filepath.Join(work, "reg", "installed.json")), DecideDeny},
		{"write a new file through the link", edit("Write", filepath.Join(work, "reg", "packages", "a.b", "new.json")), DecideDeny},
		{"edit by the real path", edit("Edit", filepath.Join(registry, "installed.json")), DecideDeny},
		{"edit by the path the launch named", edit("MultiEdit", alias+"/apps/installed.json"), DecideDeny},
		{"edit with .. back into it", edit("Write", filepath.Join(work, "..", "data", "apps", "x")), DecideDeny},
		{"the notebook tool", Call{Tool: "NotebookEdit", Input: map[string]any{"notebook_path": filepath.Join(registry, "n.ipynb")}}, DecideDeny},
		{"edit in the work folder", edit("Edit", filepath.Join(work, "main.go")), DecideNone},
		{"edit the data folder's other files", edit("Edit", filepath.Join(data, "cli-token")), DecideNone},
		{"edit a sibling that shares the prefix", edit("Edit", filepath.Join(data, "apps-notes.md")), DecideNone},
		{"bash by the real path", Call{Tool: "Bash", Input: map[string]any{"command": "echo x > " + registry + "/installed.json"}}, DecideDeny},
		{"bash by the launch's path", Call{Tool: "Bash", Input: map[string]any{"command": "rm -rf " + alias + "/apps/packages"}}, DecideDeny},
	} {
		if v, _ := s.Decide(c.call); v.Decision != c.want {
			t.Errorf("%s: %s, want %s", c.name, v.Decision, c.want)
		}
	}
}
