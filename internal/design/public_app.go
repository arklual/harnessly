package design

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// The guest page's bundle (the second page of the web build, guest.html) under the public route: _app/index.html is
// the page, _app/<file> a file it needs. Only files the page's entry reaches in the build's manifest are served, so the
// rest of the build (the app's own chunks, the manifest itself) is not.

const (
	guestEntry    = "guest.html"
	manifestFile  = ".vite/manifest.json"
	appIndex      = "index.html"
	appImmutable  = "public, max-age=31536000, immutable"
	appNoCache    = "no-cache"
	appFilesLimit = 4096
)

type manifestChunk struct {
	File           string   `json:"file"`
	CSS            []string `json:"css"`
	Assets         []string `json:"assets"`
	Imports        []string `json:"imports"`
	DynamicImports []string `json:"dynamicImports"`
}

// guestFiles are the files of the guest entry in the build: its script, the chunks it imports (also lazily: the
// locales), their styles and assets. Nil when there is no build or no such entry.
func guestFiles(app fs.FS) map[string]bool {
	raw, err := fs.ReadFile(app, manifestFile)
	if err != nil {
		return nil
	}
	var m map[string]manifestChunk
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	if _, ok := m[guestEntry]; !ok {
		return nil
	}
	out := map[string]bool{}
	seen := map[string]bool{}
	var walk func(key string)
	walk = func(key string) {
		c, ok := m[key]
		if !ok || seen[key] || len(out) > appFilesLimit {
			return
		}
		seen[key] = true
		if c.File != "" {
			out[c.File] = true
		}
		for _, f := range c.CSS {
			out[f] = true
		}
		for _, f := range c.Assets {
			out[f] = true
		}
		for _, k := range c.Imports {
			walk(k)
		}
		for _, k := range c.DynamicImports {
			walk(k)
		}
	}
	walk(guestEntry)
	return out
}

func (a *API) guestSet() map[string]bool {
	if a.GuestApp == nil {
		return nil
	}
	a.guestOnce.Do(func() { a.guestFiles = guestFiles(a.GuestApp) })
	return a.guestFiles
}

// publicApp answers _app/<rest>: the guest page or one file of its bundle.
func (a *API) publicApp(w http.ResponseWriter, r *http.Request, rest []string, head bool) {
	set := a.guestSet()
	if set == nil || len(rest) == 0 {
		publicText(w, 404, "Not found")
		return
	}
	name := path.Join(rest...)
	cache := appImmutable
	if name == appIndex {
		name, cache = guestEntry, appNoCache
	} else if !set[name] {
		publicText(w, 404, "Not found")
		return
	}
	typ := ContentType(extname(name))
	if typ == "" {
		typ = appType(name)
	}
	f, err := a.GuestApp.Open(name)
	if err != nil {
		publicText(w, 404, "Not found")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || typ == "" {
		publicText(w, 404, "Not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	h.Set("Cache-Control", cache)
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	if !head {
		_, _ = io.Copy(w, f)
	}
}

// appType: the types of a build's files beyond the ones a design serves.
func appType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".map":
		return ""
	case ".wasm":
		return "application/wasm"
	case ".webmanifest":
		return "application/manifest+json"
	case ".mp3":
		return "audio/mpeg"
	}
	return ""
}
