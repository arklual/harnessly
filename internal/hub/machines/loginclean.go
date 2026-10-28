package machines

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// loginHosts are the pages a person is sent to from an agent login: the provider's own sign-in hosts, exact names
// (not their subdomains). The web UI keeps the same list and checks again before it shows a link; this one stops a
// link that no page of the hub should ever carry before it leaves the hub, as the machine that wrote it may be hostile.
var loginHosts = map[string][]string{
	hubproto.ProviderClaude: {"claude.ai", "console.anthropic.com", "platform.claude.com"},
	hubproto.ProviderCodex:  {"auth.openai.com"},
}

const (
	maxLoginError = 500
	maxUserCode   = 32
)

var userCodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,31}$`)

// Texts the person reads when a login lost its link on the way. They say what to do instead and nothing of what the
// machine sent.
const (
	badLinkText    = "Машина прислала ссылку входа не на страницу поставщика — мы её скрыли. Войдите в консоли или по ключу."
	badConsoleText = "Машина прислала неверный адрес консоли — мы его скрыли. Войдите по ключу или начните вход заново."
)

// SafeLoginURL is the sign-in link of a login, normalised, or false when it is not safe to show: it must parse, be
// https on the default port, carry no credentials, no backslash and no control characters, and sit on a sign-in host of
// the provider.
func SafeLoginURL(provider, raw string) (string, bool) {
	hosts := loginHosts[provider]
	if len(hosts) == 0 || raw == "" || len(raw) > 4096 || strings.ContainsAny(raw, "\\ \t\r\n") {
		return "", false
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.Port() != "" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	for _, h := range hosts {
		if host == h {
			u.Host = host
			return u.String(), true
		}
	}
	return "", false
}

var consoleTabRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// SafeConsoleURL is the address of a login console, or false when it is not a page of this machine's own runtime
// base path: `/m/<id>/console/<16 hex>`, or `/m/<id>/?tab=<id>` of a runtime without console pages.
func SafeConsoleURL(machineID, raw string) bool {
	base := "/m/" + machineID + "/"
	if !strings.HasPrefix(raw, base) || len(raw) > 256 {
		return false
	}
	rest := raw[len(base):]
	if id, ok := strings.CutPrefix(rest, "console/"); ok {
		if len(id) != 16 {
			return false
		}
		for _, r := range id {
			if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
				return false
			}
		}
		return true
	}
	if tab, ok := strings.CutPrefix(rest, "?tab="); ok {
		return consoleTabRe.MatchString(tab)
	}
	return false
}

// CleanLogin is a login as the machine reported it, made fit for the people who will read it: the sign-in link only
// when it is the provider's own page, the console address only when it is under this machine's base path, the device
// code and the error text bounded. A login that lost the link it was waiting for becomes `failed` with a text that
// says what to do (the machine's own state would leave the page waiting for a link that will not come). The second
// result names the fields that were refused, for the log; it never holds their content.
func CleanLogin(l hubproto.Login, machineID string) (hubproto.Login, []string) {
	var refused []string
	if l.URL != nil {
		if safe, ok := SafeLoginURL(l.Provider, *l.URL); ok {
			l.URL = &safe
		} else {
			l.URL = nil
			refused = append(refused, "url")
			if l.State == hubproto.LoginURL {
				l.State, l.Error = hubproto.LoginFailed, ptr(badLinkText)
				l.NeedsCode, l.UserCode = false, nil
			}
		}
	}
	if l.ConsoleURL != nil && !SafeConsoleURL(machineID, *l.ConsoleURL) {
		l.ConsoleURL = nil
		refused = append(refused, "consoleUrl")
		if l.State == hubproto.LoginWaiting && l.Method == hubproto.MethodConsole {
			l.State, l.Error = hubproto.LoginFailed, ptr(badConsoleText)
		}
	}
	if l.UserCode != nil && !userCodeRe.MatchString(*l.UserCode) {
		l.UserCode = nil
		refused = append(refused, "userCode")
	}
	if l.Error != nil && utf8.RuneCountInString(*l.Error) > maxLoginError {
		cut := string([]rune(*l.Error)[:maxLoginError])
		l.Error = &cut
	}
	return l, refused
}

func ptr[T any](v T) *T { return &v }
