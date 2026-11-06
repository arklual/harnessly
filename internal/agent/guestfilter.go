package agent

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// What a hub reaches of the runtime over an observe link that keeps the access to the machine's designs
// (AGENT_HUB_DESIGN=on): the read of one project for a guest of a share link, and nothing else. A tunnel of such a link
// ends at this filter. It lets through GET and HEAD of <platform API>/design-public/<project>[/<file>] with no query, no
// cookie and no upgrade; the runtime checks the guest token of each request. The rest of the machine stays the cluster's.

// designOptIn is the operator's switch on an observe link's machine.
const designOptIn = "AGENT_HUB_DESIGN"

// guestRoute is the hub's guest read under the link's base path: /api/<name>/design-public/<tail>.
var guestRoute = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/design-public/(.+)$`)

type guestFilter struct {
	base, public string
	audit        *Audit
	proxy        *httputil.ReverseProxy
	srv          *http.Server
	denials      atomic.Int64
}

// newGuestFilter filters the hub's requests under base (/m/<machine>) for the runtime at 127.0.0.1:runtimePort, whose
// platform API is platformAPI.
func newGuestFilter(runtimePort int, base, platformAPI string, audit *Audit) *guestFilter {
	f := &guestFilter{base: strings.TrimRight(base, "/"), public: strings.TrimRight(platformAPI, "/") + "/design-public/", audit: audit}
	host := net.JoinHostPort("127.0.0.1", strconv.Itoa(runtimePort))
	f.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			tail := guestTail(f.base, pr.In)
			raw := f.public + tail
			p, _ := url.PathUnescape(raw)
			pr.Out.URL = &url.URL{Scheme: "http", Host: host, Path: p, RawPath: raw}
			pr.Out.Host = pr.In.Host
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Del("Authorization")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeFilterError(w, http.StatusBadGateway, "the runtime did not answer", "")
		},
	}
	f.srv = &http.Server{Handler: f, ReadHeaderTimeout: 10 * time.Second}
	return f
}

// guestTail is the escaped <project>[/<file>] of a guest read, "" for anything else.
func guestTail(base string, r *http.Request) string {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return ""
	}
	if r.URL.RawQuery != "" || r.Header.Get("Upgrade") != "" || r.Header.Get("Cookie") != "" {
		return ""
	}
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), base)
	if !ok {
		return ""
	}
	m := guestRoute.FindStringSubmatch(rest)
	if m == nil {
		return ""
	}
	for _, seg := range strings.Split(m[1], "/") {
		s, err := url.PathUnescape(seg)
		if err != nil || s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\\x00") {
			return ""
		}
	}
	return m[1]
}

func (f *guestFilter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if guestTail(f.base, r) == "" {
		f.denials.Add(1)
		f.audit.Record("route.denied", false, map[string]any{"method": r.Method, "path": cleanForAudit(r.URL.Path),
			"reason": "an observe link serves only the guest read of a design"})
		writeFilterError(w, http.StatusForbidden, "This machine is run by its own cluster: the hub reaches only the guest pages of shared designs", "machine_policy")
		return
	}
	f.proxy.ServeHTTP(w, r)
}

// Start listens on a free loopback port and serves; the port is where the link's tunnels go.
func (f *guestFilter) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	go func() { _ = f.srv.Serve(ln) }()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (f *guestFilter) Close() { _ = f.srv.Close() }

// designServed says the hub keeps the access to this machine's designs: on its own link when there is a runtime; on an
// observe link only when the operator opted in with AGENT_HUB_DESIGN=on. The kill switch turns it off either way.
func (h *hub) designServed() bool {
	if h.a.cfg.RuntimePort == 0 || h.policy.Disabled() {
		return false
	}
	return !h.observe || os.Getenv(designOptIn) == "on"
}
