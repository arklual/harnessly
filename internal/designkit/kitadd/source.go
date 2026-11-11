package kitadd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Source is what a kit is made from: an npm package or a shadcn registry.
type Source struct {
	Kind    string // npm or shadcn
	Package string // npm: the package name
	Version string // npm: the version asked for ("" = the newest)
	Base    string // shadcn: the registry (a folder or an address); "" = shadcn/ui itself
}

// String is the source as the command takes it.
func (s Source) String() string {
	if s.Kind == "shadcn" {
		if s.Base == "" {
			return "shadcn:ui"
		}
		return "shadcn:" + s.Base
	}
	if s.Version == "" {
		return "npm:" + s.Package
	}
	return "npm:" + s.Package + "@" + s.Version
}

var (
	npmNameRe = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*$`)
	// npmSpecRe: a package a registry may ask for: a name with an optional version or range, never a path, an
	// address, a git or file spec or an option.
	npmSpecRe = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._~-]*/)?[a-z0-9][a-z0-9._~-]*(@[A-Za-z0-9.^~<>=| *+-]{1,64})?$`)
)

// SafeNpmSpec is whether spec is a plain package (name[@version or range]) npm may be asked to install.
func SafeNpmSpec(spec string) bool {
	return npmSpecRe.MatchString(spec) && !strings.Contains(spec, "://") && !strings.HasPrefix(spec, "-")
}

// ParseSource reads npm:<package>[@<version>] or shadcn:<registry>.
func ParseSource(s string) (Source, error) {
	kind, rest, ok := strings.Cut(s, ":")
	switch {
	case !ok:
		return Source{}, fmt.Errorf("%q: the source is npm:<package>[@<version>] or shadcn:<registry>", s)
	case kind == "npm":
		name, version := rest, ""
		if i := strings.LastIndex(rest, "@"); i > 0 {
			name, version = rest[:i], rest[i+1:]
		}
		if !npmNameRe.MatchString(name) {
			return Source{}, fmt.Errorf("%q is not an npm package name", name)
		}
		return Source{Kind: "npm", Package: name, Version: version}, nil
	case kind == "shadcn":
		if rest == "ui" || rest == "" {
			rest = ""
		}
		return Source{Kind: "shadcn", Base: rest}, nil
	}
	return Source{}, fmt.Errorf("%q: unknown source %q (npm or shadcn)", s, kind)
}

// The registry of shadcn/ui itself: the list of components, the style they are written in, the base colours.
const (
	shadcnIndex  = "https://ui.shadcn.com/r/index.json"
	shadcnItems  = "https://ui.shadcn.com/r/styles/new-york-v4"
	shadcnColors = "https://ui.shadcn.com/r/colors/neutral.json"
)

// The packages every build folder gets: the page's React (the bundle does not carry it, the types are read against
// it) and the compiler that reads the props.
var buildPackages = []string{"react@18.3.1", "react-dom@18.3.1", "@types/react@18", "@types/react-dom@18", "typescript@5"}

// runCmd runs a program in dir and returns its output; the error carries the last lines of what it printed.
func runCmd(dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		tail := strings.TrimSpace(errb.String())
		if len(tail) > 2000 {
			tail = "…" + tail[len(tail)-2000:]
		}
		return out.Bytes(), fmt.Errorf("%s %s: %v\n%s", name, strings.Join(args, " "), err, tail)
	}
	return out.Bytes(), nil
}

// workDir is the build folder of a source: kept between runs so that the next run (an update) installs only what
// changed. It is the cache, not the kit: deleting it costs one install.
func workDir(o *Options, key string) (string, error) {
	if o.WorkDir != "" {
		return o.WorkDir, os.MkdirAll(o.WorkDir, 0o755)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	sum := sha256.Sum256([]byte(key))
	dir := filepath.Join(base, "harnessly", "kit-add", hex.EncodeToString(sum[:6]))
	return dir, os.MkdirAll(dir, 0o755)
}

// npmInstall installs packages into dir: a private registry comes from --registry or the .npmrc of the folder the
// command runs in (copied next to the build), a token from the environment the .npmrc names. Nothing else is asked
// of the network.
func npmInstall(o *Options, dir string, pkgs []string) error {
	if _, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"kit-build","private":true}`+"\n"), 0o644); err != nil {
			return err
		}
	}
	if o.Cwd != "" {
		if rc, err := os.ReadFile(filepath.Join(o.Cwd, ".npmrc")); err == nil {
			if err := os.WriteFile(filepath.Join(dir, ".npmrc"), rc, 0o600); err != nil {
				return err
			}
		}
	}
	args := []string{"install", "--no-audit", "--no-fund", "--ignore-scripts", "--legacy-peer-deps", "--loglevel=error"}
	if o.Registry != "" {
		args = append(args, "--registry", o.Registry)
	}
	for _, p := range pkgs {
		if !SafeNpmSpec(p) {
			return fmt.Errorf("%q is not a package npm may install here (name[@version])", p)
		}
	}
	args = append(args, pkgs...)
	o.logf("npm %s\n", strings.Join(args, " "))
	run := o.Npm
	if run == nil {
		run = func(dir string, args ...string) error { _, err := runCmd(dir, nil, "npm", args...); return err }
	}
	return run(dir, args...)
}

// fetch reads an address or a file: a registry may be a folder on disk (a mirror inside a closed network).
func (o *Options) fetch(loc string) ([]byte, error) {
	if o.Fetch != nil {
		return o.Fetch(loc)
	}
	if !strings.HasPrefix(loc, "http://") && !strings.HasPrefix(loc, "https://") {
		return os.ReadFile(loc)
	}
	cl := &http.Client{Timeout: 60 * time.Second}
	resp, err := cl.Get(loc)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: %s", loc, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

func join(base, name string) string {
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		return strings.TrimRight(base, "/") + "/" + name
	}
	return filepath.Join(base, filepath.FromSlash(name))
}

// registryItem is one item of a shadcn registry, as its built JSON (`r/<name>.json`) has it.
type registryItem struct {
	Name                 string   `json:"name"`
	Type                 string   `json:"type"`
	Description          string   `json:"description"`
	Dependencies         []string `json:"dependencies"`
	DevDependencies      []string `json:"devDependencies"`
	RegistryDependencies []string `json:"registryDependencies"`
	Files                []struct {
		Path    string `json:"path"`
		Type    string `json:"type"`
		Content string `json:"content"`
		Target  string `json:"target"`
	} `json:"files"`
	CSSVars map[string]map[string]string `json:"cssVars"`
	CSS     json.RawMessage              `json:"css"`
}

// shadcnBuild is what a registry gave: the component files written into the build folder, and how they look.
type shadcnBuild struct {
	files    []string // src/ui/<file>.tsx, the components, sorted
	deps     []string // npm packages the files import
	devDeps  []string // npm packages the styles import
	cssVars  map[string]map[string]string
	styleCSS json.RawMessage
	descs    map[string]string // file base name → the item's description
	missing  []string          // items named as dependencies that the registry does not have
	refused  []string          // npm specs that are not plain packages
}

// importAliasRe: the registry's own paths (`@/registry/new-york-v4/ui/button`, `@/components/ui/button`,
// `@/lib/utils`), which become relative paths in the build folder: src/ui, src/lib, src/hooks.
var importAliasRe = regexp.MustCompile(`(["'])@/(?:[^"']*/)?(ui|lib|hooks)/([^"']+)(["'])`)

// fetchShadcn reads every component (registry:ui) of the registry and what they need, and writes their files under
// dir/src.
func fetchShadcn(o *Options, src Source, dir string) (*shadcnBuild, error) {
	index, items, colors := shadcnIndex, shadcnItems, shadcnColors
	if src.Base != "" {
		index, items, colors = join(src.Base, "registry.json"), src.Base, ""
	}
	raw, err := o.fetch(index)
	if err != nil && src.Base != "" {
		index = join(src.Base, "index.json")
		raw, err = o.fetch(index)
	}
	if err != nil {
		return nil, fmt.Errorf("the registry's list: %w", err)
	}
	var list []registryItem
	if err := json.Unmarshal(raw, &list); err != nil {
		var reg struct {
			Items []registryItem `json:"items"`
		}
		if err2 := json.Unmarshal(raw, &reg); err2 != nil {
			return nil, fmt.Errorf("%s is neither a registry.json nor a list of items: %v", index, err)
		}
		list = reg.Items
	}
	b := &shadcnBuild{descs: map[string]string{}}
	seen := map[string]bool{}
	deps, devDeps := map[string]bool{}, map[string]bool{}
	var queue []string
	for _, it := range list {
		if it.Type == "registry:ui" {
			queue = append(queue, it.Name)
		}
	}
	sort.Strings(queue)
	// The style item carries the base CSS (and in other registries the colours).
	for _, styleName := range []string{"index", "style"} {
		if raw, err := o.fetch(join(items, styleName+".json")); err == nil {
			var it registryItem
			if json.Unmarshal(raw, &it) == nil && it.Type == "registry:style" {
				b.styleCSS = it.CSS
				b.cssVars = it.CSSVars
				for _, d := range it.DevDependencies {
					devDeps[d] = true
				}
				queue = append(queue, it.RegistryDependencies...)
				break
			}
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		loc := name // an address or a file of another registry
		if !strings.HasPrefix(name, "http://") && !strings.HasPrefix(name, "https://") && !strings.HasSuffix(name, ".json") {
			loc = join(items, name+".json")
		}
		raw, err := o.fetch(loc)
		if err != nil {
			b.missing = append(b.missing, name)
			continue
		}
		var it registryItem
		if err := json.Unmarshal(raw, &it); err != nil {
			b.missing = append(b.missing, name)
			continue
		}
		for _, d := range it.Dependencies {
			deps[d] = true
		}
		for _, d := range it.DevDependencies {
			devDeps[d] = true
		}
		if it.Type == "registry:theme" && len(it.CSSVars) > 0 && len(b.cssVars) == 0 {
			b.cssVars = it.CSSVars
		}
		queue = append(queue, it.RegistryDependencies...)
		for _, f := range it.Files {
			if f.Content == "" {
				continue
			}
			var sub string
			switch {
			case f.Type == "registry:ui" || strings.Contains(f.Path, "/ui/"):
				sub = "src/ui"
			case f.Type == "registry:lib" || strings.Contains(f.Path, "/lib/"):
				sub = "src/lib"
			case f.Type == "registry:hook" || strings.Contains(f.Path, "/hooks/"):
				sub = "src/hooks"
			default:
				continue // blocks, pages, examples: not components of the kit
			}
			base := path.Base(f.Path)
			rel := sub + "/" + base
			body := importAliasRe.ReplaceAllString(f.Content, "$1../$2/$3$4")
			if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(sub)), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
				return nil, err
			}
			if sub == "src/ui" && (strings.HasSuffix(base, ".tsx") || strings.HasSuffix(base, ".ts")) {
				b.files = append(b.files, rel)
				b.descs[strings.TrimSuffix(strings.TrimSuffix(base, ".tsx"), ".ts")] = it.Description
			}
		}
	}
	if len(b.files) == 0 {
		return nil, errors.New("the registry has no components (items of type registry:ui with their files)")
	}
	if len(b.cssVars) == 0 && colors != "" {
		if raw, err := o.fetch(colors); err == nil {
			var c struct {
				CSSVarsV4 map[string]map[string]string `json:"cssVarsV4"`
			}
			if json.Unmarshal(raw, &c) == nil {
				b.cssVars = c.CSSVarsV4
			}
		}
	}
	sort.Strings(b.files)
	sort.Strings(b.missing)
	// What a registry names is installed only when it is a plain package: no paths, addresses, git or options.
	for _, m := range []map[string]bool{deps, devDeps} {
		for d := range m {
			if !SafeNpmSpec(d) {
				delete(m, d)
				b.refused = append(b.refused, d)
			}
		}
	}
	sort.Strings(b.refused)
	b.deps = sortedSet(deps)
	b.devDeps = sortedSet(devDeps)
	return b, nil
}

func sortedSet(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
