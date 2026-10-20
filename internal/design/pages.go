package design

import (
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")

func esc(s string) string { return htmlEscaper.Replace(s) }

var moscow = func() *time.Location {
	if loc, err := time.LoadLocation("Europe/Moscow"); err == nil {
		return loc
	}
	return time.FixedZone("MSK", 3*3600)
}()

// when is a time as the pages show it: day, month, hours and minutes in Moscow.
func when(ms int64) string {
	if ms <= 0 {
		return "—"
	}
	return time.UnixMilli(ms).In(moscow).Format("02.01, 15:04")
}

func plural(n int, forms [3]string) string {
	mod10, mod100 := n%10, n%100
	if mod10 == 1 && mod100 != 11 {
		return forms[0]
	}
	if mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14) {
		return forms[1]
	}
	return forms[2]
}

func boardsLabel(n int) string {
	if !locale.Russian() {
		if n == 1 {
			return "1 board"
		}
		return strconv.Itoa(n) + " boards"
	}
	return strconv.Itoa(n) + " " + plural(n, [3]string{"доска", "доски", "досок"})
}

func (l *Library) shell(title, body, script string) string {
	tail := ""
	if script != "" {
		tail = "<script>" + script + "</script>"
	}
	return `<!doctype html><html lang="` + locale.Pick("en", "ru") + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>` +
		esc(title) + `</title><style>` + l.text("library.css") + `</style></head><body><main>` + body + `</main>` + tail + `</body></html>`
}

func (l *Library) indexPage(artifacts []*Artifact, root string) string {
	var cards strings.Builder
	for _, a := range artifacts {
		cards.WriteString(`<li><a class="card" href="/a/` + esc(a.ID) + `/"><span><b>` + esc(a.Title) + `</b></span><small>` +
			boardsLabel(len(a.Boards)) + ` · ` + when(a.UpdatedAt) + `</small></a></li>`)
	}
	body := `<h1>Claude Design</h1><p class="hint">` + locale.Pick("Folder: ", "Папка: ") + esc(root) + `</p>`
	if len(artifacts) > 0 {
		body += `<ul>` + cards.String() + `</ul>`
	} else {
		body += `<p class="empty">` + locale.Pick("Nothing here yet. Put a Claude Design project in the folder above or import an archive.", "Пока ничего нет. Положите проект Claude Design в папку выше или импортируйте архив.") + `</p>`
	}
	return l.shell("Claude Design", body, "")
}

func (l *Library) artifactPage(a *Artifact) string {
	var rows strings.Builder
	for _, b := range a.Boards {
		href := BoardHref(a.ID, b.Path)
		light := ""
		if strings.Contains(b.Path, "theme=dark") {
			light = `<a class="alt" href="` + esc(BoardHref(a.ID, strings.Replace(b.Path, "theme=dark", "theme=light", 1))) + `" title="` + locale.Pick("Light theme", "Светлая тема") + `">☀</a>`
		}
		rows.WriteString(`<li class="row" data-title="` + esc(strings.ToLower(b.Title)) + `"><a class="open" href="` + esc(href) + `">` + esc(b.Title) + `</a>` + light + `</li>`)
	}
	source := ""
	if a.Source != nil {
		source = " · " + esc(*a.Source)
	}
	body := `<a class="back" href="/">` + locale.Pick("← All designs", "← Все макеты") + `</a><h1>` + esc(a.Title) + `</h1><p class="hint">` + boardsLabel(len(a.Boards)) + locale.Pick(" · updated ", " · обновлён ") + when(a.UpdatedAt) + source + `</p>`
	if len(a.Boards) > 12 {
		body += `<input class="filter" type="search" placeholder="` + locale.Pick("Find a board", "Найти доску") + `" aria-label="` + locale.Pick("Find a board", "Найти доску") + `" autocomplete="off">`
	}
	if len(a.Boards) > 0 {
		body += `<ul>` + rows.String() + `</ul>`
	} else {
		body += `<p class="empty">` + locale.Pick("The project has no boards: waiting for the first *.dc.html.", "В проекте нет досок: ждём первые *.dc.html.") + `</p>`
	}
	return l.shell(a.Title, body, l.text("filter.js"))
}

func (l *Library) notFoundPage() string {
	return l.shell(locale.Pick("Not found", "Не найдено"), `<a class="back" href="/">`+locale.Pick("← All designs", "← Все макеты")+`</a><p class="empty">`+locale.Pick("There is no such design.", "Такого макета нет.")+`</p>`, "")
}

func (l *Library) clientScript() string { return l.text("client.js") }
