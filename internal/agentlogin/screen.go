package agentlogin

import (
	"regexp"
	"strings"
)

// What a login prints is read off the terminal screen, so the reading is lenient about colours, hyperlinks and the
// wording around the address, and strict about the address itself: it has to be the provider's.
var (
	// A hyperlink (OSC 8) wraps the visible text, which is the address again: dropping the escape keeps one copy.
	oscSeq = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)
	csiSeq = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	escSeq = regexp.MustCompile(`\x1b[()*+][0-9A-Za-z]|\x1b[@-Z\\-_]`)

	anyURL = regexp.MustCompile(`https://[^\s"'<>\x00-\x1f]+`)
	// A device code is groups of capitals and digits with a dash: "K7QF-2M9P", "H3CF-2WRYQ".
	deviceCode = regexp.MustCompile(`\b[A-Z0-9]{4,6}-[A-Z0-9]{4,6}\b`)
)

// StripANSI removes colours, cursor moves and hyperlink escapes, and carriage returns.
func StripANSI(s string) string {
	s = oscSeq.ReplaceAllString(s, "")
	s = csiSeq.ReplaceAllString(s, "")
	s = escSeq.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "\r", "")
}

// trimURL drops the punctuation a sentence puts after an address.
func trimURL(u string) string { return strings.TrimRight(u, ".,;:)]}") }

// ClaudeLoginURL is the sign-in address on the screen of `claude auth login`: the first address that is an OAuth
// authorize page of Anthropic. "" when there is none yet.
func ClaudeLoginURL(screen string) string {
	for _, u := range anyURL.FindAllString(StripANSI(screen), -1) {
		u = trimURL(u)
		if strings.Contains(u, "/oauth/authorize") && anthropicHost(u) {
			return u
		}
	}
	return ""
}

// anthropicHost says the address is on claude.com, claude.ai or anthropic.com: a login that sends the person elsewhere
// is not shown to them.
func anthropicHost(u string) bool {
	host := strings.TrimPrefix(u, "https://")
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	for _, d := range []string{"claude.com", "claude.ai", "anthropic.com"} {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// CodexDevice is the page and the one-time code on the screen of `codex login --device-auth`. The code is the first
// one after the address; both are "" until the screen has them.
func CodexDevice(screen string) (url, code string) {
	screen = StripANSI(screen)
	for _, u := range anyURL.FindAllString(screen, -1) {
		u = trimURL(u)
		if openAIHost(u) {
			url = u
			screen = screen[strings.Index(screen, u)+len(u):]
			break
		}
	}
	if url == "" {
		return "", ""
	}
	// The sentence "expires in 15 minutes" has no dash-joined group, so the first match is the code.
	return url, deviceCode.FindString(screen)
}

func openAIHost(u string) bool {
	host := strings.TrimPrefix(u, "https://")
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	for _, d := range []string{"openai.com", "chatgpt.com"} {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// LastLines is the end of a screen, for a message about why a login stopped: blank lines dropped, at most n lines.
func LastLines(screen string, n int) string {
	var lines []string
	for _, l := range strings.Split(StripANSI(screen), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " / ")
}
