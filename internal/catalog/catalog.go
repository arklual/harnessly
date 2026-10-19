package catalog

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors of a folder check, as the composer shows them.
const (
	ErrOutside  = "Каталог вне рабочей папки"
	ErrNotFound = "Каталог не найден"
)

const cacheTTL = time.Minute

// Config is where the commands come from.
type Config struct {
	// ClaudeDir is Claude Code's config folder, CodexDir is Codex's home.
	ClaudeDir, CodexDir string
	// WorkspaceRoot is the runtime's workspace folder: no folder means it, and project commands are looked up to it.
	WorkspaceRoot string
	// QuickPromptsFile and WorkspacesFile are the runtime's data files.
	QuickPromptsFile, WorkspacesFile string
}

// Result is a catalog, its encoded body and the content tag of its items and warnings.
type Result struct {
	Catalog Catalog
	Body    json.RawMessage
	Tag     string
}

type cached struct {
	Result
	signature string
	at        time.Time
}

// Commands builds catalogs, keeping each provider and folder's for a minute and until a scanned root changes.
type Commands struct {
	cfg Config
	now func() time.Time
	// encode turns a catalog into the bytes served, the same encoding the route writes.
	encode func(any) []byte

	mu    sync.Mutex
	cache map[string]cached
}

// New returns a catalog builder; encode is how answers are encoded.
func New(cfg Config, encode func(any) []byte) *Commands {
	return &Commands{cfg: cfg, now: time.Now, encode: encode, cache: map[string]cached{}}
}

// IsProvider says whether an agent CLI is known.
func IsProvider(p string) bool { return p == "claude" || p == "codex" }

func (c *Commands) root() string {
	root, _ := filepath.Abs(c.cfg.WorkspaceRoot)
	if real, ok := realpath(root); ok {
		return real
	}
	return root
}

// ResolveCwd checks the folder project commands are looked up from: it must resolve (links included) inside the
// workspace root or a folder of one of the workspaces; none means the workspace root. It returns the resolved folder
// and the root it lies in, or the error text.
func (c *Commands) ResolveCwd(cwd string) (dir, root, errText string) {
	top := c.root()
	if cwd == "" {
		return top, top, ""
	}
	if !filepath.IsAbs(cwd) || utf16Len(cwd) > 4096 || strings.ContainsRune(cwd, 0) {
		return "", "", ErrOutside
	}
	real, ok := realpath(cwd)
	if !ok {
		return "", "", ErrNotFound
	}
	for _, candidate := range append([]string{top}, workspaceDirs(c.cfg.WorkspacesFile)...) {
		if rc, ok := realpath(candidate); ok && isInside(rc, real) {
			return real, rc, ""
		}
	}
	return "", "", ErrOutside
}

func (c *Commands) watched(provider, cwd, root string) []string {
	if provider == "claude" {
		return append(claudeWatched(c.cfg.ClaudeDir, cwd, root), c.cfg.QuickPromptsFile)
	}
	return codexWatched(c.cfg.CodexDir)
}

func signature(paths []string) string {
	parts := make([]string, len(paths))
	for i, p := range paths {
		parts[i] = strconv.FormatFloat(mtimeMs(p), 'f', -1, 64)
	}
	return strings.Join(parts, ",")
}

// Get is the catalog of one agent CLI for a folder ResolveCwd accepted.
func (c *Commands) Get(provider, cwd, root string) Result {
	key := provider + "\x00" + cwd
	sig := signature(c.watched(provider, cwd, root))
	c.mu.Lock()
	hit, ok := c.cache[key]
	c.mu.Unlock()
	if ok && hit.signature == sig && c.now().Sub(hit.at) < cacheTTL {
		return hit.Result
	}
	var b *builder
	if provider == "claude" {
		b = scanClaude(c.cfg.ClaudeDir, cwd, root, quickPrompts(c.cfg.QuickPromptsFile))
	} else {
		b = scanCodex(c.cfg.CodexDir)
	}
	cat := Catalog{
		Provider:    provider,
		Cwd:         cwd,
		GeneratedAt: c.now().UnixMilli(),
		Items:       b.items,
		Warnings:    b.warnings,
		Truncated:   b.truncated,
	}
	if b.truncated {
		cat.Warnings = append(append([]string{}, b.warnings...), tooManyWarning)
	}
	for _, it := range cat.Items {
		cat.Counts.add(it.Source)
	}
	sum := sha1.Sum(c.encode([]any{cat.Items, cat.Warnings}))
	res := Result{Catalog: cat, Body: c.encode(cat), Tag: `"` + hex.EncodeToString(sum[:])[:20] + `"`}
	c.mu.Lock()
	c.cache[key] = cached{Result: res, signature: sig, at: c.now()}
	c.mu.Unlock()
	return res
}

// Invalidate drops the provider's cached catalogs: the CLI reloaded its plugins or skills, which may live outside the
// scanned folders.
func (c *Commands) Invalidate(provider string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.cache {
		if strings.HasPrefix(key, provider+"\x00") {
			delete(c.cache, key)
		}
	}
}
