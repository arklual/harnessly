package files

import (
	"os"
	"regexp"
	"strings"
	"unicode/utf16"

	"golang.org/x/text/collate"
)

// The «@»-context picker: files under a conversation's folder matching a query by fuzzy path.

const (
	contextMaxScanned = 5_000
	contextMaxResults = 20
	// ContextMaxQuery is how much of the query counts, in UTF-16 units.
	ContextMaxQuery = 200
)

var ignoredDirs = map[string]bool{".git": true, "node_modules": true, "dist": true, "build": true, ".next": true, "out": true, ".turbo": true, ".cache": true}

// secretFile: names never offered as context, even inside the workspace.
var secretFile = regexp.MustCompile(asciiFold(`^\.env(\..*)?$|\.(pem|key|p12|pfx)$|^id_(rsa|ed25519|ecdsa)(\.pub)?$`))

// AllowedContextRoot: dir (resolved) is one of the roots or inside one, each root compared by its resolved path.
func AllowedContextRoot(dir string, roots []string) bool {
	for _, root := range roots {
		if root == "" {
			continue
		}
		real, err := Realpath(resolveAbs(root))
		if err != nil {
			continue
		}
		if dir == real || strings.HasPrefix(dir, real+"/") {
			return true
		}
	}
	return false
}

// FuzzyScore matches every query unit in order, case-insensitively; lower is better (a tighter, earlier match). ok is
// false when the query does not match. Offsets count UTF-16 units, as the picker always did.
func FuzzyScore(candidate string, query []uint16) (float64, bool) {
	if len(query) == 0 {
		return float64(len(utf16Units(candidate))), true
	}
	text := utf16Units(lowerText(candidate))
	needle := lowerUnits(query)
	ti, first, last := 0, -1, -1
	for _, u := range needle {
		index := -1
		for i := ti; i < len(text); i++ {
			if text[i] == u {
				index = i
				break
			}
		}
		if index == -1 {
			return 0, false
		}
		if first == -1 {
			first = index
		}
		last = index
		ti = index + 1
	}
	return float64(last-first) + float64(first)*0.1, true
}

// lowerUnits lower-cases UTF-16 units; a lone surrogate (a query cut inside a pair) stays as it is.
func lowerUnits(units []uint16) []uint16 {
	var out []uint16
	for i := 0; i < len(units); {
		u := units[i]
		if utf16.IsSurrogate(rune(u)) {
			if i+1 < len(units) && u < 0xDC00 && units[i+1] >= 0xDC00 && units[i+1] <= 0xDFFF {
				out = append(out, utf16Units(lowerText(string(utf16.Decode(units[i:i+2]))))...)
				i += 2
				continue
			}
			out = append(out, u)
			i++
			continue
		}
		out = append(out, utf16Units(lowerText(string(rune(u))))...)
		i++
	}
	return out
}

// QueryUnits is the picker's query: its first 200 UTF-16 units.
func QueryUnits(q string) []uint16 {
	u := utf16Units(q)
	if len(u) > ContextMaxQuery {
		u = u[:ContextMaxQuery]
	}
	return u
}

func walkContext(dir, rel string, out *[]string) {
	if len(*out) >= contextMaxScanned {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if len(*out) >= contextMaxScanned {
			return
		}
		name := DecodeUTF8([]byte(e.Name()))
		if ignoredDirs[name] {
			continue
		}
		isDir, isFile := e.IsDir(), e.Type().IsRegular()
		if isDir && strings.HasPrefix(name, ".") {
			continue
		}
		if isFile && secretFile.MatchString(name) {
			continue
		}
		sub := name
		if rel != "" {
			sub = rel + "/" + name
		}
		if isDir {
			walkContext(joinPath(dir, name), sub, out)
		} else if isFile {
			*out = append(*out, sub)
		}
	}
}

// SearchContext is up to 20 files under root (resolved) matching the query, best first.
func SearchContext(root string, query []uint16) []string {
	var all []string
	walkContext(root, "", &all)
	type hit struct {
		path  string
		score float64
	}
	var hits []hit
	for _, rel := range all {
		if score, ok := FuzzyScore(rel, query); ok {
			hits = append(hits, hit{rel, score})
		}
	}
	sortStable(hits, func(c *collate.Collator, a, b hit) int {
		if d := a.score - b.score; d != 0 {
			if d < 0 {
				return -1
			}
			return 1
		}
		return c.CompareString(a.path, b.path)
	})
	if len(hits) > contextMaxResults {
		hits = hits[:contextMaxResults]
	}
	files := make([]string, len(hits))
	for i, h := range hits {
		files[i] = h.path
	}
	return files
}

// joinPath is dir/name for an absolute dir, "/" included.
func joinPath(dir, name string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + name
	}
	return dir + "/" + name
}
