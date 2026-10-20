package agentskills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ly-solutions/harnessly/skills"
)

type dirs struct{ data, codex, bin string }

func newDirs(t *testing.T) dirs {
	t.Helper()
	root := t.TempDir()
	return dirs{filepath.Join(root, "data"), filepath.Join(root, "codex"), filepath.Join(root, "bin")}
}

func (d dirs) opts(boards, artifacts bool) Options {
	return Options{DataDir: d.data, CodexHome: d.codex, BinDir: d.bin, Boards: boards, Artifacts: artifacts}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func TestBothServicesInstallEverythingForBothAgents(t *testing.T) {
	d := newDirs(t)
	res, err := Provision(d.opts(true, true))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed() {
		t.Fatal("nothing written")
	}
	for _, name := range []string{"harnessly", "boards", "artifacts"} {
		for _, p := range []string{
			filepath.Join(d.codex, "skills", "harnessly", name, "SKILL.md"),
			filepath.Join(ClaudePluginDir(d.data), "skills", name, "SKILL.md"),
		} {
			if got := read(t, p); !strings.HasPrefix(got, "---\nname: "+name+"\n") {
				t.Errorf("%s does not start with its front matter: %.60q", p, got)
			}
		}
	}
	for _, p := range []string{
		filepath.Join(d.codex, "skills", "harnessly", "boards", "scripts", "boards"),
		filepath.Join(d.bin, "boards"),
		filepath.Join(d.bin, "artifacts"),
		filepath.Join(ToolsDir(d.data), "artifacts", "bin", "artifacts.mjs"),
		filepath.Join(ToolsDir(d.data), "artifacts", "src", "cli.mjs"),
	} {
		if !exists(p) {
			t.Errorf("missing %s", p)
		}
	}
	for _, p := range []string{filepath.Join(d.bin, "boards"), filepath.Join(d.bin, "artifacts"), filepath.Join(d.codex, "skills", "harnessly", "boards", "scripts", "boards")} {
		if st, _ := os.Stat(p); st == nil || st.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s is not executable", p)
		}
	}
	if shim := read(t, filepath.Join(d.bin, "artifacts")); !strings.Contains(shim, "exec node '"+filepath.Join(ToolsDir(d.data), "artifacts", "bin", "artifacts.mjs")+"'") {
		t.Errorf("the artifacts launcher: %s", shim)
	}
	var plugin struct {
		Name, Version string
	}
	if err := json.Unmarshal([]byte(read(t, filepath.Join(ClaudePluginDir(d.data), ".claude-plugin", "plugin.json"))), &plugin); err != nil || plugin.Name != "harnessly" || !strings.HasPrefix(plugin.Version, "0.0.0-") {
		t.Errorf("plugin manifest: %+v %v", plugin, err)
	}
}

func TestOnlyConfiguredServicesGetTheirSkills(t *testing.T) {
	d := newDirs(t)
	if _, err := Provision(d.opts(false, false)); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(d.codex, "skills", "harnessly", "harnessly", "SKILL.md")) {
		t.Error("the workspace skill is always installed")
	}
	for _, p := range []string{
		filepath.Join(d.codex, "skills", "harnessly", "boards"),
		filepath.Join(d.codex, "skills", "harnessly", "artifacts"),
		filepath.Join(ClaudePluginDir(d.data), "skills", "boards"),
		filepath.Join(ClaudePluginDir(d.data), "skills", "artifacts"),
		filepath.Join(d.bin, "boards"),
		filepath.Join(d.bin, "artifacts"),
		filepath.Join(ToolsDir(d.data), "artifacts"),
	} {
		if exists(p) {
			t.Errorf("%s is installed without its service", p)
		}
	}
	if _, err := Provision(d.opts(true, false)); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(d.codex, "skills", "harnessly", "boards", "SKILL.md")) || exists(filepath.Join(d.codex, "skills", "harnessly", "artifacts")) {
		t.Error("boards only")
	}
}

func TestSecondRunWritesNothing(t *testing.T) {
	d := newDirs(t)
	if _, err := Provision(d.opts(true, true)); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, d)
	res, err := Provision(d.opts(true, true))
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed() || len(res.Kept) != 0 {
		t.Errorf("second run: %+v", res)
	}
	if after := snapshot(t, d); strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Errorf("files changed:\n%s\n--\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

// snapshot lists every file with its content hash and modification time.
func snapshot(t *testing.T, d dirs) []string {
	t.Helper()
	var out []string
	for _, root := range []string{d.data, d.codex, d.bin} {
		_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return err
			}
			st, _ := e.Info()
			b, _ := os.ReadFile(p)
			s := sha256.Sum256(b)
			out = append(out, p+" "+hex.EncodeToString(s[:4])+" "+st.ModTime().String())
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func TestTheUsersOwnFilesAreNeverTouched(t *testing.T) {
	d := newDirs(t)
	own := map[string]string{
		filepath.Join(d.codex, "skills", "my-skill", "SKILL.md"):            "mine",
		filepath.Join(d.codex, "config.toml"):                               "model = \"x\"\n",
		filepath.Join(d.codex, "skills", "harnessly", "notes.txt"):          "next to ours",
		filepath.Join(d.bin, "other-tool"):                                  "#!/bin/sh\n",
		filepath.Join(d.bin, "boards"):                                      "#!/bin/sh\necho my own boards\n", // same name as a launcher
		filepath.Join(d.codex, "skills", "harnessly", "boards", "SKILL.md"): "my own boards skill\n",
	}
	for p, c := range own {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Provision(d.opts(true, true))
	if err != nil {
		t.Fatal(err)
	}
	for p, c := range own {
		if got := read(t, p); got != c {
			t.Errorf("%s was changed to %q", p, got)
		}
	}
	kept := strings.Join(res.Kept, "\n")
	for _, p := range []string{filepath.Join(d.bin, "boards"), filepath.Join(d.codex, "skills", "harnessly", "boards", "SKILL.md")} {
		if !strings.Contains(kept, p) {
			t.Errorf("%s should be reported as kept: %v", p, res.Kept)
		}
	}
	// A second run leaves them alone as well.
	if _, err := Provision(d.opts(true, true)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(d.bin, "boards")); got != own[filepath.Join(d.bin, "boards")] {
		t.Error("the user's boards command was replaced on the second run")
	}
}

// fakeSource builds a source of two versions of a tiny skill set.
func fakeSource(version string, extra bool) fstest.MapFS {
	m := fstest.MapFS{
		"MANIFEST.sha256": {Data: []byte("manifest " + version)},
		"upstream/agent-skills/plugins/harnessly/skills/harnessly/SKILL.md": {Data: []byte("---\nname: harnessly\n---\nv" + version)},
		"upstream/agent-skills/plugins/boards/skills/boards/SKILL.md":       {Data: []byte("---\nname: boards\n---\nv" + version)},
		"upstream/agent-skills/plugins/boards/skills/boards/scripts/boards": {Data: []byte("#!/bin/sh\necho v" + version)},
		"upstream/artifacts/skills/artifacts/SKILL.md":                      {Data: []byte("---\nname: artifacts\n---\nv" + version)},
		"upstream/artifacts/bin/artifacts.mjs":                              {Data: []byte("// v" + version)},
		"upstream/artifacts/src/cli.mjs":                                    {Data: []byte("// cli v" + version)},
	}
	if extra {
		m["upstream/agent-skills/plugins/harnessly/skills/harnessly/references/more.md"] = &fstest.MapFile{Data: []byte("more " + version)}
	}
	return m
}

func TestNewVersionReplacesOursAndRemovesWhatWentAway(t *testing.T) {
	d := newDirs(t)
	o := d.opts(true, true)
	o.Source = fakeSource("1", true)
	if _, err := Provision(o); err != nil {
		t.Fatal(err)
	}
	more := filepath.Join(d.codex, "skills", "harnessly", "harnessly", "references", "more.md")
	if read(t, more) != "more 1" {
		t.Fatal("extra file not installed")
	}
	o.Source = fakeSource("2", false)
	res, err := Provision(o)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(d.codex, "skills", "harnessly", "boards", "SKILL.md")); !strings.HasSuffix(got, "v2") {
		t.Errorf("not refreshed: %q", got)
	}
	if got := read(t, filepath.Join(d.bin, "boards")); got != "#!/bin/sh\necho v2" {
		t.Errorf("launcher not refreshed: %q", got)
	}
	if exists(more) || exists(filepath.Dir(more)) {
		t.Error("a file the new version no longer has is still there")
	}
	if len(res.Removed) == 0 {
		t.Error("removal not reported")
	}
	// Turning boards off removes its skill and launcher, not the others.
	o.Boards = false
	if _, err := Provision(o); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(d.codex, "skills", "harnessly", "boards")) || exists(filepath.Join(d.bin, "boards")) {
		t.Error("boards left behind")
	}
	if !exists(filepath.Join(d.codex, "skills", "harnessly", "artifacts", "SKILL.md")) {
		t.Error("artifacts removed with boards")
	}
}

func TestAFileTheUserEditedIsNotOverwrittenByANewVersion(t *testing.T) {
	d := newDirs(t)
	o := d.opts(true, true)
	o.Source = fakeSource("1", false)
	if _, err := Provision(o); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(d.codex, "skills", "harnessly", "boards", "SKILL.md")
	if err := os.WriteFile(skill, []byte("edited by hand"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.Source = fakeSource("2", false)
	res, err := Provision(o)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, skill) != "edited by hand" || !strings.Contains(strings.Join(res.Kept, " "), skill) {
		t.Errorf("edit lost or not reported: %v", res.Kept)
	}
	if got := read(t, filepath.Join(d.codex, "skills", "harnessly", "artifacts", "SKILL.md")); !strings.HasSuffix(got, "v2") {
		t.Error("the other files are still refreshed")
	}
}

func TestHarnesslyLauncherIsLinkedOnlyWhereFree(t *testing.T) {
	d := newDirs(t)
	bin := filepath.Join(t.TempDir(), "harnessly")
	if err := os.WriteFile(bin, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	o := d.opts(false, false)
	o.Harnessly = bin
	if _, err := Provision(o); err != nil {
		t.Fatal(err)
	}
	if got, err := os.Readlink(filepath.Join(d.bin, "harnessly")); err != nil || got != bin {
		t.Errorf("link: %q %v", got, err)
	}
	// A command the user put there stays.
	own := newDirs(t)
	_ = os.MkdirAll(own.bin, 0o755)
	_ = os.WriteFile(filepath.Join(own.bin, "harnessly"), []byte("mine"), 0o755)
	o = own.opts(false, false)
	o.Harnessly = bin
	if _, err := Provision(o); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(own.bin, "harnessly")) != "mine" {
		t.Error("user's harnessly replaced")
	}
}

func TestNoBinDirNoCodexHome(t *testing.T) {
	d := newDirs(t)
	o := d.opts(true, true)
	o.BinDir, o.CodexHome = "", ""
	if _, err := Provision(o); err != nil {
		t.Fatal(err)
	}
	if exists(d.bin) || exists(d.codex) {
		t.Error("wrote where it was not asked to")
	}
	if !exists(filepath.Join(ClaudePluginDir(d.data), "skills", "boards", "SKILL.md")) {
		t.Error("the Claude plugin is still written")
	}
}

func TestEnabled(t *testing.T) {
	for v, want := range map[string]bool{"": true, "on": true, "claude": true, "off": false, "OFF": false, " 0 ": false, "false": false, "no": false, "none": false} {
		if got := Enabled(func(string) string { return v }); got != want {
			t.Errorf("RUNTIME_AGENT_SKILLS=%q: %v, want %v", v, got, want)
		}
	}
}

// The vendored files are exactly what MANIFEST.sha256 says: a hand edit of skills/upstream fails here, offline.
func TestVendoredFilesMatchTheirManifest(t *testing.T) {
	raw, err := fs.ReadFile(skills.Upstream, "MANIFEST.sha256")
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		sumPart, name, ok := strings.Cut(line, "  ")
		if !ok {
			t.Fatalf("bad manifest line %q", line)
		}
		listed[strings.TrimPrefix(name, "./")] = sumPart
	}
	seen := 0
	err = fs.WalkDir(skills.Upstream, "upstream", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(skills.Upstream, p)
		if err != nil {
			return err
		}
		s := sha256.Sum256(data)
		rel := strings.TrimPrefix(p, "upstream/")
		if listed[rel] != hex.EncodeToString(s[:]) {
			t.Errorf("%s differs from MANIFEST.sha256: run skills/sync.sh", p)
		}
		seen++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != len(listed) {
		t.Errorf("the manifest lists %d files, upstream/ has %d", len(listed), seen)
	}
}

// Every source is pinned to a full commit and the files Provision reads are there.
func TestSourcesArePinnedAndComplete(t *testing.T) {
	raw, err := fs.ReadFile(skills.Upstream, "SOURCES")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) < 4 || len(f[2]) != 40 || strings.Trim(f[2], "0123456789abcdef") != "" {
			t.Errorf("SOURCES line %q: want name repository 40-digit-commit paths…", line)
			continue
		}
		names[f[0]] = true
		for _, p := range f[3:] {
			if _, err := fs.Stat(skills.Upstream, "upstream/"+f[0]+"/"+p); err != nil {
				t.Errorf("%s: %s is not vendored", f[0], p)
			}
		}
	}
	if !names["agent-skills"] || !names["artifacts"] {
		t.Errorf("sources: %v", names)
	}
	for _, s := range (Options{Boards: true, Artifacts: true}).skills() {
		if _, err := fs.Stat(skills.Upstream, s.dir+"/SKILL.md"); err != nil {
			t.Errorf("skill %s: %v", s.name, err)
		}
	}
}
