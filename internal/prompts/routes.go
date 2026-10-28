package prompts

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"slices"
	"time"

	"github.com/ly-solutions/harnessly/internal/chatguard"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/screen"
)

// The operations behind the routes: each takes the request body (JSON) or query value and answers a Response. The HTTP
// layer checks the session cookie (403), the method (405, with Allow), the JSON content type where a route needs it
// (415) and Cache-Control, then writes the Response.

// Response is a route's answer: the status and the JSON body (nil: no body).
type Response struct {
	Status int
	Body   any
}

// ErrorBody is a refusal; Code carries the command runner's error on switch-model and names a queue refusal
// (CodeNotQueued), Waits says the agent reads the message at its next step by itself.
type ErrorBody struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
	Waits bool   `json:"waits,omitempty"`
}

func fail(status int, message string) Response { return Response{status, ErrorBody{Error: message}} }

func ok(body any) Response { return Response{200, body} }

// fields is a request body as an object; anything else reads as an empty one.
type fields map[string]json.RawMessage

func decodeFields(body []byte) fields {
	var f fields
	if json.Unmarshal(body, &f) != nil || f == nil {
		return fields{}
	}
	return f
}

// str is the field when it is a JSON string.
func (f fields) str(key string) (string, bool) {
	raw, present := f[key]
	if !present {
		return "", false
	}
	return jsonString(raw)
}

func (f fields) text(key string) string { s, _ := f.str(key); return s }

func (f fields) isFalse(key string) bool { return string(f[key]) == "false" }

var tabIDRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// --- POST /prompt-queue -------------------------------------------------------------------------------------------

// CodeNotQueued: the message is no longer in the queue (the agent took it, or it was dropped).
const CodeNotQueued = "not-queued"

// QueueAdded is the answer to add; Repeated: the client id was queued before, Item is that message and nothing new was
// queued.
type QueueAdded struct {
	Item     Item   `json:"item"`
	Delivery string `json:"delivery"`
	Repeated bool   `json:"repeated,omitempty"`
}

// QueueTaken is the answer to remove and take.
type QueueTaken struct {
	Item Item `json:"item"`
}

// SteerBody is the answer to steer: {ok: true, outcome} — sent: typed into the running turn, the agent reads it at its
// next step; delivered: the agent held the message already and nothing was typed.
type SteerBody struct {
	OK      bool   `json:"ok"`
	Outcome string `json:"outcome"`
}

// OKBody is {ok: true}.
type OKBody struct {
	OK bool `json:"ok"`
}

// PromptQueue is `{tabId, action, …}` on a tab's queue: add {text, images, clientId?} queues a message and hands it to
// the agent (into the running turn when it works), remove/take {id} take a message out (to drop it or edit it back),
// steer {id} sends it now, retry {id} asks again for a message delivery gave up on. A tab the runtime does not know is
// refused for add, instead of keeping a message nothing delivers. An add repeating a clientId answers the message it
// queued.
func (s *Service) PromptQueue(body []byte) Response {
	f := decodeFields(body)
	tabID, action := f.text("tabId"), f.text("action")
	if !tabIDRE.MatchString(tabID) || !slices.Contains([]string{"add", "remove", "take", "steer", "retry"}, action) {
		return fail(400, msgNeedTabAction.String())
	}
	if action == "add" {
		var raw []json.RawMessage
		_ = json.Unmarshal(f["images"], &raw)
		images := []string{}
		for _, r := range raw {
			if im, isString := jsonString(r); isString {
				images = append(images, im)
			}
		}
		for _, im := range images {
			if !s.cfg.IsUploadImage(im) {
				return fail(400, msgNotUploadImage.String())
			}
		}
		if _, known := s.cfg.Tabs.Tab(tabID); !known {
			return fail(404, msgNoTab.String())
		}
		if s.cfg.TerminalOff() && chatguard.BashMode(f.text("text")) {
			return fail(403, msgTerminalOff.String())
		}
		item, repeated, err := s.queue.AddOnce(tabID, f.text("clientId"), f.text("text"), images)
		if err != nil {
			return fail(409, err.Error())
		}
		if repeated {
			return ok(QueueAdded{Item: item, Delivery: DeliveryQueued, Repeated: true})
		}
		// "now": true is ⌘↵ — into the running turn; without it the message waits for the turn to end (Enter queues).
		now := string(f["now"]) == "true"
		return ok(QueueAdded{Item: item, Delivery: s.TakeNewPrompt(tabID, item.ID, now)})
	}
	id := f.text("id")
	if id == "" {
		return fail(400, msgNeedID.String())
	}
	if action == "steer" {
		r := s.Steer(tabID, id)
		if !r.OK {
			return Response{r.Status, ErrorBody{Error: r.Message, Code: r.Code}}
		}
		return ok(SteerBody{OK: true, Outcome: r.Outcome})
	}
	notQueued := Response{404, ErrorBody{Error: msgNotQueued.String(), Code: CodeNotQueued}}
	if action == "retry" {
		item, found := s.Retry(tabID, id)
		if !found {
			return notQueued
		}
		return ok(QueueTaken{item})
	}
	item, found := s.queue.Take(tabID, id)
	if !found {
		return notQueued
	}
	s.queue.forget(tabID, item.ClientID)
	return ok(QueueTaken{item})
}

// --- POST /prompt-text --------------------------------------------------------------------------------------------

// PromptTextMax is the longest composer text, in UTF-16 units.
const PromptTextMax = 512 * 1024

// PromptTyped is the answer to prompt-text: how the text went in and when each step of it ended.
type PromptTyped struct {
	Via   string `json:"via"`
	Steps *Steps `json:"steps"`
}

// PromptText is `{tabId, text, submit?, messageId?}`, the composer's text for a tab. A 404 tells the page to type the
// text itself; a duplicate counts as sent.
func (s *Service) PromptText(body []byte) Response {
	steps := NewSteps(s.clock)
	f := decodeFields(body)
	tabID, text := f.text("tabId"), f.text("text")
	if tabID == "" || text == "" {
		return fail(400, msgNeedTabText.String())
	}
	if jscompat.Len(text) > PromptTextMax {
		return fail(413, msgTooLong.String())
	}
	messageID := f.text("messageId")
	via, err := s.TypePrompt(tabID, text, !f.isFalse("submit"), messageID, steps)
	s.info("prompt typed", "tabId", tabID, "via", via, "messageId", nullable(messageID), "chars", jscompat.Len(text), "steps", steps)
	switch {
	case err == ErrNoTab:
		return fail(404, msgNoTab.String())
	case err == ErrCodexNeedsSubmit:
		return fail(400, msgCodexSubmit.String())
	case errors.Is(err, ErrTerminalOff):
		return fail(403, msgTerminalOff.String())
	case err != nil:
		return fail(500, msgTypeFailed.String())
	case via == ViaFailed:
		return fail(500, msgTypeRejected.String())
	}
	return ok(PromptTyped{Via: via, Steps: steps})
}

// --- POST /send-now -----------------------------------------------------------------------------------------------

// SendNowBody is a 200 answer of send-now: delivered or sent.
type SendNowBody struct {
	Outcome string `json:"outcome"`
}

// SendNowRoute is `{tabId, text}`: the agent reads the queued message now without its current step being cancelled.
func (s *Service) SendNowRoute(body []byte) Response {
	f := decodeFields(body)
	tabID, text := f.text("tabId"), f.text("text")
	if tabID == "" || blank(text) {
		return fail(400, msgNeedTabText.String())
	}
	r, found := s.SendNow(tabID, text)
	switch {
	case !found:
		return fail(404, msgNoTab.String())
	case r.OK:
		return ok(SendNowBody{r.Outcome})
	}
	return Response{r.Status, ErrorBody{Error: r.Message, Waits: r.Waits}}
}

// --- POST /interrupt ----------------------------------------------------------------------------------------------

// InterruptRoute is `{tabId}`: Stop. 204 without a body.
func (s *Service) InterruptRoute(body []byte) Response {
	tabID := decodeFields(body).text("tabId")
	if tabID == "" {
		return fail(400, msgNeedTab.String())
	}
	if !s.Interrupt(tabID) {
		return fail(404, msgNoTab.String())
	}
	return Response{Status: 204}
}

// --- POST /switch-model -------------------------------------------------------------------------------------------

// SwitchModelBody is the frozen 200 answer of switch-model.
type SwitchModelBody struct {
	OK bool `json:"ok"`
	// Confirmed: Claude asked "Switch model?" and the web accepted it.
	Confirmed bool `json:"confirmed"`
}

// SwitchModel is `{session, model}` (a frozen contract): /model <model> through the command runner. 200, or 400 | 404 |
// 409 with {error} (and the runner's code); a menu the command left open is handed to the page as needs-input.
func (s *Service) SwitchModel(body []byte) Response {
	f := decodeFields(body)
	session, model := f.text("session"), f.text("model")
	if session == "" || !ModelIDRE.MatchString(model) {
		return fail(400, msgUnknownModel.String())
	}
	r := s.RunCommand(RunInput{Session: session, Provider: ProviderClaude, Line: "/model " + model, ExpectModel: true})
	if r.Status != 200 {
		status := r.Status
		if status == 422 || status == 500 {
			status = 409
		}
		e, _ := r.Body.(*RunError)
		if e == nil {
			e = &RunError{}
		}
		return Response{status, ErrorBody{Error: e.Message, Code: e.Error}}
	}
	b, _ := r.Body.(*RunBody)
	if b != nil && b.Outcome == "menu" {
		tab, known := s.cfg.Tabs.TabOfSession(session)
		if known && s.MarkNeedsInputFromScreen(tab.ID) {
			return fail(409, msgMenu.String())
		}
		return fail(409, msgMenuInTerminal.String())
	}
	return ok(SwitchModelBody{OK: true, Confirmed: b != nil && b.Outcome == "confirmed" && b.Asked != nil && *b.Asked})
}

// --- POST /commands/run -------------------------------------------------------------------------------------------

// RunCommandRoute is `{session, provider, line}`: the command runner's status and body.
func (s *Service) RunCommandRoute(body []byte) Response {
	f := decodeFields(body)
	session, hasSession := f.str("session")
	if !hasSession || session == "" {
		return Response{400, &RunError{Error: RunInvalid, Message: msgNeedSession.String()}}
	}
	provider := f.text("provider")
	if provider != ProviderClaude && provider != ProviderCodex {
		return Response{400, &RunError{Error: RunInvalid, Message: msgUnknownAgent.String()}}
	}
	line, isString := f.str("line")
	if !isString {
		return Response{400, &RunError{Error: RunInvalid, Message: msgNeedLine.String()}}
	}
	r := s.RunCommand(RunInput{Session: session, Provider: provider, Line: line})
	return Response(r)
}

// --- GET|POST /commands/screen ------------------------------------------------------------------------------------

// MirrorKeys are the keys the menu mirror may press: navigation, never text; digits pick a numbered option.
var MirrorKeys = []string{"Up", "Down", "Left", "Right", "Tab", "BTab", "Enter", "Escape", "Space", "1", "2", "3", "4", "5", "6", "7", "8", "9"}

// MirrorScreen is what the menu mirror shows: the menu, whether a panel or busy, and the panel's lines.
type MirrorScreen struct {
	Menu   *screen.Menu `json:"menu"`
	Panel  bool         `json:"panel"`
	Busy   bool         `json:"busy"`
	Screen string       `json:"screen"`
}

func (s *Service) mirrorSession(session string) (Response, bool) {
	if !SessionNameRE.MatchString(session) {
		return fail(400, msgBadSession.String()), false
	}
	if !s.cfg.Panes.HasSession(session) {
		return fail(404, msgNoSession.String()), false
	}
	return Response{}, true
}

func (s *Service) readMirror(session string) Response {
	styled, captured := s.cfg.Panes.CaptureStyled(session)
	if !captured {
		return fail(500, msgNoMirror.String())
	}
	st := screen.Read(styled)
	return ok(MirrorScreen{Menu: st.Menu, Panel: st.Panel, Busy: st.Busy, Screen: screen.PanelSnapshot(st.Plain)})
}

// CommandScreen is GET ?session=: the agent's screen for the menu mirror.
func (s *Service) CommandScreen(session string) Response {
	if r, valid := s.mirrorSession(session); !valid {
		return r
	}
	return s.readMirror(session)
}

// CommandScreenKey is POST `{session, key}`: presses one mirror key in a pane where the agent runs, waits 250 ms for the
// redraw and answers the screen.
func (s *Service) CommandScreenKey(body []byte) Response {
	f := decodeFields(body)
	session := f.text("session")
	if r, valid := s.mirrorSession(session); !valid {
		return r
	}
	key := f.text("key")
	if !slices.Contains(MirrorKeys, key) {
		return fail(400, msgKeyNotAllowed.String())
	}
	if cmd := s.cfg.Panes.PaneCurrentCommand(session); cmd == "" || s.cfg.IsShell(cmd) {
		return fail(409, msgAgentNotRunning.String())
	}
	if s.cfg.TerminalOff() && s.answersAnApproval(session) {
		return fail(403, msgTerminalOff.String())
	}
	unlock := hold(session)
	sent := s.cfg.Panes.SendRawKeys(session, key) == nil
	if sent {
		s.clock.Sleep(mirrorKeySettle)
	}
	unlock()
	if !sent {
		return fail(500, msgKeyFailed.String())
	}
	return s.readMirror(session)
}

// answersAnApproval: the agent's screen asks to approve a tool call or a plan, and a mirror key (a digit, Enter) would
// answer it. With the terminal off that is the owner's to press, as /api/v2/cmd/decide is: the mirror is for the menus
// of slash commands. A screen that cannot be read is no proof that nothing is asked.
func (s *Service) answersAnApproval(session string) bool {
	content, captured := s.cfg.Panes.CaptureAtWidth(session, 120, 50)
	return !captured || screen.HasPermissionPrompt(content) || len(screen.ParsePlanOptions(content)) > 0
}

// --- raw tmux routes ----------------------------------------------------------------------------------------------

// rawSession checks a raw route's session: given, one of the runtime's own (by name), and existing.
func (s *Service) rawSession(session, missing string) (Response, bool) {
	if session == "" {
		return fail(400, missing), false
	}
	if !SessionNameRE.MatchString(session) || !s.cfg.Panes.IsAppSession(session) || !s.cfg.Panes.HasSession(session) {
		return fail(404, "Session not found"), false
	}
	return Response{}, true
}

// SendInput is `{session, input}`: keys as tmux reads them (a key name or a string), not literal text.
func (s *Service) SendInput(body []byte) Response {
	f := decodeFields(body)
	session, input := f.text("session"), f.text("input")
	if input == "" {
		session = ""
	}
	if r, valid := s.rawSession(session, "session and input parameters required"); !valid {
		return r
	}
	unlock := hold(session)
	sent := s.cfg.Panes.SendRawKeys(session, input) == nil
	unlock()
	if !sent {
		return fail(500, "Failed to send input")
	}
	return ok(OKBody{true})
}

// CaptureBody is the visible screen ("" when the capture failed).
type CaptureBody struct {
	Content string `json:"content"`
}

// Capture is GET ?session=: the plain visible screen.
func (s *Service) Capture(session string) Response {
	if r, valid := s.rawSession(session, "session parameter required"); !valid {
		return r
	}
	content, _ := s.cfg.Panes.Capture(session)
	return ok(CaptureBody{content})
}

// OptionsBody is an empty options answer.
type OptionsBody struct {
	Options []string `json:"options"`
}

// PermissionOptions is GET ?session=: the permission prompt (else any choice menu) on a capture at least 120×50.
func (s *Service) PermissionOptions(session string) Response {
	if r, valid := s.rawSession(session, "session parameter required"); !valid {
		return r
	}
	content, captured := s.cfg.Panes.CaptureAtWidth(session, 120, 50)
	if !captured || content == "" {
		return ok(OptionsBody{[]string{}})
	}
	return ok(screen.ReadPromptOptions(content))
}

// PlanOptions is GET ?session=: the plan approval options on a capture at least 120×50.
func (s *Service) PlanOptions(session string) Response {
	if r, valid := s.rawSession(session, "session parameter required"); !valid {
		return r
	}
	content, captured := s.cfg.Panes.CaptureAtWidth(session, 120, 50)
	if !captured || content == "" {
		return ok(OptionsBody{[]string{}})
	}
	return ok(OptionsBody{screen.ParsePlanOptions(content)})
}

// answerRequest is the decoded body of answer-question; preview nil means an older page sent none.
type answerRequest struct {
	shapes   []screen.QuestionShape
	previews []*bool
	choices  []screen.QuestionChoice
}

func intOr(f float64, fallback int) int {
	if f != math.Trunc(f) || math.IsInf(f, 0) || math.Abs(f) > 1e9 {
		return fallback
	}
	return int(f)
}

func decodeAnswer(f fields) (answerRequest, bool) {
	var qs []map[string]json.RawMessage
	var cs []map[string]json.RawMessage
	if json.Unmarshal(f["questions"], &qs) != nil || json.Unmarshal(f["choices"], &cs) != nil || qs == nil || cs == nil {
		return answerRequest{}, false
	}
	var r answerRequest
	for _, q := range qs {
		var count float64
		var multi bool
		if q == nil || json.Unmarshal(q["optionCount"], &count) != nil || json.Unmarshal(q["multiSelect"], &multi) != nil {
			return answerRequest{}, false
		}
		var preview *bool
		if raw, present := q["preview"]; present {
			var p bool
			if json.Unmarshal(raw, &p) != nil || string(raw) == "null" {
				return answerRequest{}, false
			}
			preview = &p
		}
		// A count that is not a whole number reads as 0: refused as a bad option count, as before.
		r.shapes = append(r.shapes, screen.QuestionShape{OptionCount: intOr(count, 0), MultiSelect: multi})
		r.previews = append(r.previews, preview)
	}
	for _, c := range cs {
		var picked []float64
		var other string
		if c == nil || json.Unmarshal(c["picked"], &picked) != nil || picked == nil || json.Unmarshal(c["other"], &other) != nil || string(c["other"]) == "null" {
			return answerRequest{}, false
		}
		ch := screen.QuestionChoice{Picked: make([]int, len(picked)), Other: other}
		for i, p := range picked {
			ch.Picked[i] = intOr(p, -1) // a pick that is not a whole number is out of range
		}
		r.choices = append(r.choices, ch)
	}
	return r, true
}

type keyIO struct {
	s       *Service
	session string
}

func (k keyIO) Key(name string) error  { return k.s.cfg.Panes.SendRawKeys(k.session, name) }
func (k keyIO) Text(text string) error { return k.s.cfg.Panes.SendLiteral(k.session, text) }
func (k keyIO) Sleep(d time.Duration)  { k.s.clock.Sleep(d) }

// AnswerQuestion is `{session, questions, choices}`: answers Claude's AskUserQuestion dialog with keys. Nothing is typed
// unless the dialog is on screen; the answer counts once the dialog left it.
func (s *Service) AnswerQuestion(body []byte) Response {
	f := decodeFields(body)
	session := f.text("session")
	// {session, chat: true}: «Обсудить в чате» — the dialog is closed by its «Chat about this» row and the person goes
	// on in the composer.
	if chat, present := f["chat"]; present && string(chat) == "true" {
		if session == "" {
			return fail(400, "session parameter required")
		}
		if r, valid := s.rawSession(session, "session parameter required"); !valid {
			return r
		}
		unlock := hold(session)
		chatted := s.chatAboutQuestion(session)
		unlock()
		if !chatted {
			return fail(409, "No question on the agent screen")
		}
		return ok(OKBody{true})
	}
	req, valid := decodeAnswer(f)
	if session == "" || !valid {
		return fail(400, "session, questions and choices parameters required")
	}
	if r, valid := s.rawSession(session, "session, questions and choices parameters required"); !valid {
		return r
	}
	defer hold(session)()
	before, captured := s.cfg.Panes.Capture(session)
	if !captured || !screen.IsQuestionOnScreen(before) {
		s.warn("answer-question: no question on the screen, nothing sent", "session", session)
		return fail(409, "No question on the agent screen")
	}
	for i, p := range req.previews {
		if p != nil {
			req.shapes[i].Preview = *p
		} else {
			req.shapes[i].Preview = screen.PreviewFromScreen(len(req.shapes), before)
		}
	}
	if err := screen.ValidateAnswerRequest(req.shapes, req.choices); err != nil {
		return fail(400, err.Error())
	}
	if screen.PlayAnswerSteps(screen.BuildAnswerSteps(req.shapes, req.choices), keyIO{s, session}) != nil {
		return fail(500, "Failed to send answers")
	}
	read := func() (string, bool) { return s.cfg.Panes.Capture(session) }
	if !screen.WaitForQuestionGone(read, s.clock.Sleep) {
		s.warn("answer-question: the question is still on the screen after the keys", "session", session)
		return fail(409, "The agent did not take the answer")
	}
	return ok(OKBody{true})
}

const (
	chatAboutStep  = 120 * time.Millisecond
	chatAboutMoves = 16
)

// chatAboutQuestion closes Claude's AskUserQuestion dialog by its «Chat about this» row: the cursor is moved down
// until it stands on that row, read off the screen after every key (the row's place differs with previews, a free-text
// row and several options), then Enter. True once the dialog has left the screen.
func (s *Service) chatAboutQuestion(session string) bool {
	for moves := 0; ; moves++ {
		plain, ok := s.cfg.Panes.Capture(session)
		if !ok {
			return false
		}
		present, selected := screen.ChatAboutRow(plain)
		if !present {
			return false
		}
		if selected {
			if s.cfg.Panes.SendRawKeys(session, "Enter") != nil {
				return false
			}
			break
		}
		if moves >= chatAboutMoves || s.cfg.Panes.SendRawKeys(session, "Down") != nil {
			return false
		}
		s.clock.Sleep(chatAboutStep)
	}
	read := func() (string, bool) { return s.cfg.Panes.Capture(session) }
	return screen.WaitForQuestionGone(read, s.clock.Sleep)
}
