// Package terminalfailure normalizes provider failures that a coding-agent CLI cannot recover from by retrying.
package terminalfailure

import (
	"regexp"
	"strings"
)

type Kind string

const QuotaExhausted Kind = "quota_exhausted"

type Failure struct {
	Kind        Kind
	Detail      string
	Terminal    bool
	NeedsTarget bool
}

type Structured struct {
	Status     int
	Code       string
	Transient  *bool
	RetryAfter string
	Detail     string
}

var terminalCodes = map[string]bool{
	"exceeded_current_quota_error": true,
	"insufficient_quota":           true,
	"quota_exhausted":              true,
}

func Classify(in Structured) (Failure, bool) {
	code := strings.ToLower(strings.TrimSpace(in.Code))
	if in.RetryAfter != "" || in.Transient != nil && *in.Transient {
		return Failure{}, false
	}
	terminal := terminalCodes[code] || in.Status == 429 && code == "rate_limit" && in.Transient != nil && !*in.Transient
	if !terminal {
		return Failure{}, false
	}
	return Failure{Kind: QuotaExhausted, Detail: in.Detail, Terminal: true, NeedsTarget: true}, true
}

const tailLimit = 16 << 10

var (
	ansiRE             = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)
	spacesRE           = regexp.MustCompile(`\s+`)
	printedStatusRE    = regexp.MustCompile(`(?i)["']?apiErrorStatus["']?\s*[:=]\s*429\b`)
	printedRateLimitRE = regexp.MustCompile(`(?i)["']?(?:error|code)["']?\s*[:=]\s*["']?rate_limit\b`)
	printedFinalRE     = regexp.MustCompile(`(?i)["']?apiErrorIsTransient["']?\s*[:=]\s*false\b`)
	printedRetryRE     = regexp.MustCompile(`(?i)retry[-_ ]?after\s*[:=]`)
	printedTransientRE = regexp.MustCompile(`(?i)["']?apiErrorIsTransient["']?\s*[:=]\s*true\b`)
	apiError429RE      = regexp.MustCompile(`(?i)\bapi\s*error\s*:\s*429\b`)
	terminalCodeRE     = regexp.MustCompile(`(?i)\b(?:exceeded_current_quota_error|insufficient_quota|quota_exhausted)\b`)
	printedCodeFieldRE = regexp.MustCompile(`(?i)["']?(?:type|code|error)["']?\s*[:=]\s*["']?(?:exceeded_current_quota_error|insufficient_quota|quota_exhausted)\b`)
	// These are the user-facing terminal notices emitted by Claude/Codex and compatible backends (including Kimi's
	// retry loop). Requiring both the usage-limit sentence and its reset sentence keeps ordinary discussion of the
	// internal quota_exhausted code from becoming a provider failure.
	usageLimitRE = regexp.MustCompile(`(?i)\byou['’]?ve\s+(?:reached|hit)\s+your\s+(?:(?:\d+[ -]hour|session)\s+)?(?:usage\s+)?limit\b.{0,320}\b(?:your\s+quota\s+will\s+reset|resets?\b)`)
)

type Detector struct {
	tail       string
	lastScreen string
	emitted    bool
}

func NewDetector() *Detector { return &Detector{} }

func failure(detail string) (Failure, bool) {
	detail = strings.TrimSpace(spacesRE.ReplaceAllString(detail, " "))
	if chars := []rune(detail); len(chars) > 512 {
		detail = string(chars[len(chars)-512:])
	}
	return Failure{Kind: QuotaExhausted, Detail: detail, Terminal: true, NeedsTarget: true}, true
}

func conversationLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "❯") || strings.HasPrefix(line, "●")
}

func usageLimitDetail(records []string) string {
	for i := len(records) - 1; i >= 0; i-- {
		if conversationLine(records[i]) {
			continue
		}
		end := min(len(records), i+4)
		joined := strings.TrimSpace(spacesRE.ReplaceAllString(strings.Join(records[i:end], " "), " "))
		if loc := usageLimitRE.FindStringIndex(joined); loc != nil {
			return joined[loc[0]:loc[1]]
		}
	}
	return ""
}

func detect(plain string) (Failure, bool) {
	// A CLI can print several retries in one turn. Classify the latest failure-shaped terminal record, rather than
	// letting an earlier transient response poison every later response still present in the rolling tail.
	records := strings.FieldsFunc(plain, func(r rune) bool { return r == '\n' || r == '\r' })
	// A wrapped TUI notice is one sentence even when tmux captured it on several rows. Prompt and assistant bullets
	// are conversation content, not terminal status, even when somebody quoted the notice verbatim.
	if detail := usageLimitDetail(records); detail != "" {
		return failure(detail)
	}
	for i := len(records) - 1; i >= 0; i-- {
		record := records[i]
		if conversationLine(record) {
			continue
		}
		candidate := printedStatusRE.MatchString(record) || apiError429RE.MatchString(record) ||
			printedCodeFieldRE.MatchString(record) || printedRateLimitRE.MatchString(record) ||
			printedFinalRE.MatchString(record) || printedRetryRE.MatchString(record) || printedTransientRE.MatchString(record)
		if !candidate {
			continue
		}
		if printedRetryRE.MatchString(record) || printedTransientRE.MatchString(record) {
			return Failure{}, false
		}
		terminalCode := terminalCodeRE.MatchString(record) && (apiError429RE.MatchString(record) || printedStatusRE.MatchString(record))
		if terminalCode || printedStatusRE.MatchString(record) && printedRateLimitRE.MatchString(record) && printedFinalRE.MatchString(record) {
			return failure(record)
		}
		break
	}
	return Failure{}, false
}

func (d *Detector) Write(chunk []byte) (Failure, bool) {
	if d.emitted || len(chunk) == 0 {
		return Failure{}, false
	}
	d.tail += string(chunk)
	if len(d.tail) > tailLimit {
		d.tail = d.tail[len(d.tail)-tailLimit:]
	}
	plain := ansiRE.ReplaceAllString(d.tail, "")
	f, ok := detect(plain)
	if !ok {
		return Failure{}, false
	}
	d.emitted = true
	return f, true
}

// PrimeScreen records the pane before a turn starts, so an old failure still visible in scrollback is not attributed
// to the new turn.
func (d *Detector) PrimeScreen(screen string) {
	d.lastScreen = ansiRE.ReplaceAllString(screen, "")
}

// ObserveScreen classifies a new failure notice in the server's tmux pane snapshots. Unlike Write, this is fed whole
// captures by the conversation actor and therefore works without a browser terminal websocket.
func (d *Detector) ObserveScreen(screen string) (Failure, bool) {
	if d.emitted || screen == "" {
		return Failure{}, false
	}
	plain := ansiRE.ReplaceAllString(screen, "")
	previous := d.lastScreen
	d.lastScreen = plain
	f, ok := detect(plain)
	if !ok || previous != "" && strings.Contains(strings.TrimSpace(spacesRE.ReplaceAllString(previous, " ")), f.Detail) {
		return Failure{}, false
	}
	d.emitted = true
	return f, true
}
