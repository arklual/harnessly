package preview

import (
	"net/http"
	"sort"
	"strings"
)

// Headers are a message's headers the way the previous runtime's HTTP library held them: lower-case names, Set-Cookie
// as a list, a repeated Cookie joined with "; ", other repeats joined with ", " except those of which only the first
// counts.
type Headers map[string][]string

var firstOnly = map[string]bool{"age": true, "authorization": true, "content-length": true, "content-type": true, "etag": true,
	"expires": true, "from": true, "host": true, "if-modified-since": true, "if-unmodified-since": true, "last-modified": true,
	"location": true, "max-forwards": true, "proxy-authorization": true, "referer": true, "retry-after": true, "server": true,
	"user-agent": true}

// HeadersOf reads Go's header map.
func HeadersOf(h http.Header) Headers {
	out := Headers{}
	for k, vs := range h {
		if len(vs) == 0 {
			continue
		}
		name := strings.ToLower(k)
		switch {
		case name == "set-cookie":
			out[name] = append([]string(nil), vs...)
		case name == "cookie":
			out[name] = []string{strings.Join(vs, "; ")}
		case firstOnly[name]:
			out[name] = []string{vs[0]}
		default:
			out[name] = []string{strings.Join(vs, ", ")}
		}
	}
	return out
}

// Get is a header's value, "" when absent; Set-Cookie's lines joined.
func (h Headers) Get(name string) string { return strings.Join(h[name], ", ") }

func (h Headers) Has(name string) bool { _, ok := h[name]; return ok }

func (h Headers) Set(name, value string) { h[name] = []string{value} }

func (h Headers) Del(name string) { delete(h, name) }

func (h Headers) Clone() Headers {
	out := make(Headers, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// hopByHop are a connection's own headers, never passed on as a message's.
var hopByHop = map[string]bool{"connection": true, "keep-alive": true, "proxy-connection": true, "transfer-encoding": true, "te": true,
	"trailer": true, "upgrade": true}

// writeTo puts the headers on a response, leaving out the connection's own.
func (h Headers) writeTo(dst http.Header) {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if hopByHop[k] {
			continue
		}
		for _, v := range h[k] {
			dst.Add(k, v)
		}
	}
}
