package router

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// MemoryRecaller searches the agents' memory folders under root (Claude Code's <config>/projects/*/memory, one file
// per memory) for a query: the files whose words meet the query's most, as «description: the start of the body».
// The words are compared by their stems (the first five letters), so «обсуждали» meets «обсуждение».
func MemoryRecaller(root string) Recaller {
	return func(ctx context.Context, query string) []string {
		want := stems(query)
		if len(want) == 0 {
			return nil
		}
		files, _ := filepath.Glob(filepath.Join(root, "*", "memory", "*.md"))
		type hit struct {
			score int
			line  string
		}
		var hits []hit
		seen := map[string]bool{}
		for _, f := range files {
			if ctx.Err() != nil {
				break
			}
			if filepath.Base(f) == "MEMORY.md" {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil || len(raw) > 64<<10 {
				continue
			}
			desc, body := split(string(raw))
			have := stems(desc + " " + body)
			score := 0
			for w := range want {
				if have[w] {
					score++
				}
			}
			if score == 0 {
				continue
			}
			line := clip(one(desc), 200)
			if b := clip(one(body), 300); b != "" {
				line += ": " + b
			}
			if seen[line] {
				continue
			}
			seen[line] = true
			hits = append(hits, hit{score, line})
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		var out []string
		for i, h := range hits {
			if i >= 6 {
				break
			}
			out = append(out, h.line)
		}
		return out
	}
}

// split is a memory's description (from its front matter) and its body.
func split(s string) (desc, body string) {
	if !strings.HasPrefix(s, "---") {
		return "", s
	}
	rest := s[3:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", s
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "description:"); ok {
			desc = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return desc, rest[end+4:]
}

var stop = map[string]bool{"помни": true, "вчера": true, "сегод": true, "давай": true, "котор": true, "about": true,
	"remem": true, "yeste": true, "обсуж": true, "говор": true, "тему": true, "темой": true}

func stems(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		r := []rune(w)
		if len(r) < 4 {
			continue
		}
		if len(r) > 5 {
			r = r[:5]
		}
		if st := string(r); !stop[st] {
			out[st] = true
		}
	}
	return out
}
