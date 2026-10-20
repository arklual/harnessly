package share

import (
	_ "embed"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The snapshot page: static HTML without a single script, rebuilt from the stored JSON on every read so render fixes
// reach old snapshots, and the same snapshot as Markdown. Both redact again: the caller passes a redactor.

//go:embed page.css
var pageCSS string

// Render modes.
const (
	ModePreview = "preview" // the owner checks the draft: a banner says so
	ModeExport  = "export"  // a downloaded file
	ModeShared  = "shared"  // a colleague opened a published link
)

// PageCSP allows no scripts, forms, images or frames; styles only from the inline sheet.
const PageCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src 'none'; form-action 'none'; base-uri 'none'"

// HTMLHeaders are the headers of every snapshot page.
func HTMLHeaders() [][2]string {
	return [][2]string{
		{"Content-Type", "text/html; charset=utf-8"},
		{"Cache-Control", "private, no-store"},
		{"Content-Security-Policy", PageCSP + "; frame-ancestors 'none'"},
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"Referrer-Policy", "no-referrer"},
	}
}

// RenderOptions are a render's mode and what a published link adds.
type RenderOptions struct {
	Mode string
	// Viewer is who opened a published link ("" for none).
	Viewer string
	// ExpiresAt is when a published link stops working (0 for none).
	ExpiresAt float64
}

// Page is what the page names: the platform's brand, and the links it must not carry (into the agent workspace).
type Page struct {
	Brand string
	// DropHostSuffix and DropPathPrefix: an absolute link to a host ending so, under that path, is dropped.
	DropHostSuffix, DropPathPrefix string
}

var (
	providerLabels = map[string]string{"claude": "Claude Code", "codex": "Codex"}
	assistantNames = map[string]string{"claude": "Claude", "codex": "Codex"}
)

// transform is the page's URL check: absolute http(s) and mailto links only, none back into the agent workspace.
func (p Page) transform(url string) (string, bool) {
	u, ok := parseURL(url)
	if !ok {
		return "", false
	}
	if p.DropHostSuffix != "" && strings.HasSuffix(u.hostname, p.DropHostSuffix) && strings.HasPrefix(u.pathname, p.DropPathPrefix) {
		return "", false
	}
	return u.href, true
}

var moscow = func() *time.Location {
	if l, err := time.LoadLocation("Europe/Moscow"); err == nil {
		return l
	}
	return time.FixedZone("MSK", 3*3600)
}()

func at(ms float64) time.Time {
	return time.UnixMilli(int64(math.Floor(ms))).In(moscow)
}

func formatTime(ms float64) string { return at(ms).Format("15:04") }

// formatDate and formatDateTime write the day as dd.mm.yyyy in Russian and as an ISO date in English.
func formatDate(ms float64) string {
	return at(ms).Format(locale.Pick("2006-01-02", "02.01.2006"))
}
func formatDateTime(ms float64) string {
	return at(ms).Format(locale.Pick("2006-01-02, 15:04", "02.01.2006, 15:04"))
}

// count is the unit for n in the runtime's language: English has one form for 1 and one for the rest, Russian three.
func count(n float64, enOne, enMany, ruOne, ruFew, ruMany string) string {
	if !locale.Russian() {
		if n == 1 {
			return enOne
		}
		return enMany
	}
	return plural(n, ruOne, ruFew, ruMany)
}

// plural is the Russian form for n.
func plural(n float64, one, few, many string) string {
	mod10 := math.Mod(n, 10)
	mod100 := math.Mod(n, 100)
	if mod10 == 1 && mod100 != 11 {
		return one
	}
	if mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14) {
		return few
	}
	return many
}

// formatNumber is Intl.NumberFormat(locale, {maximumFractionDigits: 1}): half away from zero on the shortest decimal
// form. Russian writes a comma for the decimal point and no-break spaces between thousands, English a point and commas.
func formatNumber(x float64) string {
	neg := x < 0
	digits := strconv.FormatFloat(math.Abs(x), 'f', -1, 64)
	intPart, frac, _ := strings.Cut(digits, ".")
	if len(frac) > 1 {
		up := frac[1] >= '5'
		frac = frac[:1]
		if up {
			n, _ := strconv.Atoi(intPart + frac)
			s := strconv.Itoa(n + 1)
			intPart, frac = s[:len(s)-1], s[len(s)-1:]
			if intPart == "" {
				intPart = "0"
			}
		}
	}
	if frac == "0" {
		frac = ""
	}
	if len(intPart) >= 4 {
		var b strings.Builder
		for i, c := range intPart {
			if i > 0 && (len(intPart)-i)%3 == 0 {
				b.WriteString(locale.Pick(",", "\u00a0"))
			}
			b.WriteRune(c)
		}
		intPart = b.String()
	}
	out := intPart
	if frac != "" {
		out += locale.Pick(".", ",") + frac
	}
	if neg && out != "0" {
		out = "-" + out
	}
	return out
}

func formatTokens(n float64) string {
	if n < 1000 {
		return jscompat.Number(n) + " " + count(n, "token", "tokens", "токен", "токена", "токенов")
	}
	if n < 1_000_000 {
		return formatNumber(n/1000) + locale.Pick("k tokens", " тыс. токенов")
	}
	return formatNumber(n/1_000_000) + locale.Pick("M tokens", " млн токенов")
}

func formatDuration(ms float64) string {
	minutes := math.Floor(ms/60_000 + 0.5)
	if minutes < 1 {
		return locale.Pick("under a minute", "меньше минуты")
	}
	if minutes < 60 {
		return jscompat.Number(minutes) + locale.Pick(" min", " мин")
	}
	rest := math.Mod(minutes, 60)
	hours := jscompat.Number(math.Floor(minutes / 60))
	if rest != 0 {
		return hours + locale.Pick(" h ", " ч ") + jscompat.Number(rest) + locale.Pick(" min", " мин")
	}
	return hours + locale.Pick(" h", " ч")
}

func num(o *jsvalue.Object, k string) float64 {
	f, _ := o.Number(k)
	return f
}

func economicsLine(e *jsvalue.Object) string {
	t := e.Object("tokens")
	parts := []string{formatTokens(num(t, "input") + num(t, "output") + num(t, "cacheRead") + num(t, "cacheWrite"))}
	if a, ok := e.Get("activeMs"); ok && a != nil {
		f, _ := a.(float64)
		parts = append(parts, locale.Pick("active ", "активно ")+formatDuration(f))
	}
	parts = append(parts, locale.Pick("total ", "всего ")+formatDuration(num(e, "wallMs")))
	if turns := num(e, "turns"); turns > 0 {
		parts = append(parts, jscompat.Number(turns)+" "+count(turns, "turn", "turns", "ход", "хода", "ходов"))
	}
	return strings.Join(parts, " · ")
}

var actionKind = map[string]string{
	"Read": "read", "Grep": "read", "Glob": "read", "WebSearch": "read", "WebFetch": "read",
	"Bash": "command", "exec": "command",
	"Edit": "edit", "MultiEdit": "edit", "Write": "edit", "NotebookEdit": "edit", "apply_patch": "edit",
}

func actionsSummary(actions []*jsvalue.Object) string {
	var read, command, edit float64
	for _, a := range actions {
		switch actionKind[a.String("name")] {
		case "read":
			read++
		case "command":
			command++
		case "edit":
			edit++
		}
	}
	n := float64(len(actions))
	parts := []string{jscompat.Number(n) + " " + count(n, "agent action", "agent actions", "действие", "действия", "действий") + locale.Pick("", " агента")}
	if read > 0 {
		parts = append(parts, locale.Pick("reading and search: ", "чтение и поиск: ")+jscompat.Number(read))
	}
	if command > 0 {
		parts = append(parts, locale.Pick("commands: ", "команды: ")+jscompat.Number(command))
	}
	if edit > 0 {
		parts = append(parts, locale.Pick("edits: ", "правки: ")+jscompat.Number(edit))
	}
	return strings.Join(parts, " · ")
}

type feedItem struct {
	entry   *jsvalue.Object   // nil for a group of actions
	actions []*jsvalue.Object // consecutive actions, folded together
}

func groupFeed(entries []any) []feedItem {
	var feed []feedItem
	for _, e := range entries {
		eo, _ := e.(*jsvalue.Object)
		if eo == nil {
			eo = jsvalue.NewObject()
		}
		if eo.String("t") != "action" {
			feed = append(feed, feedItem{entry: eo})
			continue
		}
		if n := len(feed); n > 0 && feed[n-1].entry == nil {
			feed[n-1].actions = append(feed[n-1].actions, eo)
			continue
		}
		feed = append(feed, feedItem{actions: []*jsvalue.Object{eo}})
	}
	return feed
}

type diffLine struct{ sign, text string }

func diffLines(d *jsvalue.Object) []diffLine {
	var out []diffLine
	if d.String("kind") == "patch" {
		for _, l := range strings.Split(d.String("text"), "\n") {
			sign := " "
			if l != "" {
				sign = jscompat.Slice(l, 1)
			}
			out = append(out, diffLine{sign, l})
		}
		return out
	}
	if old := d.String("old"); old != "" {
		for _, l := range strings.Split(old, "\n") {
			out = append(out, diffLine{"-", "- " + l})
		}
	}
	for _, l := range strings.Split(d.String("new"), "\n") {
		out = append(out, diffLine{"+", "+ " + l})
	}
	return out
}

var lineClass = map[string]string{"-": "tw-share-diff-del", "+": "tw-share-diff-add"}

func shareDiff(d *jsvalue.Object) *hnode {
	pre := el("pre").attr("class", "tw-share-diff")
	if d.String("kind") == "replace" {
		pre.kids = append(pre.kids, txt(d.String("file")+"\n"))
	}
	for _, l := range diffLines(d) {
		span := el("span", txt(l.text+"\n"))
		if c, ok := lineClass[l.sign]; ok {
			span.attr("class", c)
		}
		pre.kids = append(pre.kids, span)
	}
	return pre
}

func timeOf(e *jsvalue.Object) string {
	f, _ := e.Number("at")
	return formatTime(f)
}

func timeSpan(e *jsvalue.Object) *hnode {
	return el("span", txt(timeOf(e))).attr("class", "tw-share-time")
}

func (p Page) markdown(text string) *hnode {
	return el("div", renderMarkdown(text, p.transform)...).attr("class", "tw-share-md")
}

// hiddenImages is the "n images hidden" label of a user message.
func hiddenImages(n float64) string {
	return count(n, "image hidden", "images hidden", "изображение скрыто", "изображения скрыты", "изображений скрыто")
}

func ownerName(s *jsvalue.Object) string {
	if sub, ok := s.Object("owner").Get("sub"); ok && sub != nil {
		return jscompat.String(sub)
	}
	return locale.Pick("Person", "Человек")
}

func (p Page) feedEntry(e, s *jsvalue.Object) *hnode {
	switch e.String("t") {
	case "user":
		author := el("div", txt(ownerName(s)+" "), timeSpan(e)).attr("class", "tw-share-author")
		art := el("article", author).attr("class", "tw-share-msg").attr("data-role", "user")
		if t := e.String("text"); t != "" {
			art.kids = append(art.kids, el("div", txt(t)).attr("class", "tw-share-text"))
		}
		if n := num(e, "images"); n > 0 {
			label := "[" + hiddenImages(n) + ": " + jscompat.Number(n) + "]"
			art.kids = append(art.kids, el("div", txt(label)).attr("class", "tw-share-muted"))
		}
		return art
	case "assistant":
		author := el("div", txt(assistantNames[s.String("provider")]+" "), timeSpan(e)).attr("class", "tw-share-author")
		return el("article", author, p.markdown(e.String("markdown"))).attr("class", "tw-share-msg").attr("data-role", "assistant")
	case "thinking":
		return el("details", el("summary", txt(locale.Pick("Model reasoning", "Размышления модели"))),
			el("div", txt(e.String("text"))).attr("class", "tw-share-fold-body tw-share-text tw-share-muted")).attr("class", "tw-share-fold")
	case "plan":
		author := el("div", txt(locale.Pick("Plan ", "План ")), timeSpan(e)).attr("class", "tw-share-author")
		return el("article", author, p.markdown(e.String("markdown"))).attr("class", "tw-share-msg tw-share-plan").attr("data-role", "plan")
	case "note":
		return el("p", txt(e.String("text"))).attr("class", "tw-share-note")
	}
	return nil
}

func actionGroup(actions []*jsvalue.Object) *hnode {
	ul := el("ul").attr("class", "tw-share-fold-body tw-share-actions")
	for _, a := range actions {
		line := el("span",
			el("span", txt(a.String("name"))).attr("class", "tw-share-action-name"),
			el("span", txt(a.String("summary"))).attr("class", "tw-share-action-summary"),
		).attr("class", "tw-share-action-line")
		li := el("li", line).attr("class", "tw-share-action")
		if st, ok := a.Get("status"); ok && st != nil {
			li.attr("data-status", jscompat.String(st))
		}
		if d := a.Object("diff"); d != nil {
			li.kids = append(li.kids, shareDiff(d))
		}
		ul.kids = append(ul.kids, li)
	}
	return el("details", el("summary", txt(actionsSummary(actions))), ul).attr("class", "tw-share-fold")
}

func footerText(o RenderOptions) string {
	parts := []string{locale.Pick("Secrets and paths are hidden automatically", "Секреты и пути скрыты автоматически")}
	if o.ExpiresAt != 0 {
		parts = append(parts, locale.Pick("link valid until ", "ссылка действует до ")+formatDate(o.ExpiresAt))
	}
	if o.Viewer != "" {
		parts = append(parts, locale.Pick("opened as ", "открыто как ")+o.Viewer)
	}
	return strings.Join(parts, " · ")
}

func metaLine(s *jsvalue.Object) string {
	var parts []string
	parts = append(parts, providerLabels[s.String("provider")])
	if m, ok := s.Get("model"); ok && truthy(m) {
		parts = append(parts, jscompat.String(m))
	}
	if sub, ok := s.Object("owner").Get("sub"); ok && truthy(sub) {
		parts = append(parts, locale.Pick("author ", "автор ")+jscompat.String(sub))
	}
	parts = append(parts, locale.Pick("snapshot ", "снимок ")+formatDateTime(num(s, "capturedAt"))+locale.Pick(" MSK", " МСК"))
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " · ")
}

// RenderHTML is the snapshot page.
func (p Page) RenderHTML(source *jsvalue.Object, r *Redactor, o RenderOptions) string {
	s := RedactSnapshot(source, r)
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="` + locale.Pick("en", "ru") + `"><head><meta charSet="utf-8"/><meta name="viewport" content="width=device-width, initial-scale=1"/>`)
	b.WriteString(`<meta name="referrer" content="no-referrer"/><meta name="robots" content="noindex, nofollow"/>`)
	b.WriteString(`<meta http-equiv="Content-Security-Policy" content="` + reactEscape(PageCSP) + `"/>`)
	b.WriteString(`<title>` + reactEscape(s.String("title")+" · "+p.Brand) + `</title><style>` + pageCSS + `</style></head><body>`)
	main := el("main").attr("class", "tw-share")
	if o.Mode == ModePreview {
		main.kids = append(main.kids, el("div", txt(locale.Pick("Preview: this is how a colleague will see the snapshot. Check that nothing extra is in it, because automatic hiding does not guarantee completeness.", "Предпросмотр: так снимок увидит коллега. Проверьте, что в нём нет лишнего — автоматическое скрытие не гарантирует полноты."))).
			attr("class", "tw-share-banner").attr("role", "note"))
	}
	top := el("div", el("span", el("span", txt(p.Brand)).attr("class", "tw-share-brand"), txt(locale.Pick(" · read-only", " · только чтение")))).attr("class", "tw-share-top")
	if o.Viewer != "" {
		top.kids = append(top.kids, el("span", txt(locale.Pick("opened: ", "открыто: ")+o.Viewer)))
	}
	head := el("header", el("h1", txt(s.String("title"))).attr("class", "tw-share-title"), el("p", txt(metaLine(s))).attr("class", "tw-share-meta")).
		attr("class", "tw-share-head")
	if econ := s.Object("economics"); econ != nil {
		head.kids = append(head.kids, el("p", txt(economicsLine(econ))).attr("class", "tw-share-meta"))
	}
	feed := el("div").attr("class", "tw-share-feed")
	entries := s.Array("entries")
	if len(entries) == 0 {
		feed.kids = append(feed.kids, el("p", txt(locale.Pick("The snapshot has no messages", "В снимке нет сообщений"))).attr("class", "tw-share-note"))
	}
	for _, item := range groupFeed(entries) {
		if item.entry == nil {
			feed.kids = append(feed.kids, actionGroup(item.actions))
		} else if n := p.feedEntry(item.entry, s); n != nil {
			feed.kids = append(feed.kids, n)
		}
	}
	main.kids = append(main.kids, top, head, feed, el("footer", txt(footerText(o))).attr("class", "tw-share-foot"))
	main.write(&b)
	b.WriteString(`</body></html>`)
	return b.String()
}

var backtickRun = regexp.MustCompile("`+")

func longestTicks(text string) int {
	n := 0
	for _, m := range backtickRun.FindAllString(text, -1) {
		n = max(n, len(m))
	}
	return n
}

func fence(text string) string { return strings.Repeat("`", max(3, longestTicks(text)+1)) }

func inlineCode(text string) string {
	ticks := strings.Repeat("`", max(1, longestTicks(text)+1))
	return ticks + " " + text + " " + ticks
}

func quote(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

func markdownDiff(d *jsvalue.Object) string {
	var lines []string
	for _, l := range diffLines(d) {
		lines = append(lines, l.text)
	}
	body := strings.Join(lines, "\n")
	text := body
	if d.String("kind") == "replace" {
		text = d.String("file") + "\n" + body
	}
	marks := fence(text)
	return marks + "diff\n" + text + "\n" + marks
}

// indentLines is .replace(/^/gm, '  '): every line, empty ones too, after any line terminator JavaScript knows.
func indentLines(s string) string {
	var b strings.Builder
	b.WriteString("  ")
	for _, r := range s {
		b.WriteRune(r)
		if r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 {
			b.WriteString("  ")
		}
	}
	return b.String()
}

// RenderMarkdown is the snapshot as Markdown, for the .md download; redacted again like the page.
func (p Page) RenderMarkdown(source *jsvalue.Object, r *Redactor) string {
	s := RedactSnapshot(source, r)
	blocks := []string{"# " + strings.ReplaceAll(s.String("title"), "\n", " "), "_" + metaLine(s) + "_"}
	if econ := s.Object("economics"); econ != nil {
		blocks = append(blocks, "_"+economicsLine(econ)+"_")
	}
	blocks = append(blocks, "---")
	for _, item := range groupFeed(s.Array("entries")) {
		if item.entry == nil {
			var lines []string
			for _, a := range item.actions {
				line := "- **" + a.String("name") + "** " + inlineCode(a.String("summary"))
				if d := a.Object("diff"); d != nil {
					line += "\n\n" + indentLines(markdownDiff(d))
				}
				lines = append(lines, line)
			}
			blocks = append(blocks, "<details><summary>"+actionsSummary(item.actions)+"</summary>\n\n"+strings.Join(lines, "\n")+"\n\n</details>")
			continue
		}
		e := item.entry
		t := timeOf(e)
		switch e.String("t") {
		case "user":
			images := ""
			if n := num(e, "images"); n > 0 {
				images = "\n>\n> _[" + hiddenImages(n) + ": " + jscompat.Number(n) + "]_"
			}
			blocks = append(blocks, "**"+ownerName(s)+"** · "+t+"\n\n"+quote(e.String("text"))+images)
		case "assistant":
			blocks = append(blocks, "**"+assistantNames[s.String("provider")]+"** · "+t+"\n\n"+e.String("markdown"))
		case "thinking":
			blocks = append(blocks, "<details><summary>"+locale.Pick("Model reasoning", "Размышления модели")+"</summary>\n\n"+quote(e.String("text"))+"\n\n</details>")
		case "plan":
			blocks = append(blocks, "**"+locale.Pick("Plan", "План")+"** · "+t+"\n\n"+e.String("markdown"))
		case "note":
			blocks = append(blocks, "_"+e.String("text")+"_")
		}
	}
	blocks = append(blocks, "---", "_"+footerText(RenderOptions{Mode: ModeExport})+"_")
	return strings.Join(blocks, "\n\n") + "\n"
}
