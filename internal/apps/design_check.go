package apps

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// The design check of `harnessly app validate` (HRN-695): warnings, never errors, about what makes a view look foreign
// in the pane — an inline style, a native <select>, a colour written out instead of a --t-* token (the view then misses
// the theme). --strict counts them, as it does every warning.

var (
	inlineStyle = regexp.MustCompile(`(?i)<[a-z][^>]*\sstyle\s*=`)
	// An address from the root ("/assets/app.js"): the view's address carries its session, so only a relative one finds the file.
	// "/__hl/" is the host's own (the SDK, hl-ui.css) and is rewritten for the page; "//host" and "/#" are not the view's files.
	rootPath     = regexp.MustCompile(`(?i)\s(?:src|href)\s*=\s*["']/(?:[^/_#"'\s]|_[^_])`)
	nativeSelect = regexp.MustCompile(`(?i)<select[\s>]`)
	styleBlock   = regexp.MustCompile(`(?is)<style[^>]*>(.*?)</style>`)
	// A colour value: #rgb… or rgb()/hsl()/oklch(); a var(--x, #fff) fallback is taken out before the look.
	literalColor = regexp.MustCompile(`(?i)(#[0-9a-f]{3,8}\b|\b(rgba?|hsla?|oklch|oklab|lab|lch)\()`)
	varCall      = regexp.MustCompile(`var\([^()]*(\([^()]*\)[^()]*)*\)`)
	cssComment   = regexp.MustCompile(`(?s)/\*.*?\*/`)
	// A custom property's own value (--x: #fff) is a token's default, which the guide asks for: not a colour written out.
	customProp = regexp.MustCompile(`--[A-Za-z0-9_-]+\s*:[^;{}]*`)
)

// designSkip are the folders the check does not read: dependencies and build output are not the author's view, and
// templates are the content of documents (a Sinopia board draws its own colours).
var designSkip = map[string]bool{"node_modules": true, ".git": true, "dist": true, "build": true, "lib": true, "vendor": true, "templates": true}

// DesignProblems reads the package's HTML and CSS for the design warnings, with the file and line of each.
func DesignProblems(dir string) Problems {
	var out Problems
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != dir && designSkip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".html" && ext != ".htm" && ext != ".css" {
			return nil
		}
		if info, err := d.Info(); err != nil || info.Size() > 1<<20 || strings.HasSuffix(p, ".min.css") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, designOf(filepath.ToSlash(rel), string(raw), ext == ".css")...)
		return nil
	})
	return out
}

func designOf(rel, text string, css bool) Problems {
	var out Problems
	warn := func(at int, msg string) {
		line := strings.Count(text[:at], "\n") + 1
		out = append(out, Problem{Path: rel + ":" + strconv.Itoa(line), Message: msg, Warning: true})
	}
	colors := func(body string, base int) {
		body = cssComment.ReplaceAllStringFunc(body, blank)
		body = varCall.ReplaceAllStringFunc(body, blank)
		body = customProp.ReplaceAllStringFunc(body, blank)
		if loc := literalColor.FindStringIndex(body); loc != nil {
			warn(base+loc[0], "a colour written out ("+strings.TrimSuffix(body[loc[0]:loc[1]], "(")+"): use the --t-* tokens (or hl-ui.css) so the view follows the theme, light and dark")
		}
	}
	if css {
		colors(text, 0)
		return out
	}
	if loc := inlineStyle.FindStringIndex(text); loc != nil {
		warn(loc[0], "an inline style attribute: put it in a class (hl-ui.css has .hl-row, .hl-card, .hl-btn and the rest)")
	}
	if loc := nativeSelect.FindStringIndex(text); loc != nil {
		warn(loc[0], "a native <select> looks foreign in the pane: use .hl-seg buttons for a few choices, or a list")
	}
	if strings.HasPrefix(rel, "ui/") {
		if loc := rootPath.FindStringIndex(text); loc != nil {
			warn(loc[0], "an address from the root (\""+strings.TrimSpace(text[loc[0]:loc[1]])+"..): the view is served with its session in the path, so only a relative address "+
				"finds the file (a bundler: base './'; Vite: base: './' in vite.config.ts)")
		}
	}
	for _, m := range styleBlock.FindAllStringSubmatchIndex(text, -1) {
		colors(text[m[2]:m[3]], m[2])
	}
	return out
}

// blank keeps the length in bytes (so an offset and a line number stay right) and drops the text.
func blank(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}
