package stats

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// Daily reports: a day's sessions summed up by the person's own Claude Code or Codex CLI, kept in
// <data>/stats/daily-reports/<date>.json (the old runtime's files).

func (rt *Route) reportsDir() string { return filepath.Join(rt.DataDir, "stats", "daily-reports") }

// cachedReports are the reports on disk by their date; a later file wins a date.
func (rt *Route) cachedReports() map[string]*jsvalue.Object {
	out := map[string]*jsvalue.Object{}
	names, err := os.ReadDir(rt.reportsDir())
	if err != nil {
		return out
	}
	for _, n := range names {
		if !strings.HasSuffix(n.Name(), ".json") {
			continue
		}
		if o := readReport(filepath.Join(rt.reportsDir(), n.Name())); o != nil {
			date, _ := o.Get("date")
			out[jsValueStr(date)] = o
		}
	}
	return out
}

func readReport(path string) *jsvalue.Object {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
	if err != nil {
		return nil
	}
	o, ok := v.(*jsvalue.Object)
	if !ok {
		return nil
	}
	date, _ := o.Get("date")
	brief, _ := o.Get("brief")
	if !jsvalue.Truthy(date) || !jsvalue.Truthy(brief) {
		return nil
	}
	return o
}

// DailyReportList answers GET /api/stats/daily-report/list?offset=&limit=.
func (rt *Route) DailyReportList(r *http.Request) (int, any, http.Header) {
	return rt.getRoute(nil, func(r *http.Request) (any, error) {
		q := jscompat.Query(r.URL.RawQuery)
		num := func(k string) float64 {
			v, ok := q[k]
			if !ok {
				return math.NaN()
			}
			return jsNumber(strings.Join(v, ","))
		}
		or := func(x, d float64) float64 {
			if x == 0 || math.IsNaN(x) {
				return d
			}
			return x
		}
		offset := math.Max(0, or(num("offset"), 0))
		limit := math.Min(50, math.Max(1, or(num("limit"), 10)))
		c, err := rt.statsCache()
		if err != nil {
			return nil, err
		}
		reports := rt.cachedReports()
		o := rt.buildOverview(c, periodAll)
		allTokens := 0.0
		for _, t := range o.dailyTokens {
			allTokens = add(add(add(add(allTokens, t.input), t.output), t.cacheRead), t.cacheCreation)
		}
		totalCost := 0.0
		for _, k := range o.models.ordered() {
			totalCost = add(totalCost, o.models.m[k].cost)
		}
		type item struct {
			date     string
			sessions float64
			cost     float64
			report   any
		}
		var days []item
		for _, d := range o.daily {
			if !(z(d.sessionCount) > 0) {
				continue
			}
			dayTokens := 0.0
			for _, t := range o.dailyTokens {
				if t.date == d.date {
					dayTokens = add(add(add(t.input, t.output), t.cacheRead), t.cacheCreation)
					break
				}
			}
			cost := 0.0
			if allTokens > 0 {
				cost = totalCost * (dayTokens / allTokens)
			}
			var report any
			if rep, ok := reports[d.date]; ok {
				report = rep
			}
			days = append(days, item{d.date, d.sessionCount, cost, report})
		}
		col := newCollator()
		sortStable(days, func(a, b item) int { return col.CompareString(b.date, a.date) })
		from, to := jsSliceRange(len(days), offset, offset+limit)
		list := make([]any, 0, to-from)
		for _, d := range days[from:to] {
			list = append(list, jsvalue.NewObject("date", d.date, "sessionCount", d.sessions, "cost", d.cost, "report", d.report))
		}
		return jsvalue.NewObject("days", list, "total", float64(len(days)), "offset", offset, "limit", limit), nil
	})(r)
}

// jsSliceRange is the range Array.prototype.slice(start, end) takes.
func jsSliceRange(n int, start, end float64) (int, int) {
	clamp := func(x float64) int {
		if math.IsNaN(x) {
			return 0
		}
		x = math.Trunc(x)
		if x < 0 {
			x = math.Max(0, float64(n)+x)
		}
		return int(math.Min(x, float64(n)))
	}
	from, to := clamp(start), clamp(end)
	if to < from {
		to = from
	}
	return from, to
}

type reportSession struct {
	provider          string
	project           string
	start             float64
	msgCount, toolCnt int
	firstMessage      string
}

var commandName = regexp.MustCompile(`<command-name>/([^<]+)</command-name>`)

func (rt *Route) claudeReportSessions(date string) []reportSession {
	var out []reportSession
	for _, f := range listClaudeFiles(rt.claudeProjects()) {
		var texts []string
		var stamps []float64
		tools := 0
		cwd := ""
		if file, err := os.Open(f.path); err == nil {
			lr := newLineReader(file)
			for {
				line, ok := lr.next()
				if !ok {
					break
				}
				if len(bytes.TrimFunc(line, jscompat.IsSpace)) == 0 {
					continue
				}
				var e claudeEntry
				if !scanClaudeEntry(line, &e) {
					continue
				}
				ts := jsStr(e.Timestamp)
				if !strings.HasPrefix(ts, date) {
					continue
				}
				stamps = append(stamps, jscompat.ParseDate(ts))
				typ := jsStr(e.Type)
				if cwd == "" {
					if s, ok := e.Cwd.(string); ok {
						cwd = s
					}
				}
				if typ == "user" {
					if !rawTruthy(e.Message) {
						continue
					}
					var content any
					if e.Message[0] == '{' {
						members(e.Message, func(key []byte, esc bool, v []byte) {
							if keyIs(key, esc, "content") {
								content = value(v)
							}
						})
					}
					text := ""
					switch c := content.(type) {
					case string:
						text = jscompat.Slice(c, 300)
					case []any:
						var parts []string
						for _, it := range c {
							if mm, ok := it.(map[string]any); ok && mm["type"] == "text" {
								parts = append(parts, jscompat.Slice(jsStr(mm["text"]), 300))
							}
						}
						text = strings.Join(parts, " | ")
					}
					if strings.Contains(text, "<command-message>") {
						name := "unknown"
						if sm := commandName.FindStringSubmatch(text); sm != nil {
							name = sm[1]
						}
						text = "[Command: " + name + "]"
					}
					if strings.Contains(text, "<local-command-caveat>") || strings.Contains(text, "<task-notification>") {
						continue
					}
					if jscompat.Trim(text) != "" {
						texts = append(texts, jscompat.Slice(text, 200))
					}
				}
				if typ == "assistant" {
					if !rawTruthy(e.Message) {
						continue
					}
					if m, ok := scanMessage(e.Message); ok {
						tools += countToolUses(m.Content)
					}
				}
			}
			file.Close()
		}
		if len(stamps) > 0 && len(texts) > 0 {
			project := filepath.Base(filepath.Dir(f.path))
			if cwd != "" {
				project = rt.shortenCwd(cwd)
			}
			out = append(out, reportSession{"claude", project, minOf(stamps), len(texts), tools, texts[0]})
		}
	}
	sortStable(out, func(a, b reportSession) int { return cmpNum(a.start - b.start) })
	return out
}

// minOf is Math.min(...xs).
func minOf(xs []float64) float64 {
	m := math.Inf(1)
	for _, x := range xs {
		if math.IsNaN(x) {
			return math.NaN()
		}
		m = math.Min(m, x)
	}
	return m
}

func (rt *Route) codexReportSessions(date string) []reportSession {
	var out []reportSession
	for _, path := range allCodexFiles(rt.codexSessions()) {
		var texts []string
		var stamps []float64
		tools := 0
		cwd := ""
		if file, err := os.Open(path); err == nil {
			lr := newLineReader(file)
			for {
				line, ok := lr.next()
				if !ok {
					break
				}
				if len(bytes.TrimFunc(line, jscompat.IsSpace)) == 0 {
					continue
				}
				var l codexLineJSON
				if !scanCodexLine(line, &l) {
					continue
				}
				ts := jsStr(l.Timestamp)
				if !strings.HasPrefix(ts, date) {
					continue
				}
				stamps = append(stamps, jscompat.ParseDate(ts))
				p, _, obj := payloadOf(l.Payload)
				if !obj {
					p = codexPayload{}
				}
				if l.Type == "session_meta" {
					if s, ok := p.Cwd.(string); ok {
						cwd = s
						continue
					}
				}
				if l.Type == "event_msg" && p.Type == "user_message" {
					text := jscompat.Trim(jsStr(p.Message))
					if text != "" && !strings.HasPrefix(text, "<") && !strings.HasPrefix(text, "# AGENTS.md") {
						texts = append(texts, jscompat.Slice(text, 200))
					}
					continue
				}
				if l.Type == "response_item" && (p.Type == "function_call" || p.Type == "custom_tool_call") {
					tools++
				}
			}
			file.Close()
		}
		if len(stamps) > 0 && len(texts) > 0 {
			project := filepath.Base(filepath.Dir(path))
			if cwd != "" {
				project = rt.shortenCwd(cwd)
			}
			out = append(out, reportSession{"codex", project, minOf(stamps), len(texts), tools, texts[0]})
		}
	}
	return out
}

// localeLanguages names each interface language the way a model recognises it: in English with the native name. The native
// names are data for the prompt, not display text.
var localeLanguages = map[string]string{
	"en": "English", "ko": "Korean (한국어)", "ja": "Japanese (日本語)", "zh-CN": "Simplified Chinese (简体中文)",
	"zh-TW": "Traditional Chinese (繁體中文)", "de": "German (Deutsch)", "es": "Spanish (Español)", "fr": "French (Français)",
	"pt-BR": "Brazilian Portuguese (Português do Brasil)", "ru": "Russian (Русский)", "tr": "Turkish (Türkçe)",
}

func summaryPrompt(date, locale string) string {
	language, ok := localeLanguages[locale]
	if !ok {
		language = localeLanguages["en"]
	}
	return `Summarize the Claude and Codex session history for ` + date + `.

Output format (use exactly this structure):

[BRIEF]
A 2-3 line summary of the entire day. Use terse noun-form phrases. Plain text only. No markdown headings (#) or separators (---).

[DETAIL]
## Project Name
### Major Feature / Area
- One-line task summary
- One-line task summary
### Another Feature / Area
- One-line task summary

Rules:
- The [BRIEF] and [DETAIL] tags must each appear exactly once, at the start of a line.
- Do not insert meta text outside the tags (separators, numbering, explanations).
- Always include the "## Project Name" heading, even when there is only one project.
- Each item is a single line — no filenames, no code snippets.
- Use terse noun-form phrases (e.g. "Fix login logic", "Improve mobile layout"). Avoid finished-sentence / past-tense forms.
- Focus on WHAT, not HOW.
- Skip trivial chatter (greetings, one-off questions, commit commands).
- IMPORTANT: Write ALL brief and detail content in ` + language + `. Keep the [BRIEF] / [DETAIL] tags and markdown syntax (##, ###, -) as-is in English.

Data:`
}

func promptData(sessions []reportSession) string {
	lines := make([]string, len(sessions))
	for i, s := range sessions {
		at := "Inval"
		if !math.IsNaN(s.start) {
			at = msTime(s.start).Format("15:04")
		}
		lines[i] = "[" + at + "] [" + s.provider + "] [" + s.project + "] msgs=" + strconv.Itoa(s.msgCount) + ", tools=" +
			strconv.Itoa(s.toolCnt) + " | " + s.firstMessage
	}
	return strings.Join(lines, "\n")
}

// spaceThenLine is where the old reply pattern's `\s*\n` ends after a tag: past the last line end of the space run.
func spaceThenLine(s string, from int) (int, bool) {
	end, last := from, -1
	for end < len(s) {
		r, size := rune(s[end]), 1
		if r >= 0x80 {
			r, size = decodeRune(s[end:])
		}
		if !jscompat.IsSpace(r) {
			break
		}
		if r == '\n' {
			last = end
		}
		end += size
	}
	if last < 0 {
		return 0, false
	}
	return last + 1, true
}

func decodeRune(s string) (rune, int) {
	for i, r := range s {
		if i == 0 {
			return r, len(string(r))
		}
	}
	return 0, 1
}

// parseSummary splits the CLI's reply into the brief and the detail.
func parseSummary(response string) (brief, detail string) {
	brief = jscompat.Slice(response, 500)
	for i := 0; ; {
		at := strings.Index(response[i:], "[BRIEF]")
		if at < 0 {
			break
		}
		at += i
		if start, ok := spaceThenLine(response, at+len("[BRIEF]")); ok {
			if end := strings.Index(response[start:], "[DETAIL]"); end >= 0 {
				b := response[start : start+end]
				b = strings.TrimRightFunc(b, func(r rune) bool { return jscompat.IsSpace(r) || r == '#' || r == '-' })
				brief = jscompat.Trim(b)
				break
			}
		}
		i = at + 1
	}
	for i := 0; ; {
		at := strings.Index(response[i:], "[DETAIL]")
		if at < 0 {
			break
		}
		at += i
		if start, ok := spaceThenLine(response, at+len("[DETAIL]")); ok {
			detail = jscompat.Trim(response[start:])
			break
		}
		i = at + 1
	}
	return brief, detail
}

func (rt *Route) shellPath() string {
	if rt.ShellPath != nil {
		return rt.ShellPath()
	}
	return os.Getenv("PATH")
}

// lookPath finds a command along path, as the child started with that PATH would.
func lookPath(name, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("spawn %s ENOENT", name)
}

func childEnv(path string) []string {
	env := []string{}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PATH=") {
			env = append(env, kv)
		}
	}
	return append(env, "PATH="+path)
}

// runCLI is execFile: the command line in the error as Node wrote it, the output capped at 1 MiB.
func (rt *Route) runCLI(name string, args []string, stdin string) (stdout, stderr string, err error) {
	path := rt.shellPath()
	bin, err := lookPath(name, path)
	if err != nil {
		return "", "", err
	}
	timeout := rt.CLITimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Args[0] = name
	cmd.Env = childEnv(path)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut cappedBuffer
	out.max, errOut.max = 1<<20, 1<<20
	cmd.Stdout, cmd.Stderr = &out, &errOut
	runErr := cmd.Run()
	stdout, stderr = jscompat.DecodeUTF8(out.Bytes()), jscompat.DecodeUTF8(errOut.Bytes())
	if runErr != nil || out.over || errOut.over {
		line := strings.Join(append([]string{name}, args...), " ")
		if out.over || errOut.over {
			return stdout, stderr, errors.New("stdout maxBuffer length exceeded")
		}
		return stdout, stderr, errors.New("Command failed: " + line + "\n" + stderr)
	}
	return stdout, stderr, nil
}

type cappedBuffer struct {
	bytes.Buffer
	max  int
	over bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		b.over = true
		return 0, errors.New("maxBuffer exceeded")
	}
	return b.Buffer.Write(p)
}

func (rt *Route) summarize(provider, input, system string) (string, error) {
	if provider == "codex" {
		var suffix [8]byte
		_, _ = rand.Read(suffix[:])
		out := filepath.Join(os.TempDir(), "daily-report-"+strconv.FormatInt(rt.now().UnixMilli(), 10)+"-"+hex.EncodeToString(suffix[:])+".txt")
		prompt := system + "\n\n" + input
		stdout, stderr, err := rt.runCLI("codex", []string{"exec", "--skip-git-repo-check", "--ephemeral", "--sandbox", "read-only", "-c",
			`approval_policy="never"`, "-o", out, prompt}, "")
		if err != nil {
			_ = os.Remove(out)
			msg := "codex exec failed: " + err.Error()
			if stderr != "" {
				msg += ": " + jscompat.Trim(stderr)
			}
			return "", errors.New(msg)
		}
		text := stdout
		if raw, err := os.ReadFile(out); err == nil {
			text = jscompat.DecodeUTF8(raw)
		}
		_ = os.Remove(out)
		return jscompat.Trim(text), nil
	}
	stdout, _, err := rt.runCLI("claude", []string{"-p"}, system+"\n\n"+input)
	if err != nil {
		return "", errors.New("claude -p failed: " + err.Error())
	}
	return jscompat.Trim(stdout), nil
}

func (rt *Route) configuredProvider() string {
	raw, err := os.ReadFile(rt.ConfigFile)
	if err != nil {
		return "claude"
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return "claude"
	}
	if o, ok := v.(*jsvalue.Object); ok {
		if p, _ := o.Get("noteSummaryProvider"); p == "codex" {
			return "codex"
		}
	}
	return "claude"
}

func (rt *Route) writeReport(report *jsvalue.Object) {
	if os.MkdirAll(rt.reportsDir(), 0o755) != nil {
		return
	}
	date, _ := report.Get("date")
	_ = os.WriteFile(filepath.Join(rt.reportsDir(), jsValueStr(date)+".json"), jsvalue.Stringify(report, "  "), 0o644)
}

var dateShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// DailyReportGenerate answers POST /api/stats/daily-report/generate {date, force?, locale?, provider?}.
func (rt *Route) DailyReportGenerate(r *http.Request) (int, any, http.Header) {
	if !rt.Authorized(r) {
		return http.StatusUnauthorized, unauthorized, nil
	}
	body, _, status, refusal := nodejson.ParseBody(r, 1<<20, "1mb")
	if status != 0 {
		return status, gate.Raw{ContentType: "text/plain", Body: []byte(refusal)}, nil
	}
	if r.Method != http.MethodPost {
		s, v, h := errorAnswer(r, http.StatusMethodNotAllowed, "method-not-allowed")
		h.Set("Allow", "POST")
		return s, v, h
	}
	if body == nil {
		return http.StatusInternalServerError, serverError, nil
	}
	field := func(k string) (any, bool) {
		if o, ok := body.(*jsvalue.Object); ok {
			return o.Get(k)
		}
		return nil, false
	}
	invalid := func(code, msg string) (int, any, http.Header) {
		return jsonAnswer(r, http.StatusBadRequest, jsvalue.Stringify(jsvalue.NewObject("error", code, "message", msg), ""))
	}
	dateV, _ := field("date")
	date, isStr := dateV.(string)
	if !isStr || date == "" || !dateShape.MatchString(date) {
		return invalid("invalid-date", "date must be YYYY-MM-DD")
	}
	providerV, hasProvider := field("provider")
	if hasProvider && providerV != "claude" && providerV != "codex" {
		return invalid("invalid-provider", "provider must be claude or codex")
	}
	locale := "en"
	if l, ok := field("locale"); ok {
		if s, isStr := l.(string); isStr && jscompat.Trim(s) != "" {
			locale = s
		}
	}
	forceV, _ := field("force")
	provider := rt.configuredProvider()
	if hasProvider {
		provider = providerV.(string)
	}
	report, err := rt.generateReport(date, jsvalue.Truthy(forceV), locale, provider)
	if err != nil {
		return jsonAnswer(r, http.StatusInternalServerError,
			jsvalue.Stringify(jsvalue.NewObject("error", "generation-failed", "message", err.Error()), ""))
	}
	return jsonAnswer(r, http.StatusOK, jsvalue.Stringify(report, ""))
}

func (rt *Route) generateReport(date string, force bool, locale, provider string) (*jsvalue.Object, error) {
	if !force {
		if existing := readReport(filepath.Join(rt.reportsDir(), date+".json")); existing != nil {
			l, _ := existing.Get("locale")
			p, hasP := existing.Get("provider")
			if !hasP || p == nil {
				p = "claude"
			}
			if l == locale && p == provider {
				return existing, nil
			}
		}
	}
	sessions := append(rt.claudeReportSessions(date), rt.codexReportSessions(date)...)
	sortStable(sessions, func(a, b reportSession) int { return cmpNum(a.start - b.start) })
	generatedAt := func() string { return nowISO(rt.now()) }
	if len(sessions) == 0 {
		report := jsvalue.NewObject("date", date, "brief", "No activity recorded.", "detail", "", "generatedAt", generatedAt(), "locale", locale,
			"provider", provider)
		rt.writeReport(report)
		return report, nil
	}
	response, err := rt.summarize(provider, promptData(sessions), summaryPrompt(date, locale))
	if err != nil {
		return nil, err
	}
	brief, detail := parseSummary(response)
	report := jsvalue.NewObject("date", date, "brief", brief, "detail", detail, "generatedAt", generatedAt(), "locale", locale, "provider", provider)
	rt.writeReport(report)
	return report, nil
}
