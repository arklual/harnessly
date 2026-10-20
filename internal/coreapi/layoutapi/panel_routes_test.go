package layoutapi

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/panel"
)

const agentSession = "pt-ws-1-aaa"

func panelRig(t *testing.T) *rig {
	r := newRig(t)
	r.status.agentTabs[agentSession] = "tab-1"
	return r
}

func (r *rig) ask(body string) answer { return r.do("t", "POST", "/api/cli/panel", body) }

// answerWith makes the page answer every pane command it is sent.
func (r *rig) answerWith(ok bool, message string) {
	r.sync.onEvent = func(e *jsvalue.Object) {
		if e.String("type") == "panel-command" {
			go r.deps.Panel.Answer(e.String("id"), panel.Result{OK: ok, Message: message})
		}
	}
}

// Ported from the previous runtime's panel command route tests.
func TestCLIPanel(t *testing.T) {
	r := panelRig(t)
	r.want(r.do("", "POST", "/api/cli/panel", `{"session":"`+agentSession+`","command":{"kind":"status"}}`), 403, `{"error":"Forbidden"}`)
	a := r.do("t", "GET", "/api/cli/panel", "")
	r.want(a, 405, "")
	if a.header.Get("Allow") != "POST" || a.header.Get("Cache-Control") != "no-store" {
		t.Fatal(a.header)
	}
	r.want(r.do("t", "POST", "/api/cli/panel", `{"session":"`+agentSession+`","command":{"kind":"status"}}`, "Content-Type", "text/plain"), 415, `{"error":"Нужен application/json"}`)
	for _, s := range []string{`""`, `5`, `"a b"`, `"x;rm"`, `"` + strings.Repeat("a", 200) + `"`, `"../x"`, `null`} {
		r.want(r.ask(`{"session":`+s+`,"command":{"kind":"status"}}`), 400, `{"error":"Нужна сессия вкладки агента (tmux)"}`)
	}
	a = r.ask(`{"session":"` + agentSession + `","command":{"kind":"exec","cmd":"ls"}}`)
	if a.status != 400 || !strings.Contains(a.body, "Неизвестная команда") {
		t.Fatal(a.body)
	}
	r.want(r.ask(`{"session":"`+agentSession+`","command":{"kind":"open","tab":"browser","target":"https://evil.example"}}`), 400, "*")
	r.want(r.ask(`{"session":"pt-ws-1-zzz","command":{"kind":"status"}}`), 404, "*")
}

func TestCLIPanelAddressesTheSessionsTab(t *testing.T) {
	r := panelRig(t)
	var mu sync.Mutex
	var published []string
	r.sync.onEvent = func(e *jsvalue.Object) {
		mu.Lock()
		published = append(published, e.String("tabId"))
		mu.Unlock()
		go r.deps.Panel.Answer(e.String("id"), panel.Result{OK: true, Message: "ok"})
	}
	r.want(r.ask(`{"session":"`+agentSession+`","tabId":"tab-evil","command":{"kind":"status","tabId":"tab-evil"}}`), 200, "*")
	if len(published) != 1 || published[0] != "tab-1" {
		t.Fatal(published)
	}
}

func TestCLIPanelKeepsOpenWithTheConversation(t *testing.T) {
	r := panelRig(t)
	r.sync.clients = 0
	a := r.ask(`{"session":"` + agentSession + `","command":{"kind":"open","tab":"artifacts","target":"https://a.example.org/a/plan"}}`)
	r.want(a, 200, "*")
	o := a.json(t)
	if applied, _ := o.Get("applied"); applied != false || !strings.Contains(o.String("message"), "откроется, когда человек придёт") {
		t.Fatal(a.body)
	}
	intents := r.deps.Intents.List("tab-1")
	if len(intents) != 1 || string(jsvalue.Stringify(intents[0].Object("command"), "")) != `{"kind":"open","tab":"artifacts","target":"/agents/docs/plan"}` {
		t.Fatal(intents)
	}
	if o.String("message") != "Вкладка «artifacts» (/agents/docs/plan): записано за разговором, откроется, когда человек придёт в разговор" {
		t.Fatal(o.String("message"))
	}
	// The page of the conversation applies it at once; the wait is a real one, long enough for a loaded runner.
	r.deps.PanelWait = 2 * time.Second
	r.sync.clients = 1
	r.sync.onEvent = func(e *jsvalue.Object) {
		if e.String("type") == "pane-intents" {
			go func() {
				var ids []string
				for _, it := range r.deps.Intents.List(e.String("tabId")) {
					ids = append(ids, it.String("id"))
				}
				r.deps.Intents.MarkApplied(e.String("tabId"), ids, "desktop")
			}()
		}
	}
	a = r.ask(`{"session":"` + agentSession + `","command":{"kind":"open","tab":"browser","target":"4400"}}`)
	if o := a.json(t); o.String("message") != "Вкладка «browser» (localhost:4400): открыто у человека" {
		t.Fatal(a.body)
	}
	r.sync.onEvent = nil
	r.sync.events = nil
	r.ask(`{"session":"` + agentSession + `","command":{"kind":"show"}}`)
	if len(r.sync.events) != 1 || string(jsvalue.Stringify(r.sync.events[0], "")) != `{"type":"pane-intents","tabId":"tab-1"}` {
		t.Fatal(r.sync.events)
	}
}

func TestCLIPanelHideAndStatus(t *testing.T) {
	r := panelRig(t)
	r.answerWith(false, "Панель занята")
	r.want(r.ask(`{"session":"`+agentSession+`","command":{"kind":"hide"}}`), 409, `{"ok":false,"error":"Панель занята"}`)
	r.sync.onEvent = nil
	r.sync.clients = 0
	r.want(r.ask(`{"session":"`+agentSession+`","command":{"kind":"hide"}}`), 200, `{"ok":true,"message":"Страница с этим разговором сейчас не открыта: скрывать нечего"}`)

	r2 := panelRig(t)
	r2.deps.Intents.Add("tab-1", jsvalue.NewObject("kind", "open", "tab", "task", "target", "TCK-3004"))
	a := r2.ask(`{"session":"` + agentSession + `","command":{"kind":"status"}}`)
	o := a.json(t)
	if a.status != 200 || o.String("message") != "Страница с этим разговором сейчас не открыта" || !strings.Contains(o.Array("intents")[0].(string), "ждёт") {
		t.Fatal(a.body)
	}
	r3 := panelRig(t)
	r3.want(r3.ask(`{"session":"`+agentSession+`","command":{"kind":"status"}}`), 200,
		`{"ok":true,"message":"Страница с этим разговором сейчас не открыта","intents":["Команд панели за сутки не было"]}`)
}

func TestCLIPanelRateLimit(t *testing.T) {
	r := panelRig(t)
	r.answerWith(true, "ok")
	last := 0
	for i := 0; i < 13; i++ {
		last = r.ask(`{"session":"` + agentSession + `","command":{"kind":"status"}}`).status
	}
	if last != 429 {
		t.Fatal(last)
	}
}

func TestPanelResult(t *testing.T) {
	r := panelRig(t)
	r.want(r.do("t", "POST", "/api/platform/panel-result", `{"id":"x","ok":true,"message":"x"}`), 403, `{"error":"Forbidden"}`)
	r.want(r.do("s", "GET", "/api/platform/panel-result", ""), 405, "")
	r.want(r.do("s", "POST", "/api/platform/panel-result", `{}`, "Content-Type", "text/plain"), 415, "*")
	r.want(r.do("s", "POST", "/api/platform/panel-result", `{"id":"00000000-0000-4000-8000-000000000000","ok":"yes","message":"x"}`), 400, `{"error":"Некорректный ответ"}`)
	r.want(r.do("s", "POST", "/api/platform/panel-result", `{"ok":true,"message":"x"}`), 400, "*")
	r.want(r.do("s", "POST", "/api/platform/panel-result", `{"id":"00000000-0000-4000-8000-000000000000","ok":true,"message":"x"}`), 404, `{"error":"Команда уже выполнена или не найдена"}`)

	r.deps.PanelWait = 2 * time.Second
	ids := make(chan string, 1)
	r.sync.onEvent = func(e *jsvalue.Object) { ids <- e.String("id") }
	done := make(chan answer)
	go func() { done <- r.ask(`{"session":"` + agentSession + `","command":{"kind":"status"}}`) }()
	id := <-ids
	r.want(r.do("s", "POST", "/api/platform/panel-result", `{"id":"`+id+`","ok":true,"message":"Готово\u001b[201~\r"}`), 200, `{"accepted":true}`)
	got := <-done
	if got.status != 200 || got.json(t).String("message") != "Готово␛[201~" {
		t.Fatal(got.body)
	}
	r.want(r.do("s", "POST", "/api/platform/panel-result", `{"id":"`+id+`","ok":false,"message":"late"}`), 404, "*")
}

func TestPaneIntentsRoute(t *testing.T) {
	r := panelRig(t)
	const id = "00000000-0000-4000-8000-000000000000"
	r.want(r.do("", "GET", "/api/platform/pane-intents?tabId=tab-1", ""), 401, "*")
	intent := r.deps.Intents.Add("tab-1", jsvalue.NewObject("kind", "open", "tab", "task", "target", "TCK-1"))
	listed := r.do("s", "GET", "/api/platform/pane-intents?tabId=tab-1", "")
	r.want(listed, 200, `{"intents":[`+string(jsvalue.Stringify(intent, ""))+`]}`)
	if listed.header.Get("Cache-Control") != "no-store" {
		t.Fatal(listed.header)
	}
	marked := r.do("s", "POST", "/api/platform/pane-intents", `{"tabId":"tab-1","ids":["`+intent.String("id")+`"],"device":"phone"}`)
	if marked.status != 200 || marked.json(t).Array("intents")[0].(*jsvalue.Object).String("appliedOn") != "phone" {
		t.Fatal(marked.body)
	}
	r.want(r.do("s", "GET", "/api/platform/pane-intents?tabId=../x%20y", ""), 400, `{"error":"Нужен tabId"}`)
	r.want(r.do("s", "POST", "/api/platform/pane-intents", `{"tabId":"tab-1","ids":["nope"],"device":"desktop"}`), 400, `{"error":"Нужны id намерений"}`)
	r.want(r.do("s", "POST", "/api/platform/pane-intents", `{"tabId":"tab-1","ids":["`+id+`"],"device":"tv"}`), 400, `{"error":"device: desktop или phone"}`)
	r.want(r.do("s", "DELETE", "/api/platform/pane-intents", ""), 405, "")
}

func TestCLIVaultAndResult(t *testing.T) {
	r := panelRig(t)
	r.want(r.do("t", "POST", "/api/cli/vault", `{"session":"`+agentSession+`","origin":"http://x"}`), 400, `{"error":"Адрес сайта: только https://хост[:порт], без пути и логина"}`)
	r.want(r.do("t", "POST", "/api/cli/vault", `{"session":"pt-ws-1-zzz","origin":"https://x.example.org"}`), 404,
		`{"error":"Эта сессия не принадлежит вкладке агента: входить просит агент из своей вкладки"}`)
	r.sync.clients = 0
	r.want(r.do("t", "POST", "/api/cli/vault", `{"session":"`+agentSession+`","origin":"https://x.example.org"}`), 503, "*")
	r.sync.clients = 1
	ids := make(chan string, 1)
	r.sync.onEvent = func(e *jsvalue.Object) {
		if e.String("origin") != "https://x.example.org" {
			t.Error(e)
		}
		ids <- e.String("id")
	}
	done := make(chan answer)
	go func() {
		done <- r.do("t", "POST", "/api/cli/vault", `{"session":"`+agentSession+`","origin":"https://X.example.org/"}`)
	}()
	id := <-ids
	r.want(r.do("s", "POST", "/api/platform/vault-result", `{"id":"`+id+`","status":"hacked","message":"x"}`), 400, `{"error":"Некорректный ответ"}`)
	r.want(r.do("s", "POST", "/api/platform/vault-result", `{"id":"`+id+`","status":"logged-in","message":"Вошёл\n"}`), 200, `{"accepted":true}`)
	r.want(<-done, 200, `{"status":"logged-in","message":"Вошёл"}`)
	r.want(r.do("s", "POST", "/api/platform/vault-result", `{"id":"`+id+`","status":"denied","message":"x"}`), 404, `{"error":"Просьба уже закрыта или не найдена"}`)
}
