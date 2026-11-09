package history

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dlclark/regexp2"

	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// Secrets are cut out of a chat before the hub stores it, with the rules the cluster's gateway cuts them with
// (the gateway's line redaction): the same token shapes, the same placeholders, the same
// order, read with JavaScript's meaning. Only exact token shapes, no entropy rule, which would also eat screenshots and
// thinking signatures. Every string of the event is redacted on its own, so an escaped quote stays one.

// jsSpace is JavaScript's \s as a class body, for regexp2.
const jsSpace = `\t\n\v\f\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff`

const notPlaceholder = `(?![` + jsSpace + `"']*\[скрыто)`

type secretRule struct {
	kind string
	re   *regexp2.Regexp
	keep bool
}

// withSpace writes \s as JavaScript's: the class body inside brackets, a class of its own outside them.
func withSpace(pattern, body string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '\\' && i+1 < len(pattern) && pattern[i+1] == 's':
			if in {
				b.WriteString(body)
			} else {
				b.WriteString("[" + body + "]")
			}
			i++
			continue
		case c == '\\' && i+1 < len(pattern):
			b.WriteByte(c)
			b.WriteByte(pattern[i+1])
			i++
			continue
		case c == '[':
			in = true
		case c == ']':
			in = false
		}
		b.WriteByte(c)
	}
	return b.String()
}

// jsRe compiles a JavaScript expression: \s as JavaScript's, \b and \w as ASCII.
func jsRe(pattern string, ignoreCase bool) *regexp2.Regexp {
	pattern = withSpace(pattern, jsSpace)
	pattern = strings.ReplaceAll(pattern, `\w`, `A-Za-z0-9_`)
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if i+1 < len(pattern) && pattern[i] == '\\' && pattern[i+1] == 'b' {
			// Every \b of the rules stands before a word's first character or after its last.
			if i+2 >= len(pattern) || pattern[i+2] == ')' {
				b.WriteString(`(?![A-Za-z0-9_])`)
			} else {
				b.WriteString(`(?<![A-Za-z0-9_])`)
			}
			i++
			continue
		}
		b.WriteByte(pattern[i])
	}
	opt := regexp2.RegexOptions(regexp2.ECMAScript)
	if ignoreCase {
		opt |= regexp2.IgnoreCase
	}
	re := regexp2.MustCompile(b.String(), opt)
	re.MatchTimeout = ruleTimeout
	return re
}

// \w inside a class is written [\w-] in the rules; jsRe turns \w into the class body, so the rules below keep \w only
// inside brackets.
var secretRules = []secretRule{
	{kind: "private-key", re: jsRe(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`, false)},
	{kind: "jwt", re: jsRe(`\beyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{10,}`, false)},
	{kind: "anthropic", re: jsRe(`\bsk-ant-[\w-]{20,}`, false)},
	{kind: "openai", re: jsRe(`\bsk-(?:proj-)?[\w-]{20,}`, false)},
	{kind: "github", re: jsRe(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[\w]{22,})`, false)},
	{kind: "gitlab", re: jsRe(`\bglpat-[\w-]{20,}`, false)},
	{kind: "youtrack", re: jsRe(`\bperm[:-][A-Za-z0-9=_+/-]{4,}\.[A-Za-z0-9=_+/-]{4,}\.[A-Za-z0-9=_+/-]{10,}`, false)},
	{kind: "vault", re: jsRe(`\bhvs\.[A-Za-z0-9_-]{24,}`, false)},
	{kind: "slack", re: jsRe(`\bxox[abprs]-[\w-]{10,}`, false)},
	{kind: "telegram", re: jsRe(`\b\d{8,10}:[\w-]{35}(?![\w-])`, false)},
	{kind: "aws", re: jsRe(`\bAKIA[0-9A-Z]{16}\b`, false)},
	{kind: "google", re: jsRe(`\bAIza[\w-]{35}`, false)},
	{kind: "bearer", re: jsRe(`(\bBearer\s+)`+notPlaceholder+`[A-Za-z0-9._~+/=-]{24,}`, false), keep: true},
	{kind: "url-credentials", re: jsRe(`(\bhttps?://[^\s:/@]+:)`+notPlaceholder+`[^\s@/]+(?=@)`, true), keep: true},
	{kind: "conn-string", re: jsRe(`(\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|rediss?|amqps?)://[^:\s/@]+:)`+notPlaceholder+`[^@\s]+(?=@)`, true), keep: true},
}

// secretPrefilter finds a superset of what the rules find, in linear time (RE2): the rules run only where it matches.
// Every alternative is at most as narrow as its rule: the credentials of an address may hold a quote, so the class
// of the address part takes everything the rules' classes take.
var secretPrefilter = regexp.MustCompile(withSpace(
	`-----BEGIN [A-Z ]*PRIVATE KEY|eyJ[\w-]{10,}\.|sk-ant-|sk-(?:proj-)?[\w-]{20}|gh[pousr]_|github_pat_|glpat-|perm[:-]|hvs\.|xox[abprs]-|\d{8,10}:[\w-]{35}|AKIA|AIza|Bearer\s|://[^\s:/@]+:[^\s@]+@`,
	jscompat.SpaceClass[1:len(jscompat.SpaceClass)-1]))

func secretPlaceholder(kind string) string { return "[скрыто: " + kind + "]" }

const (
	// ruleTimeout bounds one rule on one string; eventBudget all the rules on all the strings of an event. What runs out
	// of time is hidden whole: the copy fails closed, never with a secret left in it.
	ruleTimeout = 250 * time.Millisecond
	eventBudget = 2 * time.Second
	frameBudget = 10 * time.Second
	// timedOut is the kind of a string hidden whole for want of time.
	timedOut = "timeout"
)

func redactString(v string, found map[string]int, deadline time.Time) string {
	out := v
	for _, rule := range secretRules {
		if time.Now().After(deadline) {
			found[timedOut]++
			return secretPlaceholder(timedOut)
		}
		next, err := rule.re.ReplaceFunc(out, func(m regexp2.Match) string {
			found[rule.kind]++
			if rule.keep {
				if g := m.GroupByNumber(1); g != nil && len(g.Captures) > 0 {
					return g.String() + secretPlaceholder(rule.kind)
				}
			}
			return secretPlaceholder(rule.kind)
		}, -1, -1)
		if err != nil { // a rule that ran out of time: the string is not stored at all
			found[timedOut]++
			return secretPlaceholder(timedOut)
		}
		out = next
	}
	return out
}

func redactWalk(v any, found map[string]int, deadline time.Time) any {
	switch t := v.(type) {
	case string:
		// On the decoded string: an escape in the JSON (\u0042earer) hides nothing from the rules.
		if secretPrefilter.MatchString(t) {
			return redactString(t, found, deadline)
		}
		return t
	case []any:
		for i := range t {
			t[i] = redactWalk(t[i], found, deadline)
		}
		return t
	case map[string]any:
		// The keys too: a secret can be a key (a map of tokens). Two keys that become the same placeholder are one entry,
		// their values merged. In the keys' order, so the same event always comes out the same.
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(t))
		for _, k := range keys {
			item := redactWalk(t[k], found, deadline)
			if secretPrefilter.MatchString(k) {
				k = redactString(k, found, deadline)
			}
			if have, ok := out[k]; ok {
				item = mergeValues(have, item)
			}
			out[k] = item
		}
		return out
	}
	return v
}

// mergeValues is one value of two that came under the same key: objects merge key by key, a list takes the other's
// items, anything else becomes a list of both.
func mergeValues(a, b any) any {
	am, aok := a.(map[string]any)
	bm, bok := b.(map[string]any)
	if aok && bok {
		for k, v := range bm {
			if have, ok := am[k]; ok {
				v = mergeValues(have, v)
			}
			am[k] = v
		}
		return am
	}
	if al, ok := a.([]any); ok {
		if bl, ok := b.([]any); ok {
			return append(al, bl...)
		}
		return append(al, b)
	}
	if bl, ok := b.([]any); ok {
		return append([]any{a}, bl...)
	}
	return []any{a, b}
}

// RedactSecrets is the event with the secrets cut out of its strings, and which kinds were cut how many times; the
// event as it came when there is nothing to cut.
func RedactSecrets(raw json.RawMessage) (json.RawMessage, map[string]int) {
	return redactWithin(raw, time.Now().Add(eventBudget))
}

// redactEvents redacts a frame's events, ahead of the transaction that stores them, within frameBudget in all: past
// it, every string a rule could match is hidden whole (the prefilter itself is linear).
func redactEvents(events []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, len(events))
	frame := time.Now().Add(frameBudget)
	for i, ev := range events {
		out[i], _ = redactWithin(ev, minTime(time.Now().Add(eventBudget), frame))
	}
	return out
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func redactWithin(raw json.RawMessage, deadline time.Time) (json.RawMessage, map[string]int) {
	found := map[string]int{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		if !secretPrefilter.Match(raw) {
			return raw, found
		}
		return json.RawMessage(redactString(string(raw), found, deadline)), found
	}
	v = redactWalk(v, found, deadline)
	if len(found) == 0 {
		return raw, found
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return json.RawMessage(`{}`), map[string]int{timedOut: 1}
	}
	return json.RawMessage(bytes.TrimRight(b.Bytes(), "\n")), found
}
