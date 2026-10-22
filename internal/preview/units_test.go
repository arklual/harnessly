package preview

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
	"github.com/ly-solutions/harnessly/internal/preview/ports"
	"github.com/ly-solutions/harnessly/internal/preview/weburl"
)

// units.json holds the previous runtime's preview functions run on generated inputs (testdata/gen/gen-units.mjs); every
// case must come out the same here.

type units struct {
	URLs []struct {
		In  string          `json:"in"`
		Out *map[string]any `json:"out"`
	} `json:"urls"`
	Refs []struct {
		Base, Ref string
		Out       *map[string]any `json:"out"`
	} `json:"refs"`
	Claims []struct {
		Token string          `json:"token"`
		Out   json.RawMessage `json:"out"`
	} `json:"claims"`
	ClaimSecret   string `json:"claimSecret"`
	ClaimAudience string `json:"claimAudience"`
	CookieTags    []struct {
		Target map[string]any `json:"target"`
		Tag    string         `json:"tag"`
	} `json:"cookieTags"`
	Forbidden []struct {
		In  string `json:"in"`
		Out bool   `json:"out"`
	} `json:"forbidden"`
	Literals []struct {
		In  string          `json:"in"`
		Out *map[string]any `json:"out"`
		V4  *string         `json:"v4"`
	} `json:"literals"`
	Resolve []struct {
		Host string         `json:"host"`
		Out  map[string]any `json:"out"`
	} `json:"resolve"`
	DNS     map[string][]string `json:"dns"`
	Rewrite []struct {
		Ctx, Text, Out string
	} `json:"rewrite"`
	RewriteCtx    map[string]json.RawMessage `json:"rewriteCtx"`
	StreamRewrite []struct {
		Ctx    string   `json:"ctx"`
		Chunks []string `json:"chunks"`
		Out    string   `json:"out"`
	} `json:"streamRewrite"`
	Inject []struct {
		Chunks []string `json:"chunks"`
		Out    string   `json:"out"`
	} `json:"inject"`
	RespHeaders []struct {
		Ctx     string                     `json:"ctx"`
		Headers map[string]json.RawMessage `json:"headers"`
		Opts    struct {
			BodyRewritten bool   `json:"bodyRewritten"`
			BasePath      string `json:"basePath"`
			CookieTag     string `json:"cookieTag"`
			Stand         bool   `json:"stand"`
		} `json:"opts"`
		Out map[string]json.RawMessage `json:"out"`
	} `json:"respHeaders"`
	Cookies []struct {
		Kind    string  `json:"kind"`
		In      string  `json:"in"`
		Own     *string `json:"own"`
		Session *string `json:"session"`
		Out     *string `json:"out"`
	} `json:"cookies"`
	Stands []struct {
		Host     string  `json:"host"`
		IsHost   bool    `json:"isHost"`
		IsOrigin bool    `json:"isOrigin"`
		SignIn   *string `json:"signIn"`
	} `json:"stands"`
	SignInRedirects []struct {
		Origin, Next string
		Out          bool
	} `json:"signInRedirects"`
	CacheControl []struct {
		Headers  map[string]string `json:"headers"`
		Document bool              `json:"document"`
		Out      string            `json:"out"`
	} `json:"cacheControl"`
	Verdicts []struct {
		Method  string                     `json:"method"`
		Status  int                        `json:"status"`
		Headers map[string]json.RawMessage `json:"headers"`
		Out     *struct {
			TTL float64 `json:"ttlSeconds"`
		} `json:"out"`
	} `json:"verdicts"`
	AllowsCache []struct {
		Method  string            `json:"method"`
		Headers map[string]string `json:"headers"`
		Out     bool              `json:"out"`
	} `json:"allowsCache"`
	AssetKey string `json:"assetKey"`
	Accept   []struct {
		In       string   `json:"in"`
		Accepted []string `json:"accepted"`
		Upstream string   `json:"upstream"`
		Outgoing *string  `json:"outgoing"`
	} `json:"accept"`
	Codings []struct {
		In  string  `json:"in"`
		Out *string `json:"out"`
	} `json:"codings"`
	Compressible []struct {
		In  string `json:"in"`
		Out bool   `json:"out"`
	} `json:"compressible"`
	Vary []struct {
		In, Out string
	} `json:"vary"`
	Describe []struct {
		Headers map[string]string `json:"headers"`
		Coding  *string           `json:"coding"`
		Recoded bool              `json:"recoded"`
		Out     map[string]string `json:"out"`
	} `json:"describe"`
	Pages struct {
		Status []struct {
			Status int    `json:"status"`
			Text   string `json:"text"`
			Out    string `json:"out"`
		} `json:"status"`
		Offer []struct {
			URL    string  `json:"url"`
			Parent *string `json:"parent"`
			Out    string  `json:"out"`
		} `json:"offer"`
		SignIn []struct {
			URL    string  `json:"url"`
			Parent *string `json:"parent"`
			Out    string  `json:"out"`
		} `json:"signIn"`
		Policy    string `json:"policy"`
		BridgeTag []struct {
			Parent   string  `json:"parent"`
			Tag      string  `json:"tag"`
			Devtools *string `json:"devtools"`
			Out      string  `json:"out"`
		} `json:"bridgeTag"`
	} `json:"pages"`
	LogLines []struct {
		Text     string             `json:"text"`
		Limit    int                `json:"limit"`
		Problems []devtools.LogLine `json:"problems"`
		Last     []devtools.LogLine `json:"last"`
	} `json:"logLines"`
	Ports struct {
		SS         string             `json:"ss"`
		Sockets    []map[string]any   `json:"sockets"`
		Cwds       map[string]*string `json:"cwds"`
		Classified json.RawMessage    `json:"classified"`
		Listener   []struct {
			Port int             `json:"port"`
			Out  json.RawMessage `json:"out"`
		} `json:"listener"`
	} `json:"ports"`
	Normalise []struct {
		Paths []string `json:"paths"`
		Out   []string `json:"out"`
	} `json:"normalise"`
	RootsMap   map[string]string `json:"rootsMap"`
	TargetText []struct {
		Text   string           `json:"text"`
		Target *devtools.Target `json:"-"`
		RawT   json.RawMessage  `json:"target"`
		Slot   string           `json:"slot"`
		Out    string           `json:"out"`
	} `json:"targetText"`
}

var testStands = Stands{Domain: ".preview.example.org", SignInHosts: []string{"id.example.org", "keycloak.example.org", "auth.preview.example.org"}, SessionHeader: "x-acme-stand-session"}

func loadUnits(t *testing.T) *units {
	file := "testdata/units.json"
	if f := os.Getenv("PREVIEW_UNITS"); f != "" {
		file = f // a larger generated set, not committed
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var u units
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatal(err)
	}
	return &u
}

func urlView(u *weburl.URL) map[string]any {
	return map[string]any{"href": u.Href(), "origin": u.Origin(), "protocol": u.Protocol(), "host": u.Host(), "hostname": u.Hostname, "port": u.Port,
		"pathname": u.Path, "search": u.Search(), "hash": u.Hash(), "username": u.Username, "password": u.Password}
}

func ours(raw string) bool {
	for _, s := range []string{"http:", "https:", "ws:", "wss:"} {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), s) {
			return true
		}
	}
	return false
}

func TestURLsAsTheURLClass(t *testing.T) {
	u := loadUnits(t)
	for _, c := range u.URLs {
		got, ok := weburl.Parse(c.In)
		if !ours(c.In) {
			continue // only the schemes the preview opens
		}
		if c.Out == nil {
			if ok {
				t.Errorf("%q: parsed as %s, the URL class refuses it", c.In, got.Href())
			}
			continue
		}
		if !ok {
			t.Errorf("%q: refused, the URL class reads %v", c.In, (*c.Out)["href"])
			continue
		}
		if v := urlView(got); !reflect.DeepEqual(v, *c.Out) {
			t.Errorf("%q:\n got %v\nwant %v", c.In, v, *c.Out)
		}
	}
	for _, c := range u.Refs {
		base, _ := weburl.Parse(c.Base)
		got, ok := weburl.ParseRef(c.Ref, base)
		if c.Out == nil || !ours((*c.Out)["href"].(string)) {
			if ok && c.Out == nil {
				t.Errorf("%q on %q: parsed, the URL class refuses it", c.Ref, c.Base)
			}
			continue
		}
		if !ok {
			t.Errorf("%q on %q: refused", c.Ref, c.Base)
			continue
		}
		if v := urlView(got); !reflect.DeepEqual(v, *c.Out) {
			t.Errorf("%q on %q:\n got %v\nwant %v", c.Ref, c.Base, v, *c.Out)
		}
	}
}

func claimJSON(c *Claim) []byte {
	if c == nil {
		return []byte("null")
	}
	target, _ := jsvalue.Parse(nodejson.Marshal(c.Target))
	o := jsvalue.NewObject("sub", c.Sub, "target", target, "slotOrigin", c.SlotOrigin)
	slots := jsvalue.NewObject()
	keys := make([]string, 0, len(c.PortSlots))
	for k := range c.PortSlots {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		slots.Set(k, c.PortSlots[k])
	}
	o.Set("portSlots", slots)
	if c.ParentOrigin != "" {
		o.Set("parentOrigin", c.ParentOrigin)
	}
	return nodejson.Marshal(o)
}

func sameJSON(a, b []byte) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	return reflect.DeepEqual(x, y)
}

func TestClaims(t *testing.T) {
	u := loadUnits(t)
	at := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for _, c := range u.Claims {
		got := claimJSON(VerifyClaim(c.Token, []byte(u.ClaimSecret), u.ClaimAudience, at))
		if !sameJSON(got, c.Out) {
			t.Errorf("token %s:\n got %s\nwant %s", c.Token, got, c.Out)
		}
	}
	for _, c := range u.CookieTags {
		tg := devtools.Target{Kind: c.Target["kind"].(string)}
		if p, ok := c.Target["port"].(float64); ok {
			tg.Port = int(p)
		}
		tg.Origin, _ = c.Target["origin"].(string)
		if got := CookieTag(tg, []byte(u.ClaimSecret)); got != c.Tag {
			t.Errorf("tag of %v: %s, want %s", c.Target, got, c.Tag)
		}
	}
}

func TestSSRFGuard(t *testing.T) {
	u := loadUnits(t)
	local := map[string]bool{"203.0.113.77": true, "2001:4860::1": true}
	for _, c := range u.Forbidden {
		if got := IsForbiddenAddress(c.In, local); got != c.Out {
			t.Errorf("forbidden(%q) = %v, want %v", c.In, got, c.Out)
		}
	}
	for _, c := range u.Literals {
		got := LiteralAddress(c.In)
		var want Resolved
		if c.Out != nil {
			want = Resolved{Address: (*c.Out)["address"].(string), Family: int((*c.Out)["family"].(float64))}
		}
		if got != want {
			t.Errorf("literal(%q) = %+v, want %+v", c.In, got, want)
		}
		v4 := ""
		if c.V4 != nil {
			v4 = *c.V4
		}
		if g := NormalizeIPv4(c.In); g != v4 {
			t.Errorf("normalizeIPv4(%q) = %q, want %q", c.In, g, v4)
		}
	}
	lookup := func(h string) ([]string, error) {
		a, ok := u.DNS[h]
		if !ok {
			return nil, os.ErrNotExist
		}
		return a, nil
	}
	for _, c := range u.Resolve {
		got := ResolvePublic(c.Host, lookup, map[string]bool{"203.0.113.77": true})
		want := Resolved{}
		if e, ok := c.Out["error"].(string); ok {
			want.Error = e
		} else {
			want.Address, want.Family = c.Out["address"].(string), int(c.Out["family"].(float64))
		}
		if got != want {
			t.Errorf("resolve(%q) = %+v, want %+v", c.Host, got, want)
		}
	}
}

func ctxFrom(raw json.RawMessage) rewriteCtx {
	var c struct {
		SlotOrigin   string            `json:"slotOrigin"`
		PortSlots    map[string]string `json:"portSlots"`
		ExtraOrigins map[string]string `json:"extraOrigins"`
	}
	_ = json.Unmarshal(raw, &c)
	return rewriteCtx{slotOrigin: c.SlotOrigin, portSlots: c.PortSlots, extra: c.ExtraOrigins}
}

func TestRewrites(t *testing.T) {
	u := loadUnits(t)
	for _, c := range u.Rewrite {
		if got := ctxFrom(u.RewriteCtx[c.Ctx]).rewriteLocalOrigins(c.Text); got != c.Out {
			t.Errorf("rewrite %s %q:\n got %q\nwant %q", c.Ctx, c.Text, got, c.Out)
		}
	}
	for i, c := range u.StreamRewrite {
		var buf bytes.Buffer
		rw := newOriginRewriter(&buf, ctxFrom(u.RewriteCtx[c.Ctx]))
		for _, ch := range c.Chunks {
			b, _ := base64.StdEncoding.DecodeString(ch)
			_, _ = rw.Write(b)
		}
		_ = rw.Close()
		if want, _ := base64.StdEncoding.DecodeString(c.Out); !bytes.Equal(buf.Bytes(), want) {
			t.Errorf("stream %d:\n got %q\nwant %q", i, buf.Bytes(), want)
		}
	}
	for i, c := range u.Inject {
		var buf bytes.Buffer
		j := &bridgeInjector{w: &buf, tag: []byte("<script data-x></script>")}
		for _, ch := range c.Chunks {
			b, _ := base64.StdEncoding.DecodeString(ch)
			_, _ = j.Write(b)
		}
		_ = j.Close()
		if want, _ := base64.StdEncoding.DecodeString(c.Out); !bytes.Equal(buf.Bytes(), want) {
			t.Errorf("inject %d:\n got %q\nwant %q", i, buf.Bytes(), want)
		}
	}
	for _, c := range u.RespHeaders {
		in := headersFromJSON(c.Headers)
		got := ctxFrom(u.RewriteCtx[c.Ctx]).rewriteResponseHeaders(in, testStands, headerOptions{bodyRewritten: c.Opts.BodyRewritten, basePath: c.Opts.BasePath,
			cookieTag: c.Opts.CookieTag, stand: c.Opts.Stand})
		if want := headersFromJSON(c.Out); !reflect.DeepEqual(got, want) {
			t.Errorf("headers %s %v %+v:\n got %v\nwant %v", c.Ctx, c.Headers, c.Opts, got, want)
		}
	}
	for _, c := range u.Cookies {
		var got *string
		switch c.Kind {
		case "target":
			if s := targetCookies(c.In, "tag"); s != "" {
				got = &s
			}
		case "set":
			s := prefixSetCookie(c.In, "tag", false)
			got = &s
		case "stand":
			own, session := "", ""
			if c.Own != nil {
				own = *c.Own
			}
			if c.Session != nil {
				session = *c.Session
			}
			if s := standCookie(own, session); s != "" {
				got = &s
			}
		}
		if !reflect.DeepEqual(got, c.Out) {
			t.Errorf("cookie %s %q/%v/%v: got %v want %v", c.Kind, c.In, c.Own, c.Session, deref(got), deref(c.Out))
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func headersFromJSON(m map[string]json.RawMessage) Headers {
	h := Headers{}
	for k, raw := range m {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			h[k] = []string{s}
			continue
		}
		var list []string
		_ = json.Unmarshal(raw, &list)
		h[k] = list
	}
	return h
}

func TestStands(t *testing.T) {
	u := loadUnits(t)
	for _, c := range u.Stands {
		if c.Host != "" {
			if g := testStands.IsHost(c.Host); g != c.IsHost {
				t.Errorf("isHost(%q) = %v", c.Host, g)
			}
			if g := testStands.IsOrigin("https://" + c.Host); g != c.IsOrigin {
				t.Errorf("isOrigin(%q) = %v", c.Host, g)
			}
		}
		page := "https://" + c.Host + "/p/q?x=1 2&y=é"
		if c.Host == "" {
			page = "https://st.preview.example.org/oauth2/callback"
		}
		got := testStands.signInURL(page)
		if want := deref(c.SignIn); (c.SignIn == nil && got != "") || (c.SignIn != nil && got != want) {
			t.Errorf("signIn(%q) = %q, want %q", page, got, want)
		}
	}
	for _, c := range u.SignInRedirects {
		next, _ := weburl.Parse(c.Next)
		if g := testStands.isSignInRedirect(c.Origin, next); g != c.Out {
			t.Errorf("signInRedirect(%q) = %v", c.Next, g)
		}
	}
}

func TestCacheAndCodingHeaders(t *testing.T) {
	u := loadUnits(t)
	for _, c := range u.CacheControl {
		h := Headers{}
		for k, v := range c.Headers {
			h[k] = []string{v}
		}
		if got := slotCacheControl(h, c.Document); got != c.Out {
			t.Errorf("cache-control %v doc=%v: %q want %q", c.Headers, c.Document, got, c.Out)
		}
	}
	for _, c := range u.Verdicts {
		ttl, ok := storeVerdict(c.Method, c.Status, headersFromJSON(c.Headers))
		if (c.Out != nil) != ok || ok && ttl != c.Out.TTL {
			t.Errorf("verdict %v: %v %v, want %+v", c.Headers, ttl, ok, c.Out)
		}
	}
	for _, c := range u.AllowsCache {
		h := Headers{}
		for k, v := range c.Headers {
			h[k] = []string{v}
		}
		if got := requestAllowsCache(c.Method, h); got != c.Out {
			t.Errorf("allowsCache %s %v = %v", c.Method, c.Headers, got)
		}
	}
	if got := assetCacheKey("alice", "br, gzip", "https://site.example.org/a.js"); got != u.AssetKey {
		t.Errorf("asset key %s want %s", got, u.AssetKey)
	}
	for _, c := range u.Accept {
		acc := acceptedCodings(c.In)
		if acc == nil {
			acc = []string{}
		}
		if !reflect.DeepEqual(acc, c.Accepted) || upstreamAcceptEncoding(c.In) != c.Upstream || outgoingCoding(c.In) != deref(c.Outgoing) && !(c.Outgoing == nil && outgoingCoding(c.In) == "") {
			t.Errorf("accept %q: %v %q %q", c.In, acc, upstreamAcceptEncoding(c.In), outgoingCoding(c.In))
		}
	}
	for _, c := range u.Codings {
		got := codingOf(Headers{"content-encoding": {c.In}})
		if (c.Out == nil) != (got == "") || c.Out != nil && *c.Out != got {
			t.Errorf("coding %q = %q", c.In, got)
		}
	}
	for _, c := range u.Compressible {
		if g := isCompressible(c.In); g != c.Out {
			t.Errorf("compressible %q = %v", c.In, g)
		}
	}
	for _, c := range u.Vary {
		if g := withVary(c.In, "Accept-Encoding"); g != c.Out {
			t.Errorf("vary %q = %q want %q", c.In, g, c.Out)
		}
	}
	for _, c := range u.Describe {
		h := Headers{}
		for k, v := range c.Headers {
			h[k] = []string{v}
		}
		describeBody(h, deref(c.Coding), c.Recoded)
		if c.Coding == nil {
			h2 := Headers{}
			for k, v := range c.Headers {
				h2[k] = []string{v}
			}
			describeBody(h2, "", c.Recoded)
			h = h2
		}
		want := Headers{}
		for k, v := range c.Out {
			want[k] = []string{v}
		}
		if !reflect.DeepEqual(h, want) {
			t.Errorf("describe %v %v %v: %v want %v", c.Headers, deref(c.Coding), c.Recoded, h, want)
		}
	}
}

func TestPages(t *testing.T) {
	u := loadUnits(t)
	for _, c := range u.Pages.Status {
		if g := statusPage(c.Status, c.Text); g != c.Out {
			t.Errorf("status page %d:\n%s\n%s", c.Status, g, c.Out)
		}
	}
	for _, c := range u.Pages.Offer {
		pu, _ := weburl.Parse(c.URL)
		if g := offerPage(pu, deref2(c.Parent), "NONCE"); g != c.Out {
			t.Errorf("offer page:\n%s\n%s", g, c.Out)
		}
	}
	for _, c := range u.Pages.SignIn {
		if g := signInPage(c.URL, deref2(c.Parent), "NONCE"); g != c.Out {
			t.Errorf("sign-in page:\n%s\n%s", g, c.Out)
		}
	}
	if g := offerPolicy("NONCE"); g != u.Pages.Policy {
		t.Errorf("policy %q", g)
	}
	for _, c := range u.Pages.BridgeTag {
		if g := bridgeTag(c.Parent, c.Tag, deref2(c.Devtools), c.Devtools != nil); g != c.Out {
			t.Errorf("bridge tag %q want %q", g, c.Out)
		}
	}
}

func deref2(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func TestServerLogLines(t *testing.T) {
	u := loadUnits(t)
	for _, c := range u.LogLines {
		if g := devtools.ProblemLines(c.Text, c.Limit); !reflect.DeepEqual(g, c.Problems) && !(len(g) == 0 && len(c.Problems) == 0) {
			t.Errorf("problems %q %d:\n got %v\nwant %v", c.Text, c.Limit, g, c.Problems)
		}
		if g := devtools.LastLines(c.Text, c.Limit); !reflect.DeepEqual(g, c.Last) && !(len(g) == 0 && len(c.Last) == 0) {
			t.Errorf("last %q %d:\n got %v\nwant %v", c.Text, c.Limit, g, c.Last)
		}
	}
}

func TestPortsParsing(t *testing.T) {
	u := loadUnits(t)
	socks := ports.ParseSockets(u.Ports.SS)
	var got []map[string]any
	for _, s := range socks {
		m := map[string]any{"port": float64(s.Port), "host": s.Host, "pid": nil, "command": nil}
		if s.HasPID {
			m["pid"], m["command"] = float64(s.PID), s.Command
		}
		got = append(got, m)
	}
	if !reflect.DeepEqual(got, u.Ports.Sockets) {
		t.Errorf("sockets:\n got %v\nwant %v", got, u.Ports.Sockets)
	}
	cwd := func(pid int) string {
		if c := u.Ports.Cwds[itoa(pid)]; c != nil {
			return *c
		}
		return ""
	}
	cl := ports.Classify(socks, cwd, []string{"/work/app", "/work/other/"}, "/home/agent/wt/", map[int]bool{18028: true})
	if g := nodejson.Marshal(cl); !sameJSON(g, u.Ports.Classified) {
		t.Errorf("classified:\n got %s\nwant %s", g, u.Ports.Classified)
	}
	for _, c := range u.Ports.Listener {
		l := ports.ListenerOf(socks, c.Port, map[int]bool{18028: true})
		g := []byte("null")
		if l != nil {
			g = nodejson.Marshal(map[string]any{"port": l.Port, "host": l.Host, "pid": l.PID, "command": l.Command})
		}
		if !sameJSON(g, c.Out) {
			t.Errorf("listener %d: %s want %s", c.Port, g, c.Out)
		}
	}
	for _, c := range u.Normalise {
		g := ports.NormaliseWorkRoots(c.Paths, func(p string) string { return u.RootsMap[p] }, "/home/agent/wt/", "/home/agent/")
		if !reflect.DeepEqual(g, c.Out) {
			t.Errorf("normalise %v: %v want %v", c.Paths, g, c.Out)
		}
	}
	for _, c := range u.TargetText {
		var tg *devtools.Target
		if string(c.RawT) != "null" {
			var m map[string]any
			_ = json.Unmarshal(c.RawT, &m)
			tg = &devtools.Target{Kind: m["kind"].(string)}
			if p, ok := m["port"].(float64); ok {
				tg.Port = int(p)
			}
			tg.Origin, _ = m["origin"].(string)
		}
		if g := devtools.ToTargetText(c.Text, c.Slot, tg); g != c.Out {
			t.Errorf("target text %q %v: %q want %q", c.Text, tg, g, c.Out)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
