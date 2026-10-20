package sources

import (
	"regexp"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

var (
	absolutePath   = regexp.MustCompile(`(?:^|` + jscompat.SpaceClass[:len(jscompat.SpaceClass)-1] + `'"=(])(/(?:[\w.@+-]+/)*[\w.@+-]+)`)
	hiddenWorkPath = regexp.MustCompile(`^/(proc|sys|dev|tmp/claude-[^/]+/[^/]+/tasks)\b`)
)

// Paths are the filesystem locations a conversation worked with, in the order first seen: a Set of the previous
// runtime, values as they came (a patch file without a path counts as one too).
type Paths struct {
	order []any
	seen  map[string]bool
}

func pathKey(v any) string {
	switch x := v.(type) {
	case string:
		return "s" + x
	case nil:
		return "null"
	}
	return "v" + string(jsvalue.Stringify(v, ""))
}

func (p *Paths) add(v any) {
	if p.seen == nil {
		p.seen = map[string]bool{}
	}
	k := pathKey(v)
	if p.seen[k] {
		return
	}
	p.seen[k] = true
	p.order = append(p.order, v)
}

func (p *Paths) addMatches(text any) {
	s, _ := text.(string)
	for _, m := range absolutePath.FindAllStringSubmatch(s, -1) {
		p.add(m[1])
	}
}

// Add takes in the locations of entries: tool targets, paths in shell commands, patched files, command folders.
func (p *Paths) Add(entries []*jsvalue.Object) {
	for _, e := range entries {
		switch e.String("type") {
		case "tool-call":
			if fp, _ := e.Get("filePath"); jscompat.Truthy(fp) {
				p.add(fp)
			}
			if e.String("toolName") == "Bash" {
				s, _ := e.Get("summary")
				p.addMatches(s)
			}
		case "patch-apply":
			for _, f := range e.Array("files") {
				if o, ok := f.(*jsvalue.Object); ok {
					v, _ := o.Get("path")
					p.add(v)
				} else {
					p.add(nil)
				}
			}
		case "exec-command-stream":
			if cwd, _ := e.Get("cwd"); jscompat.Truthy(cwd) {
				p.add(cwd)
			}
			s, _ := e.Get("command")
			p.addMatches(s)
		}
	}
}

// List is the locations newest first, without system and scratch task paths; cwd, when given, is the oldest.
func (p *Paths) List(cwd string) []any {
	all := p.order
	if cwd != "" {
		all = append([]any{cwd}, p.order...)
		if p.seen["s"+cwd] {
			all = all[:1]
			for _, v := range p.order {
				if s, ok := v.(string); !ok || s != cwd {
					all = append(all, v)
				}
			}
		}
	}
	out := make([]any, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		if !hiddenWorkPath.MatchString(jscompat.String(all[i])) {
			out = append(out, all[i])
		}
	}
	return out
}
