// Package vault carries an agent's request that the person's page log in on a site with the password safe: the origin
// check, the cleaning of the page's answer, the per-tab rate limit and the bridge over the sync socket. A value of the
// safe never passes through here, only a status.
package vault

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// Statuses a page may report.
var Statuses = []string{"logged-in", "not-logged-in", "needs-approval", "denied", "no-entry", "no-tab", "unavailable", "failed"}

const (
	maxOrigin     = 255
	maxMessage    = 300
	answerTimeout = 120 * time.Second
	maxPending    = 16
)

var (
	httpsPrefix = regexp.MustCompile(`(?i)^https://`)
	hostRe      = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)*|\[[0-9a-f:.]+\])$`)
)

// Origin is the one spelling of a site a password belongs to, `https://host[:port]` (lowercase, no default port); ""
// for anything else: another scheme, a path, a query, `user@`, a trailing dot.
func Origin(v any) string {
	s, ok := v.(string)
	if !ok || s == "" || jscompat.Len(s) > maxOrigin {
		return ""
	}
	m := httpsPrefix.FindString(s)
	if m == "" {
		return ""
	}
	rest := strings.TrimSuffix(s[len(m):], "/")
	if rest == "" || strings.ContainsAny(rest, "/?#@\\") {
		return ""
	}
	for _, r := range rest {
		if r < 0x20 {
			return ""
		}
	}
	u, err := url.Parse("https://" + rest)
	if err != nil || u.User != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(u.Host, "[") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if strings.HasSuffix(u.Host, ":") {
		port = ""
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n > 65535 || n == 0 {
			return ""
		}
		port = strconv.Itoa(n)
		if n == 443 {
			port = ""
		}
	}
	if !hostRe.MatchString(host) {
		return ""
	}
	origin := "https://" + host
	if port != "" {
		origin += ":" + port
	}
	if len(origin) > maxOrigin {
		return ""
	}
	return origin
}

// Result is how a login went, as the page says: a status and one safe line.
type Result struct {
	Status, Message string
}

// JSON is the result as the CLI reads it.
func (r Result) JSON() *jsvalue.Object {
	return jsvalue.NewObject("status", r.Status, "message", r.Message)
}

// ParseResult reads a page's answer; nil when it is not one.
func ParseResult(v any) *Result {
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return nil
	}
	status, _ := o.Get("status")
	known := false
	for _, s := range Statuses {
		known = known || status == s
	}
	msg, isStr := func() (string, bool) { m, _ := o.Get("message"); s, ok := m.(string); return s, ok }()
	if !known || !isStr {
		return nil
	}
	return &Result{Status: status.(string), Message: jscompat.Slice(tmux.OneLineSafe(msg), maxMessage)}
}

// Outcome is how a request ended: answered (with Result), no-ui, timeout, busy or rate-limited.
type Outcome struct {
	Kind   string
	Result Result
}

// CLIReply is the status and body the CLI gets for an outcome.
func CLIReply(o Outcome) (int, *jsvalue.Object) {
	switch o.Kind {
	case "answered":
		return 200, o.Result.JSON()
	case "timeout":
		return 200, Result{"needs-approval", locale.Pick("The person did not answer the sign-in request: ask them to confirm it in the window and try again", "Человек не ответил на просьбу войти: попросите его подтвердить в окне и повторите")}.JSON()
	case "no-ui":
		return 503, jsvalue.NewObject("error", locale.Pick("The interface is not connected: nobody is looking at this machine now (the page is closed)", "Интерфейс не подключён: сейчас никто не смотрит эту машину (страница закрыта)"))
	case "busy":
		return 409, jsvalue.NewObject("error", locale.Pick("The previous sign-in request is still waiting for the person's answer", "Предыдущая просьба войти ещё ждёт ответа человека"))
	}
	return 429, jsvalue.NewObject("error", locale.Pick("Too often: at most 6 sign-in requests in 10 minutes", "Слишком часто: не больше 6 просьб войти за 10 минут"))
}

// Rule is one sliding window: at most Limit takes within Window.
type Rule struct {
	Limit  int
	Window time.Duration
}

// Limiter is a set of sliding windows per key, every rule at once. A refused take takes no slot, and keys whose
// stamps all left the longest window are dropped.
type Limiter struct {
	rules   []Rule
	longest int64
	mu      sync.Mutex
	stamps  map[string][]int64
}

const maxKeys = 10_000

func NewLimiter(rules ...Rule) *Limiter {
	l := &Limiter{rules: rules, stamps: map[string][]int64{}}
	for _, r := range rules {
		l.longest = max(l.longest, r.Window.Milliseconds())
	}
	return l
}

// Take counts a take of key at now (ms); false and the wait until a slot frees when refused.
func (l *Limiter) Take(key string, now int64) (bool, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, list := range l.stamps {
		if len(list) == 0 || now-list[len(list)-1] >= l.longest {
			delete(l.stamps, k)
		}
	}
	var recent []int64
	for _, at := range l.stamps[key] {
		if now-at < l.longest {
			recent = append(recent, at)
		}
	}
	var wait int64
	for _, r := range l.rules {
		var in []int64
		for _, at := range recent {
			if now-at < r.Window.Milliseconds() {
				in = append(in, at)
			}
		}
		if len(in) >= r.Limit {
			wait = int64(math.Max(float64(wait), float64(in[len(in)-r.Limit]+r.Window.Milliseconds()-now)))
		}
	}
	if wait > 0 {
		l.stamps[key] = recent
		return false, wait
	}
	if _, has := l.stamps[key]; len(l.stamps) >= maxKeys && !has {
		return false, 1000
	}
	l.stamps[key] = append(recent, now)
	return true, 0
}

// Bridge carries requests to the pages and answers back.
type Bridge struct {
	Publish     func(event *jsvalue.Object)
	Subscribers func() int
	Now         func() time.Time
	Timeout     time.Duration

	limiter *Limiter
	mu      sync.Mutex
	pending map[string]pendingRequest
}

type pendingRequest struct {
	tabID  string
	answer chan Result
}

// NewBridge is a bridge with the limit of six requests a tab in ten minutes.
func NewBridge(publish func(*jsvalue.Object), subscribers func() int) *Bridge {
	return &Bridge{Publish: publish, Subscribers: subscribers, Now: time.Now, Timeout: answerTimeout,
		limiter: NewLimiter(Rule{Limit: 6, Window: 10 * time.Minute}), pending: map[string]pendingRequest{}}
}

// Dispatch asks the person's page to log in on origin for the tab and waits for the answer.
func (b *Bridge) Dispatch(tabID, origin string) Outcome {
	if b.Subscribers() == 0 {
		return Outcome{Kind: "no-ui"}
	}
	b.mu.Lock()
	for _, p := range b.pending {
		if p.tabID == tabID {
			b.mu.Unlock()
			return Outcome{Kind: "busy"}
		}
	}
	if len(b.pending) >= maxPending {
		b.mu.Unlock()
		return Outcome{Kind: "busy"}
	}
	if ok, _ := b.limiter.Take(tabID, b.Now().UnixMilli()); !ok {
		b.mu.Unlock()
		return Outcome{Kind: "rate-limited"}
	}
	id := panel.UUID()
	answer := make(chan Result, 1)
	b.pending[id] = pendingRequest{tabID: tabID, answer: answer}
	b.mu.Unlock()

	b.Publish(jsvalue.NewObject("type", "vault-request", "id", id, "tabId", tabID, "origin", origin))
	t := time.NewTimer(b.Timeout)
	defer t.Stop()
	select {
	case r := <-answer:
		return Outcome{Kind: "answered", Result: r}
	case <-t.C:
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		select {
		case r := <-answer:
			return Outcome{Kind: "answered", Result: r}
		default:
		}
		return Outcome{Kind: "timeout"}
	}
}

// Answer settles a request with the first answer; false for a later one or an id never issued.
func (b *Bridge) Answer(id string, r Result) bool {
	b.mu.Lock()
	p, ok := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if ok {
		p.answer <- r
	}
	return ok
}
