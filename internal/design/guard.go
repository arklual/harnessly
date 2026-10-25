package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The guard of project/canvas.json: in a project of a design library the file is built from meta/ and an edit by hand
// would be overwritten (or, worse, believed). An agent of the design tool is used to editing it, so a PreToolUse hook
// of the agent refuses the write and says which command to use. The guard reads the hook's JSON from stdin and needs
// nothing else: a project is a folder with project/ and artifact.json or meta/ next to it.

// GuardInput is the part of a PreToolUse hook's input the guard reads.
type GuardInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
		Path         string `json:"path"`
		Pattern      string `json:"pattern"`
		Command      string `json:"command"`
	} `json:"tool_input"`
	Cwd string `json:"cwd"`
}

var writingTools = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true}

// patchHeader matches the files an apply_patch body touches: «*** Add File: p», «*** Update File: p», «*** Delete File: p»
// and the «*** Move to: p» of a rename.
var patchHeader = regexp.MustCompile(`(?mi)^[ \t]*\*\*\*[ \t]*(?:Add File|Update File|Delete File|Move to)[ \t]*:[ \t]*(\S.*?)[ \t]*$`)

// patchTargets: the paths an apply_patch body writes (Codex sends the patch as the command), so each is checked like a
// Write.
func patchTargets(patch string) []string {
	var out []string
	for _, m := range patchHeader.FindAllStringSubmatch(patch, -1) {
		out = append(out, m[1])
	}
	return out
}

// isLibraryCanvas: file is <project>/project/canvas.json of a design project (relative paths are taken from cwd).
func isLibraryCanvas(file, cwd string) bool {
	if file == "" {
		return false
	}
	if !filepath.IsAbs(file) && cwd != "" {
		file = filepath.Join(cwd, file)
	}
	file = filepath.Clean(file)
	if filepath.Base(file) != canvasJSONName || filepath.Base(filepath.Dir(file)) != ProjectDirname {
		return false
	}
	top := filepath.Dir(filepath.Dir(file))
	return exists(filepath.Join(top, ArtifactFile)) || exists(filepath.Join(top, MetaDirname))
}

// canvasWrites are the shell forms that write a file named canvas.json: a redirect into it, tee, an in-place edit,
// a move, copy or removal naming it, a script that writes or dumps with it on the line.
var canvasWrites = regexp.MustCompile(`>{1,2}\s*['"]?[^\s'"|;&]*canvas\.json|` +
	`\btee\b[^|;&]*canvas\.json|` +
	`\b(?:sed|perl)\s+[^|;&]*-[a-zA-Z]*i[^|;&]*canvas\.json|\bsed\s+[^|;&]*--in-place[^|;&]*canvas\.json|` +
	`\b(?:mv|cp|rm|truncate|install|dd|ln|rsync)\b[^|;&]*canvas\.json|` +
	`\b(?:python3?|node|ruby|deno|bun)\b[^|&]*canvas\.json[^|&]*\b(?:write|dump|writeFile)|` +
	`\b(?:python3?|node|ruby|deno|bun)\b[^|&]*\b(?:write|dump|writeFile)[^|&]*canvas\.json`)

// GuardCanvas says whether a tool call writes the built canvas.json of a design project, and the words for the agent.
func GuardCanvas(in GuardInput, cli string) (deny bool, reason string) {
	if cli == "" {
		cli = "harnessly"
	}
	reason = locale.Pick(
		"project/canvas.json of a design project is built from its meta/ and is read-only: an edit here would be overwritten. "+
			"Change the pages with `"+cli+" design page create|rename|remove|list <project> …`; the board files themselves (*.dc.html) are edited as usual.",
		"project/canvas.json проекта дизайна собирается из meta/ и только для чтения: правка здесь будет перезаписана. "+
			"Страницы меняются командами `"+cli+" design page create|rename|remove|list <проект> …`; сами доски (*.dc.html) правятся как обычно.")
	if writingTools[in.ToolName] {
		for _, f := range []string{in.ToolInput.FilePath, in.ToolInput.NotebookPath, in.ToolInput.Path} {
			if isLibraryCanvas(f, in.Cwd) {
				return true, reason
			}
		}
		return false, ""
	}
	if in.ToolName == "Bash" {
		c := in.ToolInput.Command
		if strings.Contains(c, ProjectDirname+"/"+canvasJSONName) && canvasWrites.MatchString(c) {
			return true, reason
		}
	}
	return false, ""
}

// GuardHookAnswer is the hook's answer to Claude Code: a refusal with its reason, or nothing. It knows nothing of
// who may open which project; GuardHook does.
func GuardHookAnswer(raw []byte, cli string) []byte {
	return GuardHook(raw, cli, GuardEnv{})
}

// GuardEnv is what the hook needs beyond its input to judge access: the runtime's data folder (the snapshot of the
// gateway is there) and the time. Empty DataDir: no snapshot, so everything is allowed as before.
type GuardEnv struct {
	DataDir string
	// LibraryDir is the design library when the hook knows it: a search that starts at it or above it reads every
	// project below.
	LibraryDir string
	Now        time.Time
}

func denyAnswer(reason string) []byte {
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason,
	}})
	return append(out, '\n')
}

// GuardHook is GuardHookAnswer with the access check: a call that reads a project the person has no role in, or writes
// one they may only view, is refused with the words for the agent. With no snapshot, or one that does not enforce, the
// check is skipped (an old gateway, a machine that never got one); with one, an input the hook cannot read is refused,
// never let through.
func GuardHook(raw []byte, cli string, env GuardEnv) []byte {
	var access *Library
	if env.DataDir != "" {
		l := New(Config{DataDir: env.DataDir})
		if l.Enforcing() {
			access = l
		}
	}
	var in GuardInput
	if err := json.Unmarshal(raw, &in); err != nil {
		if access != nil {
			return denyAnswer(locale.Pick("The access to design projects could not be checked: the tool call was not understood.", "Не удалось проверить доступ к проектам: вызов инструмента не разобран."))
		}
		return nil
	}
	if access != nil {
		if reason := guardAccess(in, access, env); reason != "" {
			return denyAnswer(reason)
		}
	}
	deny, reason := GuardCanvas(in, cli)
	if !deny {
		return nil
	}
	return denyAnswer(reason)
}

// GuardCLI is the command name agents are told to run: RUNTIME_CLI_NAME when the deployment names one.
func GuardCLI() string {
	if v := strings.TrimSpace(os.Getenv("RUNTIME_CLI_NAME")); v != "" {
		return v
	}
	return "harnessly"
}

var readingTools = map[string]bool{"Read": true, "Glob": true, "Grep": true, "NotebookRead": true, "LS": true}

// projectTop is the project folder a path is in: the nearest folder up the path with project/ and artifact.json,
// meta/ or .owner.json beside it. Its parent is the library.
func projectTop(p string) (root, id string, ok bool) {
	p = filepath.Clean(p)
	for dir := p; ; {
		if st, err := os.Stat(filepath.Join(dir, ProjectDirname)); err == nil && st.IsDir() &&
			(exists(filepath.Join(dir, ArtifactFile)) || exists(filepath.Join(dir, MetaDirname)) || exists(filepath.Join(dir, OwnerFile))) {
			if id := filepath.Base(dir); IsLibraryID(id) {
				return filepath.Dir(dir), id, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
		}
		dir = parent
	}
}

// shellWrites: a shell command that may change files. A best effort, like the canvas guard: it decides only for
// people who may look and not touch.
var shellWrites = regexp.MustCompile(`(^|[^<>=&|-])>{1,2}[^&]|\b(?:tee|mv|cp|rm|rmdir|mkdir|touch|truncate|install|dd|ln|rsync|chmod|chown|patch)\b|` +
	`\b(?:sed|perl)\b[^|;&]*\s-[a-zA-Z]*i|\bgit\s+(?:commit|checkout|reset|apply|add|restore|clean|stash|merge|rebase|pull)\b|` +
	`\b(?:npm|pnpm|yarn|bun)\s+(?:install|add|i|run|exec)\b|\b(?:python3?|node|ruby|deno|perl|bash|sh)\b|\bdesign\s+(?:page|claim|history)\b`)

// shellSearches: commands that read a whole tree from the folder they stand in.
var shellSearches = regexp.MustCompile(`\b(?:find|grep|rg|ag|ack|tree|du|locate)\b|\bls\s+-[a-zA-Z]*R|\bcp\s+-[a-zA-Z]*[rR]`)

// shellTokens splits a command into the words that can be paths.
var shellTokens = regexp.MustCompile(`[^\s'"` + "`" + `;|&<>()=:,]+`)

const guardOwnerFileText = "The owner file of a design project is kept by the platform and is not edited by agents."
const guardAccessFileText = "The access snapshot of the design library is kept by the platform and is not edited by agents."

func guardAccessFileReason() string {
	return locale.Pick(guardAccessFileText, "Снимок доступа к проектам дизайна ведёт платформа, агенты его не правят.")
}

// The honest border of the hook: it sees the tool call, not the file system. A shell command can build a path the hook
// does not read (variables, a script, a symlink); this stops the direct ways, the folder itself is closed in stage 2b.

type guardCtx struct {
	in    GuardInput
	env   GuardEnv
	who   Identity
	now   time.Time
	libs  map[string]*Library
	roots map[string]bool
	home  string
}

func (g *guardCtx) lib(root string) *Library {
	l := g.libs[root]
	if l == nil {
		l = New(Config{DataDir: g.env.DataDir, LibraryDir: root})
		g.libs[root] = l
	}
	return l
}

func (g *guardCtx) addRoot(root string) {
	if root == "" {
		return
	}
	g.roots[root] = true
	if real, err := filepath.EvalSymlinks(root); err == nil {
		g.roots[real] = true
	}
}

// refuseBelow: some project of the library holds less than min for the person; the words for that project.
func (g *guardCtx) refuseBelow(root string, min Role) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	l := g.lib(root)
	// The words name no project: a search that was not aimed at one must not tell which ones exist.
	generic := locale.Pick("The search touches closed Designly projects. Point it at the folder of a project you have access to.",
		"Поиск задевает закрытые проекты Designly. Укажите папку проекта, к которому есть доступ.")
	for _, e := range entries {
		id := e.Name()
		if !e.IsDir() || !IsLibraryID(id) || !exists(filepath.Join(root, id, ProjectDirname)) {
			continue
		}
		if !l.RoleOf(id, g.who, g.now).AtLeast(min) {
			return generic
		}
	}
	return ""
}

// path judges one absolute, cleaned path. spans: the path is the folder before a wildcard, so everything below it is
// meant; recursive: a search that goes down from the path.
func (g *guardCtx) path(p string, min Role, spans, recursive bool) string {
	if !filepath.IsAbs(p) {
		return ""
	}
	if root, id, ok := projectTop(p); ok {
		g.addRoot(root)
		l := g.lib(root)
		if role := l.RoleOf(id, g.who, g.now); !role.AtLeast(min) {
			return l.NoAccessText(id, role)
		}
		return ""
	}
	for root := range g.roots {
		above := strings.HasPrefix(root, p+string(filepath.Separator))
		if p == root || (above && (recursive || spans)) {
			if reason := g.refuseBelow(root, min); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func (g *guardCtx) abs(f, base string) string {
	switch {
	case f == "~" || strings.HasPrefix(f, "~/"):
		f = filepath.Join(g.home, strings.TrimPrefix(f, "~"))
	case strings.HasPrefix(f, "$HOME"), strings.HasPrefix(f, "${HOME}"):
		f = filepath.Join(g.home, strings.TrimPrefix(strings.TrimPrefix(f, "$HOME"), "${HOME}"))
	}
	if !filepath.IsAbs(f) {
		f = filepath.Join(base, f)
	}
	return filepath.Clean(f)
}

// glob: the folder a pattern starts in and whether it has a wildcard.
func globBase(f string) (string, bool) {
	i := strings.IndexAny(f, "*?[")
	if i < 0 {
		return f, false
	}
	return filepath.Dir(f[:i] + "x"), true
}

// pathOrGlob judges a path or a pattern the way the tool takes it.
func (g *guardCtx) pathOrGlob(f, base string, min Role, recursive bool) string {
	f = g.abs(f, base)
	if dir, glob := globBase(f); glob {
		return g.path(dir, min, true, true)
	}
	return g.path(f, min, false, recursive)
}

// guardAccess is the reason to refuse a tool call by access, "" to let it go on.
func guardAccess(in GuardInput, l *Library, env GuardEnv) string {
	now := env.Now
	if now.IsZero() {
		now = time.Now()
	}
	who, _ := l.Identity()
	g := &guardCtx{in: in, env: env, who: who, now: now, libs: map[string]*Library{}, roots: map[string]bool{}}
	g.home, _ = os.UserHomeDir()
	libDir := env.LibraryDir
	if libDir == "" {
		libDir = l.libraryRootHint()
	}
	if libDir != "" {
		if abs, err := filepath.Abs(libDir); err == nil {
			g.addRoot(abs)
		}
	}
	cwd := in.Cwd
	if cwd != "" {
		cwd = filepath.Clean(cwd)
		if root, _, ok := projectTop(cwd); ok {
			g.addRoot(root)
		}
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	tool, ti := in.ToolName, in.ToolInput
	switch {
	case writingTools[tool]:
		for _, f := range append([]string{ti.FilePath, ti.NotebookPath, ti.Path}, patchTargets(ti.Command)...) {
			if f == "" {
				continue
			}
			p := g.abs(f, cwd)
			if filepath.Base(p) == OwnerFile {
				if root, id, ok := projectTop(p); ok && p == filepath.Join(root, id, OwnerFile) {
					return locale.Pick(guardOwnerFileText, "Файл владельца проекта дизайна ведёт платформа, агенты его не правят.")
				}
			}
			if base := filepath.Base(p); base == AccessFile || base == LibraryRootFile {
				return guardAccessFileReason()
			}
			if reason := g.path(p, RoleEdit, false, false); reason != "" {
				return reason
			}
		}
	case tool == "Grep":
		p := cwd
		if ti.Path != "" {
			p = g.abs(ti.Path, cwd)
		}
		// The pattern of a search is a regular expression, never a path. Grep always goes down the tree.
		if reason := g.path(p, RoleView, false, true); reason != "" {
			return reason
		}
	case tool == "Glob":
		base := cwd
		if ti.Path != "" {
			base = g.abs(ti.Path, cwd)
		}
		if reason := g.pathOrGlob(ti.Pattern, base, RoleView, true); reason != "" {
			return reason
		}
		if reason := g.path(base, RoleView, false, false); reason != "" {
			return reason
		}
	case readingTools[tool]:
		for _, f := range []string{ti.FilePath, ti.NotebookPath, ti.Path} {
			if f == "" {
				continue
			}
			if reason := g.pathOrGlob(f, cwd, RoleView, false); reason != "" {
				return reason
			}
		}
		if ti.FilePath == "" && ti.NotebookPath == "" && ti.Path == "" {
			if reason := g.path(cwd, RoleView, false, false); reason != "" {
				return reason
			}
		}
	case tool == "Bash":
		return g.bash(cwd)
	}
	return ""
}

func (g *guardCtx) bash(cwd string) string {
	cmd := g.in.ToolInput.Command
	writes := shellWrites.MatchString(cmd)
	min := RoleView
	if writes {
		min = RoleEdit
	}
	if strings.Contains(cmd, OwnerFile) && writes {
		return locale.Pick(guardOwnerFileText, "Файл владельца проекта дизайна ведёт платформа, агенты его не правят.")
	}
	if (strings.Contains(cmd, AccessFile) || strings.Contains(cmd, LibraryRootFile)) && writes {
		return guardAccessFileReason()
	}
	searches := shellSearches.MatchString(cmd)
	words := shellTokens.FindAllString(cmd, -1)
	// A cd moves the base the later relative words are read from.
	bases := []string{cwd}
	for i, w := range words {
		if (w == "cd" || w == "pushd") && i+1 < len(words) {
			bases = append(bases, g.abs(words[i+1], cwd))
		}
	}
	if reason := g.path(cwd, min, false, false); reason != "" {
		return reason
	}
	for _, base := range bases {
		for _, w := range words {
			if w == "" || w[0] == '-' && !strings.ContainsAny(w, "/~$") {
				continue
			}
			if reason := g.pathOrGlob(w, base, min, searches); reason != "" {
				return reason
			}
		}
	}
	// Standing at or above the library: a tree search reaches every project below.
	if searches {
		for _, base := range bases {
			if reason := g.path(base, min, false, true); reason != "" {
				return reason
			}
		}
	}
	return ""
}
