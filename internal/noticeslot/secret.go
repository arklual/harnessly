package noticeslot

import (
	"math"
	"regexp"
	"strings"
)

// The slot never carries secrets: a password, a key or a sign-in goes through the safe and the site sign-in, which
// keep it out of the chat, the agent's context and the logs. The slot has no password field in any kind, and a text
// that looks like a key is refused both ways: in what a caller asks to show and in what a person types back.

// keyPrefixes start the keys and tokens people paste most (OpenAI, Anthropic, GitHub, GitLab, Slack, AWS, Google,
// Stripe, npm, PyPI, Telegram bots are matched by shape below).
var keyPrefixes = []string{
	"sk-", "sk_live_", "sk_test_", "rk_live_", "pk_live_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
	"glpat-", "gldt-", "xoxb-", "xoxp-", "xoxa-", "xoxs-", "akia", "asia", "aiza", "ya29.", "npm_", "pypi-",
	"-----begin",
}

var (
	jwtShape      = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}$`)
	botTokenShape = regexp.MustCompile(`^[0-9]{8,10}:[A-Za-z0-9_-]{35}$`)
	// passwordField is a password input in a custom view: the slot has none, so a view that brings one is refused.
	passwordField = regexp.MustCompile(`(?i)(type\s*=\s*["']?password|autocomplete\s*=\s*["']?(current|new)-password|` +
		`-webkit-text-security|\bpasswordrules\s*=)`)
)

// LooksSecret tells a text that is, or holds, a key, a token or a private key: a known prefix, a JWT, a bot token, or
// one long random word (at least 24 characters of letters and digits with no spaces, mixed and high in entropy).
func LooksSecret(s string) bool {
	for _, word := range strings.Fields(s) {
		w := strings.Trim(word, `"'.,;()[]{}<>`)
		low := strings.ToLower(w)
		for _, p := range keyPrefixes {
			if strings.HasPrefix(low, p) && len(w) >= len(p)+8 {
				return true
			}
		}
		if jwtShape.MatchString(w) || botTokenShape.MatchString(w) || randomWord(w) {
			return true
		}
	}
	return strings.Contains(strings.ToLower(s), "-----begin")
}

// randomWord: long, no spaces, letters and digits mixed, and close to random (Shannon entropy per character).
func randomWord(w string) bool {
	if len(w) < 24 || strings.Contains(w, "://") || strings.Contains(w, "/") && strings.Count(w, "/") > 1 {
		return false
	}
	var letters, digits int
	counts := map[rune]int{}
	for _, r := range w {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			letters++
		case r == '-' || r == '_' || r == '+' || r == '=' || r == '/':
		default:
			return false
		}
		counts[r]++
	}
	if letters == 0 || digits == 0 || hexOnly(w) || wordsOnly(w) {
		return false
	}
	n := float64(len(w))
	var bits float64
	for _, c := range counts {
		p := float64(c) / n
		bits -= p * math.Log2(p)
	}
	return bits >= 3.6
}

// wordsOnly: words and numbers joined by _ or - (a table, a branch, a file name), not letters and digits mixed inside one
// piece as a random key has them.
func wordsOnly(w string) bool {
	for _, piece := range strings.FieldsFunc(w, func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		var hasLetter, hasDigit bool
		for _, r := range piece {
			hasDigit = hasDigit || r >= '0' && r <= '9'
			hasLetter = hasLetter || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		}
		if hasLetter && hasDigit {
			return false
		}
	}
	return true
}

// hexOnly: a commit hash or a UUID, which people and agents write in plain text all the time.
func hexOnly(w string) bool {
	for _, r := range w {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r == '-') {
			return false
		}
	}
	return true
}

// viewActive is what a custom view may not hold besides layout and buttons: code, a field, a form, a frame, a link out.
var viewActive = regexp.MustCompile(`(?i)(<\s*(script|style|input|textarea|select|form|iframe|frame|object|embed|link|meta|base|a|svg|math|template)[\s/>]|\son[a-z]+\s*=|javascript:)`)

// ViewIsActive tells a custom view that holds code, a field, a form, a frame or a link: a view is plain layout and
// buttons that answer with data-answer, and the page draws nothing else.
func ViewIsActive(html string) bool { return viewActive.MatchString(html) }

// HasPasswordField tells a custom view that brings a password input of its own.
func HasPasswordField(html string) bool { return passwordField.MatchString(html) }

// SecretWords are the words a field must not ask for: its title, text or placeholder naming a password, a key or a
// sign-in turns the call away (the safe asks for those).
var secretWords = regexp.MustCompile(`(?i)(парол|password|passwd|passphrase|api[ -_]?key|secret|токен|token|` +
	`ключ (api|доступа)|private key|seed phrase|логин и пароль|войдите|sign[ -]?in|log[ -]?in)`)

// AsksForSecret tells a field or code whose wording asks for a password, a key or a sign-in.
func AsksForSecret(parts ...string) bool {
	for _, p := range parts {
		if secretWords.MatchString(p) {
			return true
		}
	}
	return false
}
