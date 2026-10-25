package design

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// React, ReactDOM and Babel come from the runtime, not from unpkg: a board then does not wait for a third-party CDN
// (nor fail without one). The engine (support.js) asks window.__resources[url] before it builds a <script src=url>, so
// a script placed before the engine maps the three exact URLs it asks for to /__vendor/ files; any other URL (a release
// engine on other versions) still goes to unpkg as before. The files are the unpkg bytes, their sha384 is the engine's
// SRI (a test holds the two together).
//
// window.__resources also tells the engine it runs from a standalone export, and boot() then skips refetching the raw
// .dc.html (the template as written, before the HTML parser moved a {{ }} out of a <table>). A board needs that refetch,
// so the map is a getter that answers undefined once: on the first read after init() has put __dcBoot on window, which
// is boot()'s check. React and ReactDOM are asked for before init, Babel, stylesheets and siblings after boot.
const vendorPrefix = "/__vendor/"

//go:embed vendor/*.js
var vendorFS embed.FS

type vendorFile struct {
	URL  string // the unpkg address the engine asks for
	Name string // the name under /__vendor/
	SRI  string
	file string // in vendor/

	once sync.Once
	raw  []byte
	gz   []byte
}

var vendorFiles = map[string]*vendorFile{
	"https://unpkg.com/react@18.3.1/umd/react.production.min.js": {
		Name: "react@18.3.1.js", file: "react.production.min.js",
		SRI: "sha384-DGyLxAyjq0f9SPpVevD6IgztCFlnMF6oW/XQGmfe+IsZ8TqEiDrcHkMLKI6fiB/Z",
	},
	"https://unpkg.com/react-dom@18.3.1/umd/react-dom.production.min.js": {
		Name: "react-dom@18.3.1.js", file: "react-dom.production.min.js",
		SRI: "sha384-gTGxhz21lVGYNMcdJOyq01Edg0jhn/c22nsx0kyqP0TxaV5WVdsSH1fSDUf5YJj1",
	},
	"https://unpkg.com/@babel/standalone@7.29.0/babel.min.js": {
		Name: "babel-standalone@7.29.0.js", file: "babel.min.js",
		SRI: "sha384-m08KidiNqLdpJqLq95G/LEi8Qvjl/xUYll3QILypMoQ65QorJ9Lvtp2RXYGBFj1y",
	},
}

func init() {
	for url, f := range vendorFiles {
		f.URL = url
	}
}

func (f *vendorFile) load() {
	f.once.Do(func() {
		raw, err := vendorFS.ReadFile("vendor/" + f.file)
		if err != nil {
			panic("design: vendor file missing from the build: " + f.file)
		}
		f.raw = raw
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, 9)
		_, _ = zw.Write(f.raw)
		_ = zw.Close()
		f.gz = buf.Bytes()
	})
}

func (f *vendorFile) body() []byte {
	f.load()
	return f.raw
}

// resourcesScript is the script that goes before the engine in a board.
func resourcesScript() string {
	m := make(map[string]string, len(vendorFiles))
	for url, f := range vendorFiles {
		m[url] = vendorPrefix + f.Name
	}
	b, _ := json.Marshal(m) // sorted keys
	return `<script>(function(){var m=` + string(b) + `,seen=false;Object.defineProperty(window,"__resources",{configurable:true,` +
		`get:function(){if(!seen&&window.__dcBoot){seen=true;return undefined}return m}})})()</script>`
}

func (l *Library) serveVendor(w http.ResponseWriter, r *http.Request, pathname string, head bool) {
	name := strings.TrimPrefix(pathname, vendorPrefix)
	for _, f := range vendorFiles {
		if f.Name != name {
			continue
		}
		body := f.body()
		etag := `"` + f.SRI + `"`
		h := w.Header()
		h.Set("ETag", etag)
		h.Set("Cache-Control", immutable)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Type", "text/javascript; charset=utf-8")
		h.Add("Vary", "Accept-Encoding")
		if inm, ok := r.Header["If-None-Match"]; ok && strings.Join(inm, ", ") == etag {
			w.WriteHeader(304)
			return
		}
		if takesGzip(r) {
			body = f.gz
			h.Set("Content-Encoding", "gzip")
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(200)
		if !head {
			_, _ = w.Write(body)
		}
		return
	}
	sendHTML(w, 404, l.notFoundPage(), head)
}
