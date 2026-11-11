package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A preview slot on an origin of its own. The slots by path (preview.go) run a person's dev server sandboxed
// on the hub's origin; with HUB_PREVIEW_SLOT_DOMAIN each person's slot n gets the host <handle>-<n>.<domain>, so the app
// runs as itself: its own cookies, storage and service of a real origin, and nothing of the hub's.
//
// What lets a request in. The runtime still decides which slot a path is (the key in /m/<id>/__pv/<slot>-<key>/ is its
// word), the hub decides who may stand there:
//
//   - A person opens a slot with POST /api/preview/slot-open, signed in, a member of the machine. The answer is a
//     one-time ticket on the host of THEIR slot (the handle is computed from their account, never taken from the request).
//   - The ticket is traded once at /__tk/enter on that host for a cookie that only that host receives (__Host-, no
//     Domain), signed by the hub and naming the person, host, machine and slot key. Every request checks the signature,
//     that the cookie belongs to this very host, that the host's handle is the person's, that the person is still let
//     into the machine and that the switch is on; an answer for another person's host or another slot is a refusal.
//   - A request must be ours: from the slot itself, its person's other slots or the hub's own page (the frame); a
//     navigation of a whole page may come from anywhere. Service workers, the /__tk/ paths and any path outside the
//     slot's own prefix are refused, so the page reaches the slot's answers and nothing else of the machine or the hub.
//
// Rollout switch slotSwitch, per person and machine; no row means off, and an off switch answers 404 everywhere here.

// slotSwitch is the rollout switch that turns the slot hosts on (docs/hub/rollout-switches.md).
const slotSwitch = "preview-slot-host"

const (
	slotEnterPath = "/__tk/enter"
	slotReserved  = "/__tk/"
	slotCookieTTL = 12 * time.Hour
	// slotNextMax bounds the address a ticket opens first.
	slotNextMax = 2048
)

var errSlotClosed = httpx.New(http.StatusForbidden, "slot_closed", "окно закрыто: откройте его заново из страницы harnessly")

// slotKeyed says the slot hosts are configured: a domain and a key to sign with.
func (s *Service) slotKeyed() bool { return s.cfg.PreviewSlotDomain != "" && len(s.slotKey) >= 32 }

// IsSlotHost says whether host is a preview slot host of this hub.
func (s *Service) IsSlotHost(host string) bool {
	if !s.slotKeyed() {
		return false
	}
	_, _, ok := s.cfg.PreviewSlotFromHost(host)
	return ok
}

func (s *Service) slotMAC(label string, data []byte) []byte {
	mac := hmac.New(sha256.New, s.slotKey)
	mac.Write([]byte(label))
	mac.Write([]byte{0})
	mac.Write(data)
	return mac.Sum(nil)
}

// slotHandle is the person's name in their slot hosts: p plus 12 letters of a keyed hash of their account id. Nobody can
// choose it and it does not tell the account.
func (s *Service) slotHandle(userID string) string {
	sum := s.slotMAC("handle", []byte(userID))
	return "p" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum))[:12]
}

func (s *Service) slotOn(r *http.Request, u store.User, machine string) bool {
	return s.switches != nil && s.switches.On(r.Context(), slotSwitch, strings.ToLower(u.Email), machine)
}

// slotScope is what a ticket of the slot hosts is good for: this host, this machine, this slot key.
func slotScope(host, machine, key string) string { return "slot:" + host + "|" + machine + "|" + key }

// slotClaims is what the cookie says.
type slotClaims struct {
	U string `json:"u"` // the person
	H string `json:"h"` // the host the cookie belongs to
	M string `json:"m"` // the machine
	K string `json:"k"` // the slot's segment, <slot>-<key>
	E int64  `json:"e"` // expires, unix seconds
}

func (s *Service) slotCookieName() string {
	if s.cfg.Secure {
		return "__Host-hl_slot"
	}
	return "hl_slot"
}

func (s *Service) signSlotCookie(c slotClaims) string {
	body, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(s.slotMAC("cookie", body))
}

// slotCookie is the claims of the request's slot cookie when it is ours, unexpired and made for this host; nil otherwise.
func (s *Service) slotCookie(r *http.Request, host string) *slotClaims {
	ck, err := r.Cookie(s.slotCookieName())
	if err != nil {
		return nil
	}
	b64, mac, ok := strings.Cut(ck.Value, ".")
	if !ok {
		return nil
	}
	body, err1 := base64.RawURLEncoding.DecodeString(b64)
	got, err2 := base64.RawURLEncoding.DecodeString(mac)
	if err1 != nil || err2 != nil || !hmac.Equal(got, s.slotMAC("cookie", body)) {
		return nil
	}
	var c slotClaims
	if json.Unmarshal(body, &c) != nil || c.U == "" || c.H != host || s.now().Unix() >= c.E {
		return nil
	}
	if !machineID.MatchString(c.M) || !previewSegment.MatchString(c.K) {
		return nil
	}
	return &c
}

// cleanSlotPath: a path that names no dot segment, no empty segment, no backslash and no control character.
func cleanSlotPath(p string) bool {
	if p == "" || p[0] != '/' || strings.Contains(p, "//") || strings.ContainsRune(p, '\\') {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// cleanSlotRequest applies cleanSlotPath to what a request sent, also before the address is decoded.
func cleanSlotRequest(r *http.Request) bool {
	if !cleanSlotPath(r.URL.Path) {
		return false
	}
	if raw := strings.ToLower(r.URL.RawPath); strings.Contains(raw, "%2f") || strings.Contains(raw, "%5c") || strings.Contains(raw, "%00") {
		return false
	}
	return true
}

// slotFrameOrigins are the origins of the hub's own pages: the only ones that may frame a slot.
func (s *Service) slotFrameOrigins() []string {
	var out []string
	for _, raw := range []string{s.cfg.PublicURL, s.cfg.AgentURL} {
		if u, err := url.Parse(raw); err == nil && u.Scheme != "" && u.Host != "" {
			o := u.Scheme + "://" + u.Host
			dup := false
			for _, e := range out {
				dup = dup || e == o
			}
			if !dup {
				out = append(out, o)
			}
		}
	}
	return out
}

// slotHeaders are what the hub says on a slot host's answers whatever the app said: nothing that acts on the whole
// origin from a target (HSTS, Clear-Site-Data and the like), no sandbox of the path mode (the origin is the slot's own),
// framed only by the hub's pages, not for indexing and not for shared caches.
func slotHeaders(h http.Header, ancestors []string) {
	for _, name := range previewOriginHeaders {
		h.Del(name)
	}
	for _, name := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers", "Access-Control-Expose-Headers", "Access-Control-Max-Age", "Access-Control-Allow-Private-Network",
		"X-Frame-Options"} {
		h.Del(name)
	}
	policies := h.Values("Content-Security-Policy")
	h.Del("Content-Security-Policy")
	for _, p := range policies {
		if p != previewSandbox {
			h.Add("Content-Security-Policy", p)
		}
	}
	src := "'none'"
	if len(ancestors) > 0 {
		src = strings.Join(ancestors, " ")
	}
	h.Add("Content-Security-Policy", "frame-ancestors "+src)
	h.Set("X-Robots-Tag", "noindex, nofollow")
	cc := h.Get("Cache-Control")
	switch {
	case cc == "":
		h.Set("Cache-Control", "private, no-cache")
	case strings.Contains(strings.ToLower(cc), "public"):
		h.Set("Cache-Control", "private, no-cache")
	}
}

// slotClosed answers a request the slot host does not let in; a frame gets the page that tells the page above.
func (s *Service) slotClosed(w http.ResponseWriter, r *http.Request, he *httpx.Error) {
	s.log.Info("preview slot host refused", "status", he.Status, "code", he.Code, "path", httpx.SafePath(r.URL.Path))
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("Sec-Fetch-Dest") == "iframe" {
		src := "'none'"
		if o := s.slotFrameOrigins(); len(o) > 0 {
			src = strings.Join(o, " ")
		}
		serveFrameRefusalFor(w, r, he, src)
		return
	}
	httpx.WriteError(w, r, he)
}

// slotOurs says the request comes from the slot itself, from its person's other slots or from a page of the hub; a
// navigation of a whole page may come from anywhere (a link, the hub's frame).
func (s *Service) slotOurs(r *http.Request, host, handle string) bool {
	hubHosts := map[string]bool{}
	for _, o := range s.slotFrameOrigins() {
		if u, err := url.Parse(o); err == nil {
			hubHosts[strings.ToLower(u.Host)] = true
		}
	}
	ours := func(raw string) bool {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return false
		}
		h := strings.ToLower(u.Host)
		if h == host || hubHosts[h] {
			return true
		}
		other, _, ok := s.cfg.PreviewSlotFromHost(h)
		return ok && other == handle
	}
	origin := r.Header.Get("Origin")
	if origin != "" && !ours(origin) {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	case "same-site":
		return origin != "" || ours(r.Header.Get("Referer"))
	}
	// cross-site: only a page navigation.
	return isNavigation(r) && r.Header.Get("Sec-Fetch-Mode") == "navigate" &&
		(r.Header.Get("Sec-Fetch-Dest") == "document" || r.Header.Get("Sec-Fetch-Dest") == "iframe")
}

// slotOpen is POST /api/preview/slot-open {machine, path}: path is a slot's address as the runtime gave it
// (/m/<machine>/__pv/<slot>-<key>/…). The answer is the address on the person's own slot host that opens it once.
func (s *Service) slotOpen(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	if refuseCrossSite(w, r) {
		return nil
	}
	var in struct {
		Machine string `json:"machine"`
		Path    string `json:"path"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	if !machineID.MatchString(in.Machine) {
		return httpx.Invalid("machine", "нет такой машины")
	}
	m, err := s.admit(r.Context(), p.User.ID, in.Machine)
	if err != nil {
		return err
	}
	// Off is indistinguishable from not there.
	if !s.slotOn(r, p.User, m.ID) {
		return httpx.ErrNotFound
	}
	u, err := url.Parse(in.Path)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || !previewPath(u.Path, m.ID) {
		return httpx.Invalid("path", "это не окно предпросмотра машины")
	}
	rest := strings.TrimPrefix(u.Path, "/m/"+m.ID+"/__pv/")
	key, tail, _ := strings.Cut(rest, "/")
	tail = "/" + tail
	if !cleanSlotPath(tail) {
		return httpx.Invalid("path", "недопустимый адрес в окне")
	}
	next := tail
	if u.RawQuery != "" {
		next += "?" + u.RawQuery
	}
	if len(next) > slotNextMax {
		return httpx.Invalid("path", "адрес слишком длинный")
	}
	host := s.cfg.PreviewSlotHost(s.slotHandle(p.User.ID), int(key[0]-'0'))
	tk := s.tickets.Issue(p.User.ID, slotScope(host, m.ID, key))
	scheme := "http"
	if s.cfg.Secure {
		scheme = "https"
	}
	q := url.Values{"t": {tk}, "m": {m.ID}, "k": {key}, "n": {next}}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"url":  scheme + "://" + host + slotEnterPath + "?" + q.Encode(),
		"host": host,
	})
	return nil
}

// ServeSlotHost answers a request to a preview slot host.
func (s *Service) ServeSlotHost(w http.ResponseWriter, r *http.Request) {
	host := strings.ToLower(r.Host)
	handle, slot, ok := s.cfg.PreviewSlotFromHost(host)
	if !ok || !s.slotKeyed() {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if r.Header.Get("Service-Worker") != "" {
		// A worker would outlive the slot and answer for the whole origin.
		s.slotClosed(w, r, httpx.ErrForbidden)
		return
	}
	if !cleanSlotRequest(r) {
		s.slotClosed(w, r, httpx.Invalid("path", "недопустимый адрес"))
		return
	}
	if r.URL.Path == slotEnterPath {
		s.slotEnter(w, r, host, handle, slot)
		return
	}
	if strings.HasPrefix(r.URL.Path, slotReserved) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	c := s.slotCookie(r, host)
	if c == nil || s.slotHandle(c.U) != handle || int(c.K[0]-'0') != slot {
		s.slotClosed(w, r, errSlotClosed)
		return
	}
	user, err := s.db.S().UserByID(r.Context(), c.U)
	if err != nil || user.DisabledAt != nil || !s.slotOn(r, user, c.M) {
		s.slotClosed(w, r, errSlotClosed)
		return
	}
	if _, err := s.admit(r.Context(), c.U, c.M); err != nil {
		s.slotClosed(w, r, errSlotClosed)
		return
	}
	prefix := "/m/" + c.M + "/__pv/" + c.K
	switch {
	case r.URL.Path == "/" && isNavigation(r):
		http.Redirect(w, r, prefix+"/", http.StatusFound)
		return
	case r.URL.Path == prefix:
		target := prefix + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	case !strings.HasPrefix(r.URL.Path, prefix+"/"):
		// The page reaches its slot's answers and nothing else of the machine or the hub.
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if !s.slotOurs(r, host, handle) {
		s.slotClosed(w, r, httpx.ErrCrossSite)
		return
	}
	s.servePreviewOn(w, r, c.M, true)
}

// slotEnter trades a ticket for the host's cookie and sends the person on into the slot.
func (s *Service) slotEnter(w http.ResponseWriter, r *http.Request, host, handle string, slot int) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	if r.Method != http.MethodGet {
		httpx.WriteError(w, r, httpx.New(http.StatusMethodNotAllowed, "method_not_allowed", "только GET"))
		return
	}
	if mode := r.Header.Get("Sec-Fetch-Mode"); mode != "" && mode != "navigate" {
		s.slotClosed(w, r, httpx.ErrCrossSite)
		return
	}
	q := r.URL.Query()
	tk, mid, key, next := q.Get("t"), q.Get("m"), q.Get("k"), q.Get("n")
	pathPart, _, _ := strings.Cut(next, "?")
	if tk == "" || !machineID.MatchString(mid) || !previewSegment.MatchString(key) || int(key[0]-'0') != slot ||
		len(next) > slotNextMax || !cleanSlotPath(pathPart) {
		s.slotClosed(w, r, errSlotClosed)
		return
	}
	// The ticket is spent here, whatever comes next: a replay finds nothing.
	uid, ok := s.tickets.Redeem(tk, slotScope(host, mid, key))
	if !ok || s.slotHandle(uid) != handle {
		s.slotClosed(w, r, errSlotClosed)
		return
	}
	user, err := s.db.S().UserByID(r.Context(), uid)
	if err != nil || user.DisabledAt != nil || !s.slotOn(r, user, mid) {
		s.slotClosed(w, r, errSlotClosed)
		return
	}
	if _, err := s.admit(r.Context(), uid, mid); err != nil {
		s.slotClosed(w, r, errSlotClosed)
		return
	}
	// A slot that changes hands (another target, another key) starts clean: nothing the last one stored is left.
	if prev := s.slotCookie(r, host); prev == nil || prev.U != uid || prev.M != mid || prev.K != key {
		h.Set("Clear-Site-Data", `"cache", "storage"`)
	}
	ck := &http.Cookie{
		Name: s.slotCookieName(), Path: "/", HttpOnly: true, Secure: s.cfg.Secure, MaxAge: int(slotCookieTTL / time.Second),
		Value: s.signSlotCookie(slotClaims{U: uid, H: host, M: mid, K: key, E: s.now().Add(slotCookieTTL).Unix()}),
	}
	if s.cfg.Secure {
		// In the hub's frame the slot is a third-party context: the cookie needs None, and Partitioned keeps it to this frame.
		ck.SameSite, ck.Partitioned = http.SameSiteNoneMode, true
	} else {
		ck.SameSite = http.SameSiteLaxMode
	}
	http.SetCookie(w, ck)
	http.Redirect(w, r, "/m/"+mid+"/__pv/"+key+next, http.StatusFound)
}

var _ = strconv.Itoa
