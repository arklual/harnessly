package prompts

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func body(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}

// asJSON is a response body as plain JSON values.
func asJSON(t *testing.T, r Response) map[string]any {
	t.Helper()
	if r.Body == nil {
		return nil
	}
	raw, err := json.Marshal(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func expectError(t *testing.T, name string, r Response, status int, message string) {
	t.Helper()
	if got := asJSON(t, r); r.Status != status || got["error"] != message {
		t.Errorf("%s: %d %v, want %d %q", name, r.Status, got, status, message)
	}
}

func TestPromptQueueRoute(t *testing.T) {
	r := newRig(t, tui(t, "claude-busy-spinner.ansi.txt"), Tab{CliState: StateBusy})
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return nil, false }

	// Enter to a working agent queues: the message waits for the end of its turn.
	waits := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "после хода", "images": []any{}}))
	if w := asJSON(t, waits); waits.Status != 200 || w["delivery"] != "queued" {
		t.Fatalf("add, Enter: %d %v", waits.Status, w)
	}
	r.svc.Queue().Take("tab-1", w0(t, waits))
	// ⌘↵ ("now") hands it to the working agent in its turn.
	res := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "и проверь ещё логи", "images": []any{}, "now": true}))
	got := asJSON(t, res)
	item, _ := got["item"].(map[string]any)
	if res.Status != 200 || got["delivery"] != "steered" || item["text"] != "и проверь ещё логи" {
		t.Fatalf("add: %d %v", res.Status, got)
	}

	// One the agent cannot take now is reported queued and stays.
	r.panes.setScreen(tui(t, "claude-input-draft.ansi.txt"))
	res = r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "после хода", "images": []any{3, "/uploads/a.png"}}))
	if got := asJSON(t, res); got["delivery"] != "queued" || !reflect.DeepEqual(queueTexts(r, "tab-1"), []string{"после хода"}) {
		t.Fatalf("queued: %v", got)
	}
	queued := r.svc.Queue().List("tab-1")[0]
	if !reflect.DeepEqual(queued.Images, []string{"/uploads/a.png"}) {
		t.Errorf("non-string images kept: %v", queued.Images)
	}

	// Pictures that are not this machine's uploads are refused before anything reaches the agent.
	before := len(r.panes.sent())
	expectError(t, "foreign picture", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "x", "images": []any{"/etc/passwd"}})), 400, "Картинка не из загрузок")
	if len(r.panes.sent()) != before {
		t.Error("typed after a refused picture")
	}

	expectError(t, "bad tab", r.svc.PromptQueue(body(map[string]any{"tabId": "a b", "action": "add"})), 400, "Нужны tabId и action")
	expectError(t, "bad action", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "drop"})), 400, "Нужны tabId и action")
	expectError(t, "not an object", r.svc.PromptQueue([]byte(`[1]`)), 400, "Нужны tabId и action")
	expectError(t, "unknown tab", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-9", "action": "add", "text": "x"})), 404, msgNoTab.String())
	expectError(t, "blank", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "  "})), 409, "Пустое сообщение")
	expectError(t, "no id", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "take"})), 400, "Нужен id")
	expectError(t, "steer blocked", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "steer", "id": queued.ID})), 409, steerErrors[OutcomeBlocked].String())

	res = r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "take", "id": queued.ID}))
	if got := asJSON(t, res); res.Status != 200 || got["item"].(map[string]any)["id"] != queued.ID {
		t.Fatalf("take: %v", got)
	}
	expectError(t, "taken twice", r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "remove", "id": queued.ID})), 404, "Сообщения уже нет в очереди")

	r.panes.setScreen(tui(t, "claude-idle-placeholder.ansi.txt"))
	it, _ := r.svc.Queue().Add("tab-1", "сейчас", nil)
	if res := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "steer", "id": it.ID})); res.Status != 200 || asJSON(t, res)["ok"] != true {
		t.Fatalf("steer: %v", asJSON(t, res))
	}
}

func TestPromptTextRoute(t *testing.T) {
	r := newRig(t, "❯ ", Tab{})
	expectError(t, "empty", r.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": ""})), 400, "Нужны tabId и text")
	expectError(t, "long", r.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": strings.Repeat("x", PromptTextMax+1)})), 413, "Сообщение слишком длинное")
	expectError(t, "unknown tab", r.svc.PromptText(body(map[string]any{"tabId": "tab-9", "text": "x"})), 404, msgNoTab.String())
	res := r.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": "a\nb", "messageId": "m1"}))
	got := asJSON(t, res)
	if res.Status != 200 || got["via"] != "paste" {
		t.Fatalf("typed: %v", got)
	}
	if steps, _ := got["steps"].(map[string]any); len(steps) != 2 {
		t.Errorf("steps %v", got["steps"])
	}
	if got := asJSON(t, r.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": "a\nb", "messageId": "m1"}))); got["via"] != "duplicate" {
		t.Errorf("duplicate: %v", got)
	}
	r.panes.failOn = "paste:"
	expectError(t, "tmux failed", r.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": "x\ny"})), 500, "Не удалось передать сообщение агенту")

	codex := newRig(t, "› Stuck\n", Tab{Provider: ProviderCodex})
	expectError(t, "codex unsubmitted", codex.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": "x", "submit": false})), 400, "Codex принимает сообщение только с отправкой")
	expectError(t, "codex kept it", codex.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": "Stuck"})), 500, "Агент не принял сообщение — оно осталось в его поле ввода")
}

func TestSendNowRoute(t *testing.T) {
	r := newRig(t, tui(t, "claude-2.1.296-queued-stream.ansi.txt"), Tab{CliState: StateBusy, TranscriptPath: "/t.jsonl"})
	stream := transcript(t, "claude-2.1.296-send-now-stream.jsonl")
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return stream[:2], true }
	expectError(t, "blank", r.svc.SendNowRoute(body(map[string]any{"tabId": "tab-1", "text": "  "})), 400, "Нужны tabId и text")
	expectError(t, "unknown", r.svc.SendNowRoute(body(map[string]any{"tabId": "tab-9", "text": "x"})), 404, msgNoTab.String())
	res := r.svc.SendNowRoute(body(map[string]any{"tabId": "tab-1", "text": "Then also say QUEUED-MSG-3 right after STORY-END"}))
	if got := asJSON(t, res); res.Status != 409 || got["waits"] != true || got["error"] != msgWriting.String() {
		t.Fatalf("writing: %v", got)
	}
	r.svc.cfg.ReadTranscript = func(string) ([]string, bool) { return stream, true }
	res = r.svc.SendNowRoute(body(map[string]any{"tabId": "tab-1", "text": "Then also say QUEUED-MSG-3 right after STORY-END"}))
	if got := asJSON(t, res); res.Status != 200 || got["outcome"] != "delivered" {
		t.Fatalf("delivered: %v", got)
	}
}

func TestInterruptRoute(t *testing.T) {
	r := newRig(t, "", Tab{})
	expectError(t, "no tab", r.svc.InterruptRoute([]byte(`{}`)), 400, "Нужен tabId")
	expectError(t, "unknown", r.svc.InterruptRoute(body(map[string]any{"tabId": "tab-9"})), 404, msgNoTab.String())
	if res := r.svc.InterruptRoute(body(map[string]any{"tabId": "tab-1"})); res.Status != 204 || res.Body != nil {
		t.Fatalf("got %+v", res)
	}
}

func TestSwitchModelRoute(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r := newRig(t, idle, Tab{})
	expectError(t, "bad model", r.svc.SwitchModel(body(map[string]any{"session": testSession, "model": "x'; rm"})), 400, "Неизвестная модель")

	confirm := tui(t, "claude-switch-model-confirm.txt")
	applied := strings.Replace(tui(t, "claude-default-start.txt"), "Opus 5.5", "Opus 5.5\n  ⎿  Set model to Sonnet 5", 1)
	r.panes.onSend = func(call string) {
		switch call {
		case "line:/model sonnet":
			r.panes.setScreen(confirm)
		case "keys:Enter":
			r.panes.setScreen(applied)
		}
	}
	res := r.svc.SwitchModel(body(map[string]any{"session": testSession, "model": "sonnet"}))
	if got := asJSON(t, res); res.Status != 200 || got["ok"] != true || got["confirmed"] != true {
		t.Fatalf("confirmed: %d %v", res.Status, got)
	}
	if ev := r.events.events(); ev[0] != "launch:"+testSession+":model=sonnet" {
		t.Errorf("events %v", ev)
	}

	// Another menu goes to the page as needs-input.
	r.panes.onSend = nil
	r.panes.setScreen(idle)
	r.panes.onSend = func(call string) {
		if call == "line:/model opus" {
			r.panes.setScreen(strings.Replace(confirm, "Switch model?", "Pick one", 1))
		}
	}
	r.events.menuMoves = true
	expectError(t, "menu", r.svc.SwitchModel(body(map[string]any{"session": testSession, "model": "opus"})), 409, msgMenu.String())

	r.panes.current = "zsh"
	res = r.svc.SwitchModel(body(map[string]any{"session": testSession, "model": "opus"}))
	if got := asJSON(t, res); res.Status != 409 || got["code"] != RunNotRunning {
		t.Errorf("shell: %v", got)
	}
}

func TestRunCommandRouteValidation(t *testing.T) {
	r := newRig(t, "", Tab{})
	for name, c := range map[string]struct {
		in      any
		message string
	}{
		"no session":   {map[string]any{"provider": "claude", "line": "/x"}, "Нужна сессия"},
		"bad provider": {map[string]any{"session": "s", "provider": "gpt", "line": "/x"}, "Неизвестный агент"},
		"no line":      {map[string]any{"session": "s", "provider": "codex", "line": 5}, "Нужна строка команды"},
	} {
		res := r.svc.RunCommandRoute(body(c.in))
		if got := asJSON(t, res); res.Status != 400 || got["error"] != "invalid" || got["message"] != c.message {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestCommandScreenRoutes(t *testing.T) {
	r := newRig(t, tui(t, "codex-model-step1.txt"), Tab{})
	expectError(t, "bad session", r.svc.CommandScreen("a:b"), 400, "Неверная сессия")
	expectError(t, "gone", r.svc.CommandScreen("hs-zz"), 404, "Сессия не найдена")
	res := r.svc.CommandScreen(testSession)
	got := asJSON(t, res)
	if res.Status != 200 || got["menu"].(map[string]any)["title"] != "Select Model and Effort" {
		t.Fatalf("screen: %v", got)
	}
	expectError(t, "text key", r.svc.CommandScreenKey(body(map[string]any{"session": testSession, "key": "rm"})), 400, "Эту клавишу из веба нажать нельзя")
	res = r.svc.CommandScreenKey(body(map[string]any{"session": testSession, "key": "Down"}))
	if res.Status != 200 || !reflect.DeepEqual(r.panes.sent(), []string{"keys:Down"}) || r.clock.slept[len(r.clock.slept)-1] != mirrorKeySettle {
		t.Fatalf("key: %v", r.panes.sent())
	}
	r.panes.current = "bash"
	expectError(t, "shell", r.svc.CommandScreenKey(body(map[string]any{"session": testSession, "key": "Down"})), 409, "Агент в этом диалоге не запущен")
}

func TestRawRoutes(t *testing.T) {
	r := newRig(t, tui(t, "claude-switch-model-confirm.txt"), Tab{})
	expectError(t, "send-input empty", r.svc.SendInput(body(map[string]any{"session": testSession})), 400, "session and input parameters required")
	expectError(t, "foreign session", r.svc.SendInput(body(map[string]any{"session": "person-main", "input": "q"})), 404, "Session not found")
	if res := r.svc.SendInput(body(map[string]any{"session": testSession, "input": "C-c"})); res.Status != 200 || !reflect.DeepEqual(r.panes.sent(), []string{"keys:C-c"}) {
		t.Fatalf("send-input: %v", r.panes.sent())
	}
	expectError(t, "capture empty", r.svc.Capture(""), 400, "session parameter required")
	if got := asJSON(t, r.svc.Capture(testSession)); !strings.Contains(got["content"].(string), "Switch model?") {
		t.Errorf("capture %v", got)
	}
	got := asJSON(t, r.svc.PermissionOptions(testSession))
	if opts, _ := got["options"].([]any); len(opts) != 2 || r.panes.atWidths != 1 {
		t.Errorf("permission options %v", got)
	}
	if got := asJSON(t, r.svc.PlanOptions(testSession)); !reflect.DeepEqual(got["options"], []any{"Yes, switch to Sonnet 5", "No, go back"}) {
		t.Errorf("plan options %v", got)
	}
	r.panes.setScreen("")
	if got := asJSON(t, r.svc.PermissionOptions(testSession)); !reflect.DeepEqual(got, map[string]any{"options": []any{}}) {
		t.Errorf("empty capture %v", got)
	}
}

// The AskUserQuestion route (answer-question): keys for every layout, nothing typed without a question on screen.
func TestAnswerQuestionRoute(t *testing.T) {
	question := tui(t, "claude-ask-preview-question.txt")
	idle := tui(t, "claude-ask-answered-idle.txt")
	setup := func(takes bool) *rig {
		r := newRig(t, question, Tab{})
		r.panes.onSend = func(string) {
			if takes {
				r.panes.setScreen(idle)
			}
		}
		return r
	}
	questions := []any{map[string]any{"optionCount": 3, "multiSelect": false, "preview": false}, map[string]any{"optionCount": 2, "multiSelect": true, "preview": false}}
	previewed := map[string]any{"optionCount": 3, "multiSelect": false, "preview": true}
	choice := func(picked []int, other string) map[string]any {
		if picked == nil {
			picked = []int{}
		}
		return map[string]any{"picked": picked, "other": other}
	}
	call := func(r *rig, qs any, cs any) Response {
		return r.svc.AnswerQuestion(body(map[string]any{"session": testSession, "questions": qs, "choices": cs}))
	}

	r := setup(true)
	if res := call(r, questions, []any{choice([]int{1}, ""), choice([]int{0, 1}, "")}); res.Status != 200 ||
		!reflect.DeepEqual(r.panes.sent(), []string{"keys:2", "keys:1", "keys:2", "keys:Right", "keys:1"}) {
		t.Fatalf("every question: %d %v", res.Status, r.panes.sent())
	}
	r = setup(true)
	call(r, questions[:1], []any{choice(nil, "Enter; C-c")})
	if !reflect.DeepEqual(r.panes.sent(), []string{"keys:4", "literal:Enter; C-c", "keys:Enter"}) {
		t.Errorf("free text: %v", r.panes.sent())
	}
	r = setup(true)
	call(r, questions[:1], []any{choice([]int{2}, "")})
	if !reflect.DeepEqual(r.panes.sent(), []string{"keys:3"}) {
		t.Errorf("lone single choice: %v", r.panes.sent())
	}
	r = setup(true)
	if res := call(r, []any{previewed}, []any{choice([]int{0}, "")}); res.Status != 200 || !reflect.DeepEqual(r.panes.sent(), []string{"keys:1", "keys:Enter"}) {
		t.Errorf("previews: %v", r.panes.sent())
	}
	r = setup(true)
	if res := call(r, []any{map[string]any{"optionCount": 3, "multiSelect": false}}, []any{choice([]int{1}, "")}); res.Status != 200 ||
		!reflect.DeepEqual(r.panes.sent(), []string{"keys:2", "keys:Enter"}) {
		t.Errorf("preview read off the screen: %v", r.panes.sent())
	}
	r = setup(false)
	expectError(t, "not taken", call(r, questions[:1], []any{choice([]int{0}, "")}), 409, "The agent did not take the answer")
	if !reflect.DeepEqual(r.panes.sent(), []string{"keys:1"}) {
		t.Errorf("not taken: %v", r.panes.sent())
	}
	r = setup(true)
	r.panes.setScreen(idle)
	expectError(t, "no question", call(r, questions[:1], []any{choice([]int{0}, "")}), 409, "No question on the agent screen")
	if len(r.panes.sent()) != 0 {
		t.Errorf("typed without a question: %v", r.panes.sent())
	}
	r = setup(true)
	expectError(t, "free text next to previews", call(r, []any{previewed}, []any{choice(nil, "своё")}), 400, "question 1: no free text next to previews")
	expectError(t, "out of range", call(r, questions, []any{choice([]int{5}, ""), choice([]int{0}, "")}), 400, "question 1: option out of range")
	expectError(t, "not whole", call(r, questions[:1], []any{map[string]any{"picked": []float64{0.5}, "other": ""}}), 400, "question 1: option out of range")
	expectError(t, "malformed", r.svc.AnswerQuestion(body(map[string]any{"session": testSession, "questions": "x", "choices": []any{}})), 400, "session, questions and choices parameters required")
	if len(r.panes.sent()) != 0 {
		t.Errorf("typed for a bad request: %v", r.panes.sent())
	}
	r.panes.exists = false
	expectError(t, "gone", call(r, questions, []any{choice([]int{0}, ""), choice([]int{0}, "")}), 404, "Session not found")
	r = setup(true)
	r.panes.failOn = "keys:Right"
	expectError(t, "tmux failed midway", call(r, questions, []any{choice([]int{0}, ""), choice([]int{0}, "")}), 500, "Failed to send answers")
}

// w0 is the id of the item an add answered with.
func w0(t *testing.T, res Response) string {
	t.Helper()
	item, _ := asJSON(t, res)["item"].(map[string]any)
	id, _ := item["id"].(string)
	return id
}
