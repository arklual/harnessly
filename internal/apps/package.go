package apps

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A package on disk: the manifest plus the files it names. Every path the manifest names stays inside the package (no
// "..", no absolute path, no link that leads out), so an installed app cannot reach the rest of the machine through its
// own files.

const (
	// MaxPackageBytes is the most a package may weigh, every file counted.
	MaxPackageBytes = 50 << 20
	// MaxInstructionsBytes is the most the always-in-context instructions may weigh; past InstructionsWarnBytes the
	// check warns that every turn pays for them.
	MaxInstructionsBytes  = 16 << 10
	InstructionsWarnBytes = 4 << 10
	// maxPackageFiles bounds the walk.
	maxPackageFiles = 5000
)

// Package is a package read from a folder.
type Package struct {
	Dir      string
	Manifest *Manifest
	// MCP is the package's .mcp.json servers by name, as written.
	MCP map[string]MCPServer
}

// MCPServer is one server of .mcp.json: a remote one (url) or, for a trusted app, a local process (command).
type MCPServer struct {
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// LoadPackage reads the package in dir and checks it whole. The package comes back whenever the manifest could be read,
// with the problems found; nil means there is no manifest to read.
func LoadPackage(dir string) (*Package, Problems) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, Problems{{Message: err.Error()}}
	}
	raw, err := os.ReadFile(filepath.Join(abs, ManifestPath))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, Problems{{Path: ManifestPath, Message: "no manifest: an app package has harnessly/app.json (harnessly app new makes one)"}}
	}
	if err != nil {
		return nil, Problems{{Path: ManifestPath, Message: err.Error()}}
	}
	m, ps := ParseManifest(raw)
	if m == nil {
		for i := range ps {
			ps[i].Path = ManifestPath
		}
		return nil, ps
	}
	p := &Package{Dir: abs, Manifest: m}
	ps = append(ps, p.checkFiles()...)
	return p, ps
}

// checkFiles is the part of the check that reads the package's files.
func (p *Package) checkFiles() Problems {
	var ps Problems
	add := func(path, format string, a ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, a...)})
	}
	warn := func(path, format string, a ...any) {
		ps = append(ps, Problem{Path: path, Message: fmt.Sprintf(format, a...), Warning: true})
	}
	m := p.Manifest

	var total int64
	files := 0
	_ = filepath.WalkDir(p.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(p.Dir, path)
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		files++
		if files > maxPackageFiles {
			add("", "more than %d files", maxPackageFiles)
			return filepath.SkipAll
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if _, err := p.inside(rel); err != nil {
				add(filepath.ToSlash(rel), "the link leads out of the package")
			}
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	if total > MaxPackageBytes {
		add("", "the package weighs %d MB, at most %d MB", total>>20, MaxPackageBytes>>20)
	}

	if ag := m.Agent; ag != nil {
		if ag.Instructions != "" {
			if info, err := p.stat("agent.instructions", ag.Instructions, &ps); err == nil {
				switch {
				case info.Size() > MaxInstructionsBytes:
					add("agent.instructions", "%s weighs %d KB, at most %d KB: move the rest into a skill", ag.Instructions, info.Size()>>10, MaxInstructionsBytes>>10)
				case info.Size() > InstructionsWarnBytes:
					warn("agent.instructions", "%s weighs %d KB: every turn of the chat carries it, keep it under %d KB", ag.Instructions, info.Size()>>10, InstructionsWarnBytes>>10)
				}
			}
		}
		for i, s := range ag.Skills {
			_, _ = p.stat(fmt.Sprintf("agent.skills[%d]", i), "skills/"+s+"/SKILL.md", &ps)
		}
		if ag.Validators != "" {
			p.checkJSON("agent.validators", ag.Validators, &ps)
		}
		if ag.Flow != "" {
			p.checkJSON("agent.flow", ag.Flow, &ps)
		}
	}

	mcpRaw, err := p.read(".mcp.json")
	switch {
	case err == nil:
		var doc struct {
			MCPServers map[string]MCPServer `json:"mcpServers"`
		}
		if err := json.Unmarshal(mcpRaw, &doc); err != nil {
			add(".mcp.json", "%v", err)
			break
		}
		p.MCP = doc.MCPServers
		for name, s := range doc.MCPServers {
			path := ".mcp.json:" + name
			switch {
			case name == hostServer:
				add(path, "%s is the host's own server: name yours otherwise", hostServer)
			case s.Command != "" && m.Trust != TrustTrusted:
				add(path, "a local server (command) needs trust trusted; a %s app uses remote servers (url)", m.Trust)
			case s.Command == "" && !strings.HasPrefix(s.URL, "https://"):
				add(path, "a remote server has an https:// url")
			}
		}
	case !errors.Is(err, fs.ErrNotExist):
		add(".mcp.json", "%v", err)
	}
	if ag := m.Agent; ag != nil {
		for i, name := range ag.MCP {
			if _, ok := p.MCP[name]; !ok {
				add(fmt.Sprintf("agent.mcp[%d]", i), "there is no server %q in .mcp.json", name)
			}
		}
	}

	for i, v := range m.Views {
		if v.Entry == "" || strings.HasPrefix(v.Entry, "https://") {
			continue
		}
		_, _ = p.stat(fmt.Sprintf("views[%d].entry", i), v.Entry, &ps)
	}
	if c := m.Chat; c != nil {
		for i, cmd := range c.Commands {
			_, _ = p.stat(fmt.Sprintf("chat.commands[%d]", i), "commands/"+strings.TrimPrefix(cmd, "/")+".md", &ps)
		}
	}

	if sin, err := m.SinopiaOf(); err == nil {
		if sin.Bindings != nil && sin.Bindings.Schema != "" {
			p.checkJSON("sinopia.bindings.schema", sin.Bindings.Schema, &ps)
		}
		for i, k := range sin.Kinds {
			path := fmt.Sprintf("sinopia.kinds[%d]", i)
			if k.Schema != "" {
				p.checkJSON(path+".schema", k.Schema, &ps)
			}
			if k.Icon != "" {
				_, _ = p.stat(path+".icon", k.Icon, &ps)
			}
			for from, file := range k.Migrations {
				if file == "" {
					continue
				}
				if _, err := p.stat(path+".migrations."+from, file, &ps); err != nil {
					continue
				}
				raw, err := p.read(file)
				if err != nil {
					continue
				}
				if _, err := ParsePatch(raw); err != nil {
					add(path+".migrations."+from, "%s: %v", file, err)
				}
			}
		}
		for i, t := range sin.Templates {
			_, _ = p.stat(fmt.Sprintf("sinopia.templates[%d]", i), strings.TrimSuffix(t, "/")+"/"+TemplateBoard, &ps)
		}
	}

	if raw, err := p.read(".claude-plugin/plugin.json"); err == nil {
		var pj struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &pj) != nil || pj.Name == "" {
			add(".claude-plugin/plugin.json", "is not a plugin manifest with a name")
		} else if pj.Name != PluginName(m.ID) {
			warn(".claude-plugin/plugin.json", "name %q differs from %q: the install writes its own", pj.Name, PluginName(m.ID))
		}
	}
	return ps
}

// PluginName is the Claude Code plugin name of an app: its skills are <plugin>:<skill> to the agent.
func PluginName(id string) string { return strings.ReplaceAll(id, ".", "-") }

// inside resolves a path of the manifest to a file of the package, refusing one that leaves it.
func (p *Package) inside(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("%q is not a path inside the package", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%q leads out of the package", rel)
	}
	full := filepath.Join(p.Dir, clean)
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return full, err
	}
	root, err := filepath.EvalSymlinks(p.Dir)
	if err != nil {
		return full, err
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%q leads out of the package", rel)
	}
	return real, nil
}

func (p *Package) read(rel string) ([]byte, error) {
	full, err := p.inside(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (p *Package) stat(field, rel string, ps *Problems) (fs.FileInfo, error) {
	full, err := p.inside(rel)
	if err == nil {
		var info fs.FileInfo
		info, err = os.Stat(full)
		if err == nil && info.IsDir() {
			err = fmt.Errorf("%s is a folder", rel)
		}
		if err == nil {
			return info, nil
		}
	}
	msg := err.Error()
	if errors.Is(err, fs.ErrNotExist) {
		msg = fmt.Sprintf("there is no file %s in the package", rel)
	}
	*ps = append(*ps, Problem{Path: field, Message: msg})
	return nil, err
}

func (p *Package) checkJSON(field, rel string, ps *Problems) {
	if _, err := p.stat(field, rel, ps); err != nil {
		return
	}
	raw, _ := p.read(rel)
	if !json.Valid(raw) {
		*ps = append(*ps, Problem{Path: field, Message: rel + " is not valid JSON"})
	}
}

// File reads a file of the package by a manifest path.
func (p *Package) File(rel string) ([]byte, error) { return p.read(rel) }
