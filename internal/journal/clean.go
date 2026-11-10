package journal

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

// The longest a field is, in characters.
const (
	maxText  = 600
	maxURL   = 300
	maxShort = 120
)

var (
	// key=value pairs whose value is a secret, in a text or a cookie.
	secretPair = regexp.MustCompile(`(?i)\b(access_token|refresh_token|id_token|token|api[_-]?key|secret|client_secret|password|passwd|pwd|authorization|otp)(["']?\s*[=:]\s*["']?)[^\s&"',;]+`)
	// Names that are secrets only as a query's or a cookie's key=value ("code: 401" is a status).
	secretQuery = regexp.MustCompile(`(?i)\b(key|auth|session|sid|code|signature|sig|nonce|state)=[^\s&"',;]+`)
	bearer      = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`)
	// A run that looks like a key or a token: 32 and more of base64url or hex.
	longToken = regexp.MustCompile(`[A-Za-z0-9_\-+=]{32,}`)
	// Well-known key prefixes.
	knownKey = regexp.MustCompile(`\b(sk-[A-Za-z0-9_-]{8,}|gh[pousr]_[A-Za-z0-9]{8,}|glpat-[A-Za-z0-9_-]{8,}|xox[abpr]-[A-Za-z0-9-]{8,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9._-]+)`)
	// A path segment that is a session's token: long, of a token's alphabet; a signed one (a JWT, a gateway's carried
	// session) has dots and is longer still.
	tokenSegment  = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)
	signedSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_.-]{39,}$`)
)

// Redact hides what looks like a secret in a text: key=value pairs of secret names, bearer tokens, known key prefixes
// and long token-like runs.
func Redact(s string) string {
	s = knownKey.ReplaceAllString(s, "…")
	s = bearer.ReplaceAllString(s, "$1 …")
	s = secretPair.ReplaceAllString(s, "$1$2…")
	s = secretQuery.ReplaceAllString(s, "$1=…")
	s = longToken.ReplaceAllStringFunc(s, func(m string) string {
		// A path or a file name with no digit is words, not a token.
		if !strings.ContainsAny(m, "0123456789") {
			return m
		}
		return "…"
	})
	return s
}

// CleanURL is an address as the journal keeps it: no query, no fragment, no user info, a token in the path hidden.
func CleanURL(raw string) string {
	s := raw
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if scheme, rest, ok := strings.Cut(s, "://"); ok {
		if at := strings.Index(rest, "@"); at >= 0 && (!strings.Contains(rest, "/") || at < strings.Index(rest, "/")) {
			rest = rest[at+1:]
		}
		s = scheme + "://" + rest
	}
	parts := strings.Split(s, "/")
	for i, p := range parts {
		if (tokenSegment.MatchString(p) || signedSegment.MatchString(p)) && randomLooking(p) {
			parts[i] = "…"
		}
	}
	return clip(plain(strings.Join(parts, "/")), maxURL)
}

func plain(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s))
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func short(s string) string { return clip(plain(s), maxShort) }

// clean is a line as it is written: known level, time set, every field one plain line, secrets hidden, bounded.
func clean(e Entry, now time.Time) Entry {
	switch e.Level {
	case Error, Warn, Info:
	default:
		e.Level = Info
	}
	if e.At <= 0 || e.At > now.UnixMilli()+60_000 {
		e.At = now.UnixMilli()
	}
	e.Source, e.Event = short(e.Source), short(e.Event)
	e.Trace, e.Chat, e.App, e.View = short(e.Trace), short(e.Chat), short(e.App), short(e.View)
	e.Tool, e.Method = short(e.Tool), short(e.Method)
	if e.URL != "" {
		e.URL = CleanURL(e.URL)
	}
	if e.Text != "" {
		e.Text = clip(Redact(plain(e.Text)), maxText)
	}
	if e.Status < 0 || e.Status > 999 {
		e.Status = 0
	}
	if e.Dur < 0 {
		e.Dur = 0
	}
	return e
}

// randomLooking is a segment that reads as a key rather than a word: a digit in it, or both cases (a session's token in
// base64url without a digit is still mixed-case), never a lowercase-hyphenated name such as "conversation-history".
func randomLooking(s string) bool {
	return strings.ContainsAny(s, "0123456789") ||
		strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyz") && strings.ContainsAny(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
}
