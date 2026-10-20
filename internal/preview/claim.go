// Package preview is the reverse proxy behind the right pane's browser: a slot origin of the gateway shows a local dev
// server port (with its neighbour ports) or an external site, named by a claim the gateway signs with the pair's
// secret. It rewrites what a dev server needs rewritten to work inside the slot origin, injects the page's bridge,
// passes WebSockets (HMR) through, refuses private addresses, and records what the page did for its DevTools.
package preview

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
	"github.com/ly-solutions/harnessly/internal/preview/weburl"
)

// Claim is what the gateway says a request may reach.
type Claim struct {
	Sub        string
	Target     devtools.Target
	SlotOrigin string
	PortSlots  map[string]string
	// ParentOrigin is the platform's page origin the in-page bridge talks to; "" when the gateway sent none, and then
	// there is no bridge.
	ParentOrigin string
}

// CookieTag names a target in its cookies on the slot host, so targets taking turns in a slot never share cookies.
// Keyed with the secret: a page cannot compute another target's tag.
func CookieTag(t devtools.Target, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	if t.Kind == "port" {
		mac.Write([]byte("port:" + strconv.Itoa(t.Port)))
	} else {
		mac.Write([]byte(t.Origin))
	}
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

var (
	slotOriginRe   = regexp.MustCompile(`^https://[a-z0-9-]+(\.[a-z0-9-]+)+$`)
	parentOriginRe = regexp.MustCompile(`^https://[a-z0-9-]+(\.[a-z0-9-]+)*(:\d{1,5})?$`)
	targetOriginRe = regexp.MustCompile(`^https?://(?:[a-z0-9_-]+(?:\.[a-z0-9_-]+)*|\[[0-9a-f:.]+\])(?::\d{1,5})?$`)
)

func isPort(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f < 1 || f > 65535 {
		return 0, false
	}
	return int(f), true
}

// isTargetOrigin: exactly the origin the URL parser would print, so no other spelling of a host reaches the proxy.
func isTargetOrigin(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || !targetOriginRe.MatchString(s) {
		return "", false
	}
	u, ok := weburl.Parse(s)
	return s, ok && u.Origin() == s && u.Port != "0"
}

func parseTarget(v any) (devtools.Target, bool) {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return devtools.Target{}, false
	}
	kind, _ := o.Get("kind")
	switch kind {
	case "port":
		p, _ := o.Get("port")
		if port, ok := isPort(p); ok {
			return devtools.Target{Kind: "port", Port: port}, true
		}
	case "url":
		raw, _ := o.Get("origin")
		if origin, ok := isTargetOrigin(raw); ok {
			return devtools.Target{Kind: "url", Origin: origin}, true
		}
	}
	return devtools.Target{}, false
}

func portSlotsOf(v any, t devtools.Target, slot string) (map[string]string, bool) {
	if list, isList := v.([]any); isList {
		return map[string]string{}, t.Kind == "url" && len(list) == 0
	}
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return nil, false
	}
	if t.Kind == "url" {
		return map[string]string{}, o.Len() == 0
	}
	if main, _ := o.Get(strconv.Itoa(t.Port)); main != slot {
		return nil, false
	}
	out := map[string]string{}
	for _, k := range o.Keys() {
		val, _ := o.Get(k)
		n, err := strconv.Atoi(k)
		if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != k {
			return nil, false
		}
		s, isString := val.(string)
		if n != t.Port && (!isString || s != slot+devtools.NeighbourPrefix+k) {
			return nil, false
		}
		out[k], _ = val.(string)
	}
	return out, true
}

func parseClaim(payload *jsvalue.Object) (*Claim, bool) {
	sub, ok := field(payload, "sub").(string)
	if !ok || sub == "" {
		return nil, false
	}
	target, ok := parseTarget(field(payload, "target"))
	if !ok {
		return nil, false
	}
	slot, ok := field(payload, "slotOrigin").(string)
	if !ok || !slotOriginRe.MatchString(slot) {
		return nil, false
	}
	slots, ok := portSlotsOf(field(payload, "portSlots"), target, slot)
	if !ok {
		return nil, false
	}
	c := &Claim{Sub: sub, Target: target, SlotOrigin: slot, PortSlots: slots}
	if raw, present := payload.Get("parentOrigin"); present {
		parent, ok := raw.(string)
		if !ok || !parentOriginRe.MatchString(parent) {
			return nil, false
		}
		c.ParentOrigin = parent
	}
	return c, true
}

func field(o *jsvalue.Object, k string) any {
	v, _ := o.Get(k)
	return v
}

// VerifyClaim checks an HS256 claim token with the audience; nil when it does not hold.
func VerifyClaim(token string, secret []byte, audience string, now time.Time) *Claim {
	if token == "" || len(secret) == 0 {
		return nil
	}
	if agent.VerifyHS256(secret, token, audience, now) != nil {
		return nil
	}
	parts := strings.Split(token, ".")
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	raw, err := jsvalue.Parse(body)
	if err != nil {
		return nil
	}
	obj, ok := raw.(*jsvalue.Object)
	if !ok {
		return nil
	}
	c, ok := parseClaim(obj)
	if !ok {
		return nil
	}
	return c
}
