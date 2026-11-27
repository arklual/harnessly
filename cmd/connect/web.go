package main

import (
	"crypto/rand"
	"encoding/hex"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
)

// The window: a page on a loopback port with a secret path, opened in the person's browser. It pairs the computer
// (address and code), shows the state, links and unlinks.
type window struct {
	a    *app
	url  string
	path string
	// autostart is setAutostart (a test gives its own).
	autostart func(bool) error
	isOn      func() bool
}

func startWindow(a *app) (*window, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	w := &window{a: a, path: "/" + hex.EncodeToString(b) + "/", autostart: setAutostart, isOn: autostartOn}
	w.url = "http://" + ln.Addr().String() + w.path
	mux := http.NewServeMux()
	mux.HandleFunc(w.path, w.serve)
	go func() { _ = http.Serve(ln, mux) }()
	return w, nil
}

var page = template.Must(template.New("p").Parse(`<!doctype html><meta charset="utf-8">
<meta name="viewport" content="width=device-width">
<title>Harnessly Connect</title>
<style>
:root{color-scheme:light dark;--bg:#f6f5f2;--fg:#1d1d1b;--mut:#6b6a66;--card:#fff;--line:#e2e0da;--ok:#2f7d4f;--bad:#b3412f;--btn:#1d1d1b;--btnfg:#fff}
@media(prefers-color-scheme:dark){:root{--bg:#171716;--fg:#ecebe7;--mut:#9a9893;--card:#21211f;--line:#34332f;--btn:#ecebe7;--btnfg:#171716}}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
main{max-width:30rem;margin:0 auto;padding:2.5rem 1rem}
h1{font-size:1.25rem;margin:0 0 1.5rem}
.card{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:1.25rem;margin-bottom:1rem}
label{display:block;font-size:.85rem;color:var(--mut);margin:.75rem 0 .25rem}
input{width:100%;box-sizing:border-box;padding:.55rem .65rem;border:1px solid var(--line);border-radius:7px;background:transparent;color:inherit;font:inherit}
button{margin-top:1rem;padding:.55rem 1rem;border:0;border-radius:7px;background:var(--btn);color:var(--btnfg);font:inherit;cursor:pointer}
button.quiet{background:transparent;color:var(--fg);border:1px solid var(--line)}
.state{font-weight:600}.on{color:var(--ok)}.off{color:var(--bad)}
.mut{color:var(--mut);font-size:.85rem}.err{color:var(--bad)}
code{font-size:.85rem;word-break:break-all}
form{display:inline}
</style>
<main>
<h1>Harnessly Connect</h1>
{{if .Err}}<p class="err">{{.Err}}</p>{{end}}
{{if not .Paired}}
<div class="card">
<p>Агенты harnessly будут работать с файлами и командами этого компьютера. Код возьмите в harnessly: <code>harnessly connect code</code>.</p>
<form method="post"><input type="hidden" name="do" value="pair">
<label>Адрес harnessly</label><input name="url" placeholder="https://harnessly.example.com" required>
<label>Код</label><input name="code" placeholder="hx_…" required>
<label>Имя компьютера</label><input name="name" value="{{.Host}}">
<button>Подключить</button></form>
</div>
{{else}}
<div class="card">
<div>{{.Name}} → {{.URL}}</div>
{{if .Online}}<p class="state on">Онлайн</p>{{else if .Running}}<p class="state off">Нет связи: {{.Why}}</p>{{else}}<p class="state off">Отключено</p>{{end}}
<p class="mut">Команд агентов сейчас: {{.Commands}}</p>
{{if .Running}}<form method="post"><input type="hidden" name="do" value="stop"><button>Отключить и остановить всё</button></form>
{{else}}<form method="post"><input type="hidden" name="do" value="start"><button>Подключить</button></form>{{end}}
</div>
<div class="card">
<p class="mut">Журнал команд и изменений файлов: <code>{{.Journal}}</code></p>
<p class="mut">Автозапуск при входе: {{if .Autostart}}включён{{else}}выключен{{end}}</p>
<form method="post"><input type="hidden" name="do" value="autostart"><button class="quiet">{{if .Autostart}}Выключить{{else}}Включить{{end}} автозапуск</button></form>
<form method="post"><input type="hidden" name="do" value="forget"><button class="quiet">Забыть это подключение</button></form>
</div>
{{end}}
</main>`))

func (w *window) serve(rw http.ResponseWriter, r *http.Request) {
	msg := ""
	if r.Method == http.MethodPost {
		// Same-origin only: a page of another site cannot press the buttons.
		if o := r.Header.Get("Origin"); o != "" && o+w.path != w.url {
			http.Error(rw, "forbidden", http.StatusForbidden)
			return
		}
		switch r.FormValue("do") {
		case "pair":
			if _, err := pair(r.FormValue("url"), r.FormValue("code"), r.FormValue("name")); err != nil {
				msg = err.Error()
			} else {
				_ = w.a.start()
				_ = w.autostart(true)
			}
		case "start":
			if err := w.a.start(); err != nil {
				msg = err.Error()
			}
		case "stop":
			w.a.stop()
		case "forget":
			w.a.stop()
			_ = os.Remove(configPath())
		case "autostart":
			if err := w.autostart(!w.isOn()); err != nil {
				msg = err.Error()
			}
		}
		if msg == "" {
			http.Redirect(rw, r, w.path, http.StatusSeeOther)
			return
		}
	}
	paired, name, url, running, online, why, commands := w.a.state()
	host, _ := os.Hostname()
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(rw, map[string]any{"Err": msg, "Paired": paired, "Name": name, "URL": url, "Running": running,
		"Online": online, "Why": why, "Commands": commands, "Host": host, "Journal": journalPath(), "Autostart": w.isOn()})
}

// openURL opens a page or a file with what the system opens it with.
func openURL(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}
