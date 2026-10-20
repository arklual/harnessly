// Package agentskills installs the skills agents use in a harnessly workspace, for both Claude Code and Codex: the
// workspace skill (the harnessly command), the boards skill when a boards server is configured, and the artifacts skill
// when an artifacts catalog is. The skills are the vendored files of skills.Upstream.
//
// Nothing of the person's own is touched. Everything lands in folders named for harnessly:
//
//	<data folder>/agent-skills/claude         a Claude Code plugin, loaded with --plugin-dir (skills are harnessly:<name>)
//	<codex home>/skills/harnessly/<name>      Codex user skills
//	<data folder>/agent-skills/tools          the artifacts command line client
//	<bin folder>/{artifacts,boards}           launchers on the PATH of the agents' shells
//
// Each root keeps a manifest of what was written. A file is rewritten only when it is unchanged since harnessly wrote
// it; one the person made or edited stays, and is reported. A skill that is no longer wanted (its server was removed)
// is deleted from the manifest's files only. Running it again with nothing new writes nothing.
package agentskills

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/skills"
)

// Options says where the skills go and which are wanted.
type Options struct {
	// DataDir is the runtime's data folder; CodexHome Codex's ($CODEX_HOME, else ~/.codex); BinDir the folder of
	// launchers on the agents' PATH ("" installs none: the helpers are then only next to their skills).
	DataDir, CodexHome, BinDir string
	// Harnessly is the harnessly executable to link as <BinDir>/harnessly when nothing is there; "" links none.
	Harnessly string
	// Boards and Artifacts say whether those servers are configured.
	Boards, Artifacts bool
	// Source is the skills' files; nil is skills.Upstream.
	Source fs.FS
}

// Result lists what a run did, as paths.
type Result struct {
	Written, Removed []string
	// Kept are files that were left alone because they are not harnessly's or were edited since.
	Kept []string
}

// Changed says whether the run wrote or removed anything.
func (r Result) Changed() bool { return len(r.Written)+len(r.Removed) > 0 }

// Enabled reads RUNTIME_AGENT_SKILLS: anything but off, 0, false, no or none installs the skills.
func Enabled(getenv func(string) string) bool {
	switch strings.ToLower(strings.TrimSpace(getenv("RUNTIME_AGENT_SKILLS"))) {
	case "off", "0", "false", "no", "none":
		return false
	}
	return true
}

// ClaudePluginDir is the Claude Code plugin folder Provision fills.
func ClaudePluginDir(dataDir string) string { return filepath.Join(dataDir, "agent-skills", "claude") }

// ToolsDir is where the artifacts client lives.
func ToolsDir(dataDir string) string { return filepath.Join(dataDir, "agent-skills", "tools") }

const manifestName = ".harnessly-managed.json"

type file struct {
	data []byte
	mode os.FileMode
}

type skill struct {
	name string
	// dir is the skill's folder in the source.
	dir string
	on  bool
}

func (o Options) skills() []skill {
	return []skill{
		{"harnessly", "upstream/agent-skills/plugins/harnessly/skills/harnessly", true},
		{"boards", "upstream/agent-skills/plugins/boards/skills/boards", o.Boards},
		{"artifacts", "upstream/artifacts/skills/artifacts", o.Artifacts},
	}
}

// Provision writes the skills; the error joins what could not be written, the rest is done.
func Provision(o Options) (Result, error) {
	src := o.Source
	if src == nil {
		src = skills.Upstream
	}
	var res Result
	var errs []error

	version, err := sourceVersion(src)
	if err != nil {
		return res, err
	}

	claude := map[string]file{}
	codex := map[string]file{}
	for _, s := range o.skills() {
		if !s.on {
			continue
		}
		files, err := readTree(src, s.dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for rel, f := range files {
			claude[path.Join("skills", s.name, rel)] = f
			codex[path.Join(s.name, rel)] = f
		}
	}
	claude[".claude-plugin/plugin.json"] = file{data: pluginJSON(version), mode: 0o644}

	sync := func(root, manifest string, want map[string]file) {
		r, err := syncTree(root, manifest, want)
		res.Written = append(res.Written, r.Written...)
		res.Removed = append(res.Removed, r.Removed...)
		res.Kept = append(res.Kept, r.Kept...)
		if err != nil {
			errs = append(errs, err)
		}
	}
	sync(ClaudePluginDir(o.DataDir), filepath.Join(ClaudePluginDir(o.DataDir), manifestName), claude)
	if o.CodexHome != "" {
		root := filepath.Join(o.CodexHome, "skills", "harnessly")
		sync(root, filepath.Join(root, manifestName), codex)
	}

	// The command line tools of the skills.
	tools := map[string]file{}
	if o.Artifacts {
		for _, rel := range []string{"bin/artifacts.mjs", "src/cli.mjs"} {
			data, err := fs.ReadFile(src, "upstream/artifacts/"+rel)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			tools["artifacts/"+rel] = file{data: data, mode: 0o644}
		}
	}
	sync(ToolsDir(o.DataDir), filepath.Join(ToolsDir(o.DataDir), manifestName), tools)

	if o.BinDir != "" {
		bin := map[string]file{}
		if o.Artifacts {
			shim := fmt.Sprintf("#!/bin/sh\n# Installed by harnessly; the client of the artifacts catalog (needs Node.js).\nexec node %s \"$@\"\n",
				shellQuote(filepath.Join(ToolsDir(o.DataDir), "artifacts", "bin", "artifacts.mjs")))
			bin["artifacts"] = file{data: []byte(shim), mode: 0o755}
		}
		if o.Boards {
			data, err := fs.ReadFile(src, "upstream/agent-skills/plugins/boards/skills/boards/scripts/boards")
			if err != nil {
				errs = append(errs, err)
			} else {
				bin["boards"] = file{data: data, mode: 0o755}
			}
		}
		sync(o.BinDir, filepath.Join(o.DataDir, "agent-skills", "bin-managed.json"), bin)
		if o.Harnessly != "" {
			linkHarnessly(o.BinDir, o.Harnessly)
		}
	}
	return res, errors.Join(errs...)
}

// linkHarnessly makes <bin>/harnessly point at the executable when nothing is there yet.
func linkHarnessly(bin, target string) {
	link := filepath.Join(bin, "harnessly")
	if _, err := os.Lstat(link); err == nil {
		return
	}
	if _, err := os.Stat(target); err != nil {
		return
	}
	if os.MkdirAll(bin, 0o755) == nil {
		_ = os.Symlink(target, link)
	}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// sourceVersion identifies the vendored files: the start of the hash of their manifest.
func sourceVersion(src fs.FS) (string, error) {
	raw, err := fs.ReadFile(src, "MANIFEST.sha256")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12], nil
}

func pluginJSON(version string) []byte {
	b, _ := json.MarshalIndent(map[string]any{
		"name":        "harnessly",
		"version":     "0.0.0-" + version,
		"description": "Skills for agents running in a harnessly workspace: the harnessly command, boards and artifacts.",
		"homepage":    "https://github.com/ly-solutions/harnessly",
		"license":     "Apache-2.0",
		"author":      map[string]string{"name": "Ly Solutions", "url": "https://github.com/ly-solutions"},
	}, "", "  ")
	return append(b, '\n')
}

// readTree reads every file under dir of src; relative paths in the result use "/".
func readTree(src fs.FS, dir string) (map[string]file, error) {
	out := map[string]file{}
	err := fs.WalkDir(src, dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, dir+"/")
		mode := os.FileMode(0o644)
		if strings.HasPrefix(rel, "scripts/") {
			mode = 0o755
		}
		out[rel] = file{data: data, mode: mode}
		return nil
	})
	return out, err
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// syncTree makes root hold want, as far as the manifest says the files are harnessly's.
func syncTree(root, manifestPath string, want map[string]file) (Result, error) {
	var res Result
	var errs []error
	old := map[string]string{}
	if raw, err := os.ReadFile(manifestPath); err == nil {
		var m struct {
			Files map[string]string `json:"files"`
		}
		if json.Unmarshal(raw, &m) == nil && m.Files != nil {
			old = m.Files
		}
	}
	kept := map[string]string{}

	rels := make([]string, 0, len(want))
	for rel := range want {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		f := want[rel]
		target := filepath.Join(root, filepath.FromSlash(rel))
		h := sum(f.data)
		cur, err := os.ReadFile(target)
		switch {
		case err == nil && sum(cur) == h:
			if st, err := os.Stat(target); err == nil && st.Mode().Perm() != f.mode {
				_ = os.Chmod(target, f.mode)
			}
		case err == nil && old[rel] != "" && sum(cur) == old[rel]:
			// Written by harnessly and not touched since: the new version replaces it.
			if err := writeFile(target, f); err != nil {
				errs = append(errs, err)
				kept[rel] = old[rel]
				continue
			}
			res.Written = append(res.Written, target)
		case err == nil:
			res.Kept = append(res.Kept, target)
			continue
		case errors.Is(err, fs.ErrNotExist):
			if err := writeFile(target, f); err != nil {
				errs = append(errs, err)
				continue
			}
			res.Written = append(res.Written, target)
		default:
			errs = append(errs, err)
			continue
		}
		kept[rel] = h
	}

	for rel, h := range old {
		if _, still := want[rel]; still {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		cur, err := os.ReadFile(target)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err == nil && sum(cur) == h:
			if err := os.Remove(target); err != nil {
				errs = append(errs, err)
				kept[rel] = h
				continue
			}
			res.Removed = append(res.Removed, target)
			pruneEmpty(filepath.Dir(target), root)
		default:
			// Edited since: the person's now.
			res.Kept = append(res.Kept, target)
		}
	}

	if len(kept) == 0 && len(old) == 0 {
		return res, errors.Join(errs...)
	}
	if err := writeManifest(manifestPath, kept, old); err != nil {
		errs = append(errs, err)
	}
	return res, errors.Join(errs...)
}

func writeManifest(path string, files, old map[string]string) error {
	if len(files) == len(old) {
		same := true
		for k, v := range files {
			if old[k] != v {
				same = false
			}
		}
		if same {
			return nil
		}
	}
	raw, _ := json.MarshalIndent(map[string]any{"files": files}, "", "  ")
	return writeFile(path, file{data: append(raw, '\n'), mode: 0o644})
}

func pruneEmpty(dir, root string) {
	for dir != root && strings.HasPrefix(dir, root) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// writeFile writes through a temporary file, so an agent that reads a skill never sees half of it.
func writeFile(target string, f file) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".harnessly-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(f.data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(name, f.mode)); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, target); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
