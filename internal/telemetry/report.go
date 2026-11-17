package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The shapes Scrub masks, in this order: whatever names a person, a machine or a secret goes before it leaves.
var (
	scrubURL   = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s'"<>()\[\]{}]+`)
	scrubEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	scrubHome  = regexp.MustCompile(`(?:/home|/Users|/root|/var/home)/[^/\s'"]+|[A-Za-z]:\\Users\\[^\\\s'"]+`)
	scrubIP    = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}(?::\d+)?\b`)
	scrubToken = regexp.MustCompile(`[A-Za-z0-9_\-]{24,}|\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b|\b[0-9a-fA-F]{12,}\b`)
	scrubNum   = regexp.MustCompile(`\b\d{4,}\b`)
)

// MessageMax is the most runes a reported message keeps.
const MessageMax = 300

// Scrub makes an error's text fit to leave the machine: addresses, e-mails, home folders, IP addresses, ids and
// tokens, long numbers are masked, and the text is cut to MessageMax. What stays is the shape of the error, which is
// also what groups the same error from many machines into one.
func Scrub(s string) string {
	s = scrubURL.ReplaceAllString(s, "<url>")
	s = scrubEmail.ReplaceAllString(s, "<email>")
	s = scrubHome.ReplaceAllString(s, "~")
	s = scrubIP.ReplaceAllString(s, "<ip>")
	s = scrubToken.ReplaceAllString(s, "<id>")
	s = scrubNum.ReplaceAllString(s, "<n>")
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > MessageMax {
		s = string([]rune(s)[:MessageMax]) + "…"
	}
	return s
}

// Reporter sends errors as PostHog error tracking takes them ($exception): scrubbed, each distinct error once an hour,
// at most PerHour distinct errors an hour, so a loop on one machine costs nothing and floods nobody.
type Reporter struct {
	Queue   *Queue
	PerHour int
	Now     func() time.Time

	mu     sync.Mutex
	window time.Time
	seen   map[string]bool
}

// Fingerprint groups one error: where it was seen, its kind and its scrubbed text.
func Fingerprint(source, kind, message string) string {
	sum := sha256.Sum256([]byte(source + "\x00" + kind + "\x00" + message))
	return hex.EncodeToString(sum[:8])
}

// Error reports one error: source is where it was seen (web, runtime, app, …), kind what happened (exception, http,
// mcp.call, …), message its text (scrubbed here), props more technical properties. It says whether it was queued.
func (r *Reporter) Error(source, kind, message string, props *jsvalue.Object) bool {
	if r == nil || !r.Queue.Enabled() || !r.Queue.allowed() {
		return false
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	message = Scrub(message)
	if message == "" {
		message = kind
	}
	fp := Fingerprint(source, kind, message)
	per := r.PerHour
	if per <= 0 {
		per = 60
	}
	r.mu.Lock()
	if now.Sub(r.window) >= time.Hour || r.seen == nil {
		r.window, r.seen = now, map[string]bool{}
	}
	if r.seen[fp] || len(r.seen) >= per {
		r.mu.Unlock()
		return false
	}
	r.seen[fp] = true
	r.mu.Unlock()

	p := jsvalue.NewObject()
	if props != nil {
		p.Assign(props)
	}
	p.Set("$exception_list", []any{jsvalue.NewObject("type", source+"."+kind, "value", message,
		"mechanism", jsvalue.NewObject("handled", true, "synthetic", false))})
	p.Set("$exception_level", "error")
	p.Set("$exception_fingerprint", fp)
	p.Set("source", source)
	p.Set("kind", kind)
	r.Queue.Push(Event{Name: "$exception", At: now, Properties: p})
	return true
}
