package share

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/dlclark/regexp2"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// Secret and path redaction of a snapshot. It runs when a snapshot is captured and again on every render, so one pass's
// output passes the next unchanged: placeholders match no rule, and the rules that keep a key and hide its value skip a
// value that is already a placeholder. Regular expressions are the previous runtime's, read with JavaScript's meaning
// of \s, ".", ^ and $.

// placeholder is what replaces a hidden value, in the runtime's language. The web page builds the same text
// (hl-share-redact.ts), so the two stay byte for byte equal per language.
func placeholder(rule string) string {
	return locale.Pick("[hidden: "+rule+"]", "[скрыто: "+rule+"]")
}

// JavaScript's \s as a class body, "." and the multiline anchors, for regexp2 in ECMAScript mode.
const (
	jsSpace = `\t\n\v\f\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff`
	jsDot   = `[^\n\r\u2028\u2029]`
	jsLineA = `(?<![^\n\r\u2028\u2029])`
	jsLineZ = `(?![^\n\r\u2028\u2029])`
	// notPlace skips a value that is already a placeholder. Both languages are recognised, so a snapshot taken by a
	// Russian runtime passes an English one unchanged and the other way round.
	notPlace = `(?![` + jsSpace + `"']*\[(?:скрыто|hidden: ))`
)

type patternRule struct {
	id string
	re *regexp2.Regexp
	// keep: the first group stays visible before the placeholder.
	keep bool
	// need is what a text must contain for the rule to match (a fast substring test, on the text or its ASCII lower
	// case); worth then finds a superset of the rule's matches in linear time (RE2). The backtracking expression runs
	// only on a text both pass.
	need  func(s string, lower func() string) bool
	worth *regexp.Regexp
}

func containsAny(subs ...string) func(string, func() string) bool {
	return func(s string, _ func() string) bool {
		for _, sub := range subs {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
}

func anyLower(subs ...string) func(string, func() string) bool {
	return func(_ string, lower func() string) bool {
		l := lower()
		for _, sub := range subs {
			if strings.Contains(l, sub) {
				return true
			}
		}
		return false
	}
}

func all(fs ...func(string, func() string) bool) func(string, func() string) bool {
	return func(s string, lower func() string) bool {
		for _, f := range fs {
			if !f(s, lower) {
				return false
			}
		}
		return true
	}
}

// asciiLower lowers ASCII letters only, byte for byte, so offsets stay.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// telegramShape: eight to ten digits right before a colon somewhere.
func telegramShape(s string, _ func() string) bool {
	for i := strings.IndexByte(s, ':'); i >= 0; {
		n := 0
		for j := i - 1; j >= 0 && s[j] >= '0' && s[j] <= '9'; j-- {
			n++
		}
		if n >= 8 {
			return true
		}
		k := strings.IndexByte(s[i+1:], ':')
		if k < 0 {
			break
		}
		i += 1 + k
	}
	return false
}

// mustRe compiles a pattern; \b in it is JavaScript's (ASCII word characters), which regexp2 does not give: a \b
// before a word character becomes "not after one", after a word character "not before one".
func mustRe(pattern string, ignoreCase bool) *regexp2.Regexp {
	pattern = strings.ReplaceAll(pattern, `\b`, "\x00")
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != 0 {
			b.WriteByte(pattern[i])
			continue
		}
		if i == len(pattern)-1 || pattern[i+1] == ')' {
			b.WriteString(`(?![A-Za-z0-9_])`)
		} else {
			b.WriteString(`(?<![A-Za-z0-9_])`)
		}
	}
	pattern = b.String()
	opt := regexp2.RegexOptions(regexp2.ECMAScript)
	if ignoreCase {
		opt |= regexp2.IgnoreCase
	}
	return regexp2.MustCompile(pattern, opt)
}

// sp is JavaScript's \s for the RE2 prefilters.
const sp = jscompat.SpaceClass

func pre(p string) *regexp.Regexp { return regexp.MustCompile(p) }

// patternRules in order: specific shapes first, so a GitHub token inside TOKEN= is counted as GitHub.
var patternRules = []patternRule{
	{id: "private-key", need: containsAny("PRIVATE KEY-----"), re: mustRe(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`, false), worth: pre(`PRIVATE KEY-----`)},
	{id: "jwt", need: containsAny("eyJ"), re: mustRe(`\beyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{10,}`, false), worth: pre(`eyJ[\w-]{10,}\.[\w-]{10,}\.[\w-]{10,}`)},
	{id: "anthropic", need: containsAny("sk-ant-"), re: mustRe(`\bsk-ant-[\w-]{20,}`, false), worth: pre(`sk-ant-[\w-]{20,}`)},
	{id: "openai", need: containsAny("sk-"), re: mustRe(`\bsk-(?:proj-)?[\w-]{20,}`, false), worth: pre(`sk-[\w-]{20,}`)},
	{id: "github", need: containsAny("ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_"), re: mustRe(`\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_\w{22,})`, false), worth: pre(`gh[pousr]_[A-Za-z0-9]{36,}|github_pat_\w{22,}`)},
	{id: "gitlab", need: containsAny("glpat-"), re: mustRe(`\bglpat-[\w-]{20,}`, false), worth: pre(`glpat-[\w-]{20,}`)},
	{id: "slack", need: containsAny("xox"), re: mustRe(`\bxox[abprs]-[\w-]{10,}`, false), worth: pre(`xox[abprs]-[\w-]{10,}`)},
	{id: "telegram", need: telegramShape, re: mustRe(`\b\d{8,10}:[\w-]{35}(?![\w-])`, false), worth: pre(`\d{8,10}:[\w-]{35}`)},
	{id: "aws", need: containsAny("AKIA"), re: mustRe(`\bAKIA[0-9A-Z]{16}\b`, false), worth: pre(`AKIA[0-9A-Z]{16}`)},
	{id: "aws", need: anyLower("aws_secret_access_key"), re: mustRe(`(aws_secret_access_key["']?[`+jsSpace+`]*[:=][`+jsSpace+`]*["']?)`+notPlace+`[A-Za-z0-9/+=]{40}`, true), keep: true,
		worth: pre(`(?i)aws_secret_access_key["']?` + sp + `*[:=]` + sp + `*["']?[A-Za-z0-9/+=]{40}`)},
	{id: "google", need: containsAny("AIza"), re: mustRe(`\bAIza[\w-]{35}`, false), worth: pre(`AIza[\w-]{35}`)},
	{id: "yandex", need: containsAny("t1.", "AQVN", "y0_"), re: mustRe(`\b(?:t1\.[\w-]+=*\.[\w-]{86}=*|AQVN[\w-]{35,38}|y0_[\w-]{50,})`, false), worth: pre(`t1\.[\w-]+=*\.[\w-]{86}|AQVN[\w-]{35}|y0_[\w-]{50}`)},
	{id: "bearer", need: anyLower("authorization"), re: mustRe(`(Authorization["']?[`+jsSpace+`]*:[`+jsSpace+`]*["']?(?:Bearer|Basic|Token)[`+jsSpace+`]+)`+notPlace+`[^`+jsSpace+`"']+`, true), keep: true,
		worth: pre(`(?i)authorization["']?` + sp + `*:` + sp + `*["']?(?:bearer|basic|token)` + sp + `+`)},
	{id: "bearer", need: containsAny("Bearer"), re: mustRe(`(\bBearer[`+jsSpace+`]+)`+notPlace+`[A-Za-z0-9._~+/=-]{16,}`, false), keep: true, worth: pre(`Bearer` + sp + `+`)},
	{id: "cookie", need: anyLower("cookie:"), re: mustRe(jsLineA+`([`+jsSpace+`]*(?:Set-)?Cookie:[`+jsSpace+`]*)`+notPlace+jsDot+`+`+jsLineZ, true), keep: true, worth: pre(`(?i)cookie:`)},
	{id: "conn-string", need: all(containsAny("://"), containsAny("@")), re: mustRe(`(\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|rediss?|amqps?)://[^:`+jsSpace+`/@]+:)`+notPlace+`[^@`+jsSpace+`]+(?=@)`, true), keep: true,
		worth: pre(`(?i)(?:postgres|mysql|mongodb|redis|amqp)[a-z+]*://[^/@]+:[^@]+@`)},
	{id: "url-credentials", need: all(containsAny("://"), containsAny("@")), re: mustRe(`(\bhttps?://[^`+jsSpace+`:/@]+:)`+notPlace+`[^`+jsSpace+`@/]+(?=@)`, true), keep: true, worth: pre(`(?i)https?://[^/@]+:[^@/]+@`)},
	{id: "env-assignment", need: all(containsAny("="), containsAny("KEY", "SECRET", "TOKEN", "PASS", "DSN")), re: mustRe(jsLineA+`([`+jsSpace+`]*(?:export[`+jsSpace+`]+)?[A-Z0-9_]*(?:KEY|SECRET|TOKEN|PASSWORD|PASS|DSN)[A-Z0-9_]*=)`+notPlace+
		`(?=[^`+jsSpace+`])`+jsDot+`+`+jsLineZ, false), keep: true, worth: pre(`(?:KEY|SECRET|TOKEN|PASS|DSN)[A-Z0-9_]*=`)},
	{id: "kv-secret", need: all(containsAny(":", "="), anyLower("pass", "secret", "token", "key")), re: mustRe(`(\b(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|client[_-]?secret)["']?[`+jsSpace+`]*[:=][`+jsSpace+`]*["']?)`+notPlace+
		`[^`+jsSpace+`"',;]{6,}`, true), keep: true, worth: pre(`(?i)(?:passw(?:or)?d|secret|token|api[_-]?key|access[_-]?key)["']?` + sp + `*[:=]`)},
}

// Redactor hides the secrets and shortens the paths of the strings of one snapshot, counting what it did.
type Redactor struct {
	knownSecrets []string
	workspaceDir string
	homeDirs     []string
	profileHomes string

	pathsShortened int
	redactions     *jsvalue.Object
}

// RedactorConfig is what a redactor knows of this runtime.
type RedactorConfig struct {
	// KnownSecrets are exact secret values (the pair's secret, the cli-token, secret-looking environment values).
	KnownSecrets []string
	// HomeDirs are shortened to "~"; /home/<user> and /root always are.
	HomeDirs []string
	// WorkspaceDir is dropped from the front of paths ("" for none).
	WorkspaceDir string
	// ProfileHomes is the folder of per-person profiles whose <name>/home is a home too ("" for none).
	ProfileHomes string
}

// NewRedactor is a redactor with fresh counters.
func NewRedactor(c RedactorConfig) *Redactor {
	r := &Redactor{redactions: jsvalue.NewObject(), profileHomes: strings.TrimRight(c.ProfileHomes, "/")}
	seen := map[string]bool{}
	for _, s := range c.KnownSecrets {
		if jscompat.Len(s) >= 8 && !seen[s] {
			seen[s] = true
			r.knownSecrets = append(r.knownSecrets, s)
		}
	}
	sort.SliceStable(r.knownSecrets, func(i, j int) bool { return jscompat.Len(r.knownSecrets[i]) > jscompat.Len(r.knownSecrets[j]) })
	r.workspaceDir = strings.TrimRight(c.WorkspaceDir, "/")
	seenHome := map[string]bool{}
	for _, d := range c.HomeDirs {
		d = strings.TrimRight(d, "/")
		if jscompat.Len(d) > 1 && !seenHome[d] {
			seenHome[d] = true
			r.homeDirs = append(r.homeDirs, d)
		}
	}
	sort.SliceStable(r.homeDirs, func(i, j int) bool { return jscompat.Len(r.homeDirs[i]) > jscompat.Len(r.homeDirs[j]) })
	return r
}

func (r *Redactor) count(rule string) {
	n, _ := r.redactions.Number(rule)
	r.redactions.Set(rule, n+1)
}

// Report is {pathsShortened, redactions} so far.
func (r *Redactor) Report() *jsvalue.Object {
	return jsvalue.NewObject("pathsShortened", r.pathsShortened, "redactions", r.redactions.Copy())
}

func isBidi(r rune) bool { return r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069 }

// Redact is one string with its secrets hidden and its paths shortened.
func (r *Redactor) Redact(text string) string {
	if text == "" {
		return text
	}
	if strings.ContainsFunc(text, isBidi) {
		text = strings.Map(func(c rune) rune {
			if isBidi(c) {
				return -1
			}
			return c
		}, text)
	}
	if len(r.knownSecrets) > 0 {
		text = r.hideKnown(text)
	}
	lowered := ""
	lowerOf := ""
	lower := func() string {
		if lowerOf != text || lowered == "" {
			lowered, lowerOf = asciiLower(text), text
		}
		return lowered
	}
	for _, rule := range patternRules {
		if !rule.need(text, lower) {
			continue
		}
		text = r.applyRule(rule, text)
	}
	text = r.shortenPaths(text)
	return r.hideHighEntropy(text)
}

// applyRule replaces a rule's matches. Each match holds a match of the rule's linear prefilter, and starts on the line
// that one starts on (only whitespace runs cross a line feed, a few at most), so the backtracking expression runs only
// on windows from that line's start to a few lines past the prefilter's match. At the edges of a window a line feed
// reads to every rule as the start or end of the text does.
func (r *Redactor) applyRule(rule patternRule, text string) string {
	replace := func(s string) string {
		out, err := rule.re.ReplaceFunc(s, func(m regexp2.Match) string {
			r.count(rule.id)
			if rule.keep {
				if g := m.GroupByNumber(1); g != nil && len(g.Captures) > 0 {
					return g.String() + placeholder(rule.id)
				}
			}
			return placeholder(rule.id)
		}, -1, -1)
		if err != nil {
			return s
		}
		return out
	}
	found := rule.worth.FindAllStringIndex(text, -1)
	if len(found) == 0 {
		return text
	}
	if rule.id == "private-key" {
		return replace(text)
	}
	const extraLines = 4
	var b strings.Builder
	done := 0
	for i := 0; i < len(found); {
		start := strings.LastIndexByte(text[:found[i][0]], '\n') + 1
		end := found[i][1]
		for {
			end = lineEndAfter(text, end, extraLines)
			i++
			if i >= len(found) || found[i][0] >= end {
				break
			}
			end = max(end, found[i][1])
		}
		start = max(start, done)
		b.WriteString(text[done:start])
		b.WriteString(replace(text[start:end]))
		done = end
	}
	b.WriteString(text[done:])
	return b.String()
}

// lineEndAfter is where the n-th line ending at or after i is (exclusive of it), or the end of the text.
func lineEndAfter(text string, i, n int) int {
	for ; n > 0; n-- {
		j := strings.IndexByte(text[i:], '\n')
		if j < 0 {
			return len(text)
		}
		i += j
		if n > 1 {
			i++
		}
	}
	return i
}

// hideKnown replaces this runtime's exact secrets, the longest first where two start at one place.
func (r *Redactor) hideKnown(text string) string {
	var b strings.Builder
	i := 0
	for {
		best, at := -1, -1
		for k, s := range r.knownSecrets {
			if j := strings.Index(text[i:], s); j >= 0 && (at < 0 || i+j < at) {
				best, at = k, i+j
			}
		}
		if best < 0 {
			break
		}
		b.WriteString(text[i:at])
		b.WriteString(placeholder("known-secret"))
		r.count("known-secret")
		i = at + len(r.knownSecrets[best])
	}
	if i == 0 {
		return text
	}
	b.WriteString(text[i:])
	return b.String()
}

func isPathPrevByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '~' || c == '-'
}

// pathStart: not inside a longer path or word, nor the host of "scheme://".
func pathStart(s string, i int) bool {
	if i > 0 && isPathPrevByte(s[i-1]) {
		return false
	}
	return !(i >= 2 && s[i-1] == '/' && s[i-2] == ':')
}

func isNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-'
}

// homeEnd: the prefix ends at a "/", the end, or a character that cannot continue a name.
func homeEnd(s string, i int) bool { return i >= len(s) || s[i] == '/' || !isNameByte(s[i]) }

// replaceAll replaces, left to right without overlap, every match the finder reports at a position holding first.
func (r *Redactor) replaceAll(s string, first byte, match func(s string, i int) int, with string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] != first {
			continue
		}
		if n := match(s, i); n > 0 {
			b.WriteString(s[last:i])
			b.WriteString(with)
			r.pathsShortened++
			last = i + n
			i = last - 1
		}
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func (r *Redactor) shortenPaths(text string) string {
	if r.workspaceDir != "" {
		dir := r.workspaceDir + "/"
		text = r.replaceAll(text, dir[0], func(s string, i int) int {
			if strings.HasPrefix(s[i:], dir) && pathStart(s, i) {
				return len(dir)
			}
			return 0
		}, "")
	}
	literal := func(dir string) func(string, int) int {
		return func(s string, i int) int {
			if strings.HasPrefix(s[i:], dir) && pathStart(s, i) && homeEnd(s, i+len(dir)) {
				return len(dir)
			}
			return 0
		}
	}
	named := func(prefix, suffix string) func(string, int) int {
		return func(s string, i int) int {
			if !strings.HasPrefix(s[i:], prefix) || !pathStart(s, i) {
				return 0
			}
			j := i + len(prefix)
			k := j
			for k < len(s) && isNameByte(s[k]) {
				k++
			}
			if k == j {
				return 0
			}
			// The name runs to a character that cannot continue it, so only the suffix can follow it.
			if suffix == "" {
				return k - i
			}
			if strings.HasPrefix(s[k:], suffix) && homeEnd(s, k+len(suffix)) {
				return k + len(suffix) - i
			}
			return 0
		}
	}
	for _, d := range r.homeDirs {
		text = r.replaceAll(text, d[0], literal(d), "~")
	}
	if r.profileHomes != "" {
		text = r.replaceAll(text, r.profileHomes[0], named(r.profileHomes+"/", "/home"), "~")
	}
	text = r.replaceAll(text, '/', named("/home/", ""), "~")
	return r.replaceAll(text, '/', literal("/root"), "~")
}

func isEntropyByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '+' || c == '/' || c == '_' || c == '=' || c == '-'
}

func (r *Redactor) hideHighEntropy(text string) string {
	var b strings.Builder
	last := 0
	for i := 0; i < len(text); {
		if !isEntropyByte(text[i]) {
			i++
			continue
		}
		j := i
		for j < len(text) && isEntropyByte(text[j]) {
			j++
		}
		if j-i >= 32 && looksLikeSecret(text[i:j]) {
			b.WriteString(text[last:i])
			b.WriteString(placeholder("high-entropy"))
			r.count("high-entropy")
			last = j
		}
		i = j
	}
	if last == 0 {
		return text
	}
	b.WriteString(text[last:])
	return b.String()
}

func looksLikeSecret(v string) bool {
	if strings.Trim(v, "0123456789abcdefABCDEF-") == "" {
		return false
	}
	dense := false
	for i := 0; i < len(v); {
		if !isAlnumByte(v[i]) {
			i++
			continue
		}
		j := i
		letter, digit := false, false
		for j < len(v) && isAlnumByte(v[j]) {
			if v[j] >= '0' && v[j] <= '9' {
				digit = true
			} else {
				letter = true
			}
			j++
		}
		if j-i >= 16 && letter && digit {
			dense = true
		}
		i = j
	}
	if !dense {
		return false
	}
	if strings.Contains(v, "/") {
		all := true
		for _, part := range strings.Split(v, "/") {
			for k := 0; k < len(part); k++ {
				c := part[k]
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
					all = false
				}
			}
		}
		if all {
			return false
		}
	}
	return shannonEntropy(v) >= 4.2
}

func isAlnumByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func shannonEntropy(v string) float64 {
	counts := map[byte]int{}
	for i := 0; i < len(v); i++ {
		counts[v[i]]++
	}
	bits := 0.0
	for _, c := range counts {
		p := float64(c) / float64(len(v))
		bits -= p * math.Log2(p)
	}
	return bits
}

// EnvSecrets are the exact secret values in an environment: values of eight or more characters, without whitespace and
// not a path, of variables whose names look secret (or the named auth variable).
func EnvSecrets(environ []string, authVar string) []string {
	var out []string
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || jscompat.Len(value) < 8 {
			continue
		}
		upper := strings.ToUpper(name)
		if name != authVar && !strings.Contains(upper, "KEY") && !strings.Contains(upper, "SECRET") && !strings.Contains(upper, "TOKEN") &&
			!strings.Contains(upper, "PASSWORD") {
			continue
		}
		if strings.HasPrefix(value, "/") || strings.ContainsFunc(value, jscompat.IsSpace) {
			continue
		}
		out = append(out, value)
	}
	return out
}
