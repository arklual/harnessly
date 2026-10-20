package ports

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// Component is a component picked in a preview: its name, an Angular host tag, or the source a React build named.
type Component struct {
	Name    string
	HostTag string
	File    string
	Line    int
}

type Match struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Kind string `json:"kind"`
}

const (
	maxMatches       = 3
	searchTimeout    = 1500 * time.Millisecond
	maxBuffer        = 256 * 1024
	maxTemplateBytes = 512 * 1024
	maxTagLength     = 100
	maxFileLength    = 1024
)

var (
	excludedDirs = []string{"node_modules", "dist", ".git", ".next", "build"}
	nameRe       = regexp.MustCompile(`^[A-Za-z_$][\w$]{0,99}$`)
	hostTagRe    = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	templateURL  = regexp.MustCompile(`templateUrl:\s*['"]([^'"\n]+)['"]`)
	hitLine      = regexp.MustCompile(`^(\d+):`)
	// ErrSearchTimedOut: rg or grep did not finish in time.
	ErrSearchTimedOut = errors.New("search timed out")
)

// ParseComponent validates the picked component; false for anything else.
func ParseComponent(v any) (Component, bool) {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return Component{}, false
	}
	get := func(k string) (any, bool) { return o.Get(k) }
	name, _ := get("name")
	n, ok := name.(string)
	if !ok || !nameRe.MatchString(n) {
		return Component{}, false
	}
	c := Component{Name: n}
	if raw, present := get("hostTag"); present {
		tag, ok := raw.(string)
		if !ok || jscompat.Len(tag) > maxTagLength || !hostTagRe.MatchString(tag) {
			return Component{}, false
		}
		c.HostTag = tag
	}
	if raw, present := get("source"); present {
		src, _ := raw.(*jsvalue.Object)
		if src == nil {
			src = jsvalue.NewObject()
		}
		f, _ := src.Get("file")
		l, _ := src.Get("line")
		file, ok := f.(string)
		if !ok || file == "" || jscompat.Len(file) > maxFileLength || strings.Contains(file, "\x00") {
			return Component{}, false
		}
		line, ok := l.(float64)
		if !ok || line != float64(int(line)) || line < 1 {
			return Component{}, false
		}
		c.File, c.Line = file, int(line)
	}
	return c, true
}

func escapeRegex(v string) string {
	return regexp.MustCompile(`[.*+?^${}()|[\]\\]`).ReplaceAllString(v, `\$0`)
}

func angularSelectorPattern(tag string) string { return `selector:\s*['"]` + escapeRegex(tag) + `['"]` }

func reactDeclarationPattern(name string) string {
	return `(function|const|class)\s+` + escapeRegex(name) + `\b`
}

type hit struct {
	file string
	line int
}

// parseSearchOutput reads `rg|grep --null -n` hits: path NUL line:text. Colons in the path survive because the path
// ends at the NUL.
func parseSearchOutput(out string, limit int) []hit {
	var hits []hit
	for _, row := range strings.Split(out, "\n") {
		nul := strings.IndexByte(row, 0)
		if nul < 1 {
			continue
		}
		m := hitLine.FindStringSubmatch(row[nul+1:])
		if m == nil {
			continue
		}
		line, _ := strconv.Atoi(m[1])
		hits = append(hits, hit{strings.TrimPrefix(row[:nul], "./"), line})
		if len(hits) == limit {
			break
		}
	}
	return hits
}

// containedRelative is the root-relative path of file when its real path lies strictly inside the real root.
func containedRelative(root, file string) string {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ""
	}
	target := file
	if !filepath.IsAbs(target) {
		target = filepath.Join(realRoot, file)
	}
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(realRoot, real)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return rel
}

func rgArgs(pattern string, globs []string) []string {
	args := []string{"--null", "-n", "--no-heading", "--no-config", "--max-count", strconv.Itoa(maxMatches), "--max-filesize", "1M"}
	for _, g := range globs {
		args = append(args, "-g", g)
	}
	for _, d := range excludedDirs {
		args = append(args, "-g", "!"+d)
	}
	return append(args, "-e", pattern, "--", ".")
}

func grepArgs(pattern string, globs []string) []string {
	args := []string{"-rnZE", "-I", "-m", strconv.Itoa(maxMatches)}
	for _, g := range globs {
		args = append(args, "--include="+g)
	}
	for _, d := range excludedDirs {
		args = append(args, "--exclude-dir="+d)
	}
	return append(args, "-e", pattern, "--", ".")
}

// Searcher runs rg, or grep where rg is missing.
type Searcher struct {
	RG, Grep string
	Timeout  time.Duration
}

type limitedBuffer struct {
	bytes.Buffer
	over bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxBuffer {
		b.over = true
		return 0, errors.New("maxBuffer exceeded")
	}
	return b.Buffer.Write(p)
}

func (s Searcher) exec(bin string, args []string, root string) ([]hit, error) {
	timeout := s.Timeout
	if timeout == 0 {
		timeout = searchTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	var out limitedBuffer
	cmd.Stdout = &out
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ErrSearchTimedOut
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return parseSearchOutput(out.String(), maxMatches), nil
}

func (s Searcher) Search(root, pattern string, globs []string) ([]hit, error) {
	rg, grep := s.RG, s.Grep
	if rg == "" {
		rg = "rg"
	}
	if grep == "" {
		grep = "grep"
	}
	hits, err := s.exec(rg, rgArgs(pattern, globs), root)
	if err != nil && err != ErrSearchTimedOut {
		return s.exec(grep, grepArgs(pattern, globs), root)
	}
	return hits, err
}

func templateOf(root, componentFile string) string {
	abs := componentFile
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, componentFile)
	}
	st, err := os.Stat(abs)
	if err != nil || st.Size() > maxTemplateBytes {
		return ""
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	m := templateURL.FindStringSubmatch(string(raw))
	if m == nil {
		return ""
	}
	return containedRelative(root, filepath.Join(filepath.Dir(abs), m[1]))
}

func contained(root string, hits []hit) []hit {
	var out []hit
	for _, h := range hits {
		if f := containedRelative(root, h.file); f != "" {
			out = append(out, hit{f, h.line})
		}
	}
	return out
}

// Resolve maps a component to its files under root.
func (s Searcher) Resolve(root string, c Component) ([]Match, error) {
	matches := []Match{}
	if c.HostTag != "" {
		hits, err := s.Search(root, angularSelectorPattern(c.HostTag), []string{"*.ts"})
		if err != nil {
			return nil, err
		}
		hits = contained(root, hits)
		for _, h := range hits {
			matches = append(matches, Match{h.file, h.line, "component"})
		}
		for _, h := range hits {
			if t := templateOf(root, h.file); t != "" {
				matches = append(matches, Match{t, 1, "template"})
			}
		}
		if len(matches) > maxMatches {
			matches = matches[:maxMatches]
		}
		return matches, nil
	}
	if c.File != "" {
		if f := containedRelative(root, c.File); f != "" {
			return []Match{{f, c.Line, "component"}}, nil
		}
	}
	hits, err := s.Search(root, reactDeclarationPattern(c.Name), []string{"*.tsx", "*.jsx"})
	if err != nil {
		return nil, err
	}
	for _, h := range contained(root, hits) {
		matches = append(matches, Match{h.file, h.line, "component"})
	}
	return matches, nil
}
