// Package webrelease reads the contract a built page carries (web-release.json, written by tools/web-compat.mjs) and
// tells whether a runtime answers every call of a page built from another tree.
//
// A page released apart from the runtime (RUNTIME_WEB_DIR) may be newer than the runtime that serves it, and a call to a
// route the runtime does not have yet answers 404 in the middle of the page. The page compiled into the runtime came
// from the runtime's own tree, so its manifest lists the runtime's routes; the external page's manifest lists its calls.
// Every call has to be served by one of those routes, by the same rule as tools/web-routes-check.mjs: a route serves a
// call when it equals it or is a segment prefix of it, "*" and "{name}" match one segment, and a call that ends in "**"
// (a dynamic tail) is served by a longer route that starts like its head.
package webrelease

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// File is the manifest's name at the root of a built page.
const File = "web-release.json"

// Manifest is web-release.json.
type Manifest struct {
	Schema    int        `json:"schema"`
	Version   string     `json:"version"`
	SourceSHA string     `json:"sourceSha"`
	Calls     [][]string `json:"calls"`
	Routes    [][]string `json:"routes"`
}

// ErrNone is Read's error for a page that carries no manifest.
var ErrNone = errors.New("the page carries no " + File)

// Read reads the manifest at the root of a page. ErrNone when there is none.
func Read(page fs.FS) (Manifest, error) {
	var m Manifest
	if page == nil {
		return m, ErrNone
	}
	raw, err := fs.ReadFile(page, File)
	if errors.Is(err, fs.ErrNotExist) {
		return m, ErrNone
	}
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("%s: %w", File, err)
	}
	if m.Schema != 1 {
		return m, fmt.Errorf("%s: schema %d is not known", File, m.Schema)
	}
	return m, nil
}

// Missing lists the page's calls that none of the runtime's routes serves, as paths.
func Missing(page, runtime Manifest) []string {
	var out []string
	for _, c := range page.Calls {
		served := false
		for _, r := range runtime.Routes {
			if Serves(r, c) {
				served = true
				break
			}
		}
		if !served {
			out = append(out, "/"+strings.Join(c, "/"))
		}
	}
	return out
}

func wild(s string) bool { return s == "*" || (strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}")) }

// Serves is tools/web-routes-check.mjs's serves(route, call).
func Serves(route, call []string) bool {
	dynamic := len(call) > 0 && call[len(call)-1] == "**"
	head := call
	if dynamic {
		head = call[:len(call)-1]
	}
	if len(route) > len(head) && !dynamic {
		return false
	}
	n := min(len(route), len(head))
	for i := 0; i < n; i++ {
		if wild(route[i]) || head[i] == "*" {
			continue
		}
		if route[i] != head[i] {
			return false
		}
	}
	return true
}
