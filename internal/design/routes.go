package design

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/push"
)

var tabIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

type libraryResponse struct {
	Port      int         `json:"port"`
	Root      string      `json:"root"`
	SelfTabID *string     `json:"selfTabId"`
	Artifacts []*Artifact `json:"artifacts"`
}

// Library is GET the machine's design library: the projects with the agents working on them, their checks and
// claims, and the port of the library server, started here on first use; ?lite=1 gives only the port.
func (a *API) Library(r *http.Request) Answer {
	if _, no := a.Guard(r, http.MethodGet); no != nil {
		return *no
	}
	h := noStore()
	var self *string
	if tab := QueryString(r, "tab"); tabIDRe.MatchString(tab) {
		self = &tab
	}
	fail := func() Answer {
		return JSON(r, 500, errorBody(locale.Pick("Could not open the design library", "Не удалось открыть библиотеку макетов")), h)
	}
	port, err := a.L.EnsureServer()
	if err != nil {
		return fail()
	}
	if v, ok := Query(r, "lite"); ok && v == "1" {
		return JSON(r, 200, libraryResponse{Port: port, Root: a.L.Root(), SelfTabID: self, Artifacts: []*Artifact{}}, h)
	}
	arts := a.L.ListArtifacts()
	a.L.AttachChecks(arts)
	a.L.AttachReleases(arts)
	if a.L.AttachAgents(arts, deref(self)) != nil || a.L.AttachClaims(arts) != nil {
		return fail()
	}
	return JSON(r, 200, libraryResponse{Port: port, Root: a.L.Root(), SelfTabID: self, Artifacts: arts}, h)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strField(b *jsvalue.Object, k string) (string, bool) {
	s, ok := Field(b, k).(string)
	return s, ok
}

// Boards is POST {artifact, action, path?, title?, delta?}: a board renamed, moved, duplicated or removed, or the
// design renamed; the boards as they are now.
func (a *API) Boards(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	bad := JSON(r, 400, errorBody(locale.Pick("Invalid request", "Неверный запрос")), h)
	artifact, ok1 := strField(body, "artifact")
	action, ok2 := strField(body, "action")
	if !ok1 || !ok2 || !BoardActions[action] {
		return bad
	}
	e := BoardEdit{Action: action}
	if Has(body, "path") {
		p, ok := strField(body, "path")
		if !ok {
			return bad
		}
		e.Path = &p
	}
	if t, ok := strField(body, "title"); ok {
		e.Title = &t
	}
	if d, ok := Field(body, "delta").(float64); ok {
		e.Delta = &d
	}
	boards, err := a.L.EditBoards(artifact, e)
	if err != nil {
		if IsBoardEditError(err) {
			return JSON(r, 422, errorBody(err.Error()), h)
		}
		return JSON(r, 500, errorBody(locale.Pick("Could not change the boards", "Не удалось изменить доски")), h)
	}
	return JSON(r, 200, struct {
		Boards []Board `json:"boards"`
	}{boards}, h)
}

// Locate is POST {path, text}: the file and line of an element picked on a library page.
func (a *API) Locate(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	p, ok := strField(body, "path")
	txt, isText := strField(body, "text")
	if !ok || jscompat.Len(p) > 2048 || (Has(body, "text") && (!isText || jscompat.Len(txt) > 2000)) {
		return JSON(r, 400, errorBody("invalid request"), h)
	}
	return JSON(r, 200, struct {
		Matches []Match `json:"matches"`
	}{a.L.Locate(p, txt)}, h)
}

var stoppedStates = map[string]bool{"inactive": true, "cancelled": true, "unknown": true}

// Message is POST {tabId, artifact, text, blocks?}: the text goes into that agent conversation, prefixed with the
// design and its folder, typed the way a person types it; picked-element comments (blocks) are pasted whole.
func (a *API) Message(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	tabID, ok1 := strField(body, "tabId")
	artifact, ok2 := strField(body, "artifact")
	txt, ok3 := strField(body, "text")
	multiline := Field(body, "blocks") == true
	max := 2000
	if multiline {
		max = 24000
	}
	if !ok1 || !ok2 || !IsLibraryID(artifact) || !ok3 || jscompat.Trim(txt) == "" || jscompat.Len(txt) > max {
		return JSON(r, 400, errorBody(locale.Pick("An empty message or an unknown design", "Пустое сообщение или неизвестный макет")), h)
	}
	if a.Terminal == nil {
		return JSON(r, 503, errorBody("the tab keeper is not running"), h)
	}
	session, panel, found := a.Terminal.AgentTab(tabID)
	design := a.L.ReadArtifact(artifact)
	if !found || !agentPanels[panel] {
		return JSON(r, 404, errorBody(locale.Pick("The agent conversation is already closed", "Разговор агента уже закрыт")), h)
	}
	if design == nil {
		return JSON(r, 404, errorBody(locale.Pick("The design is gone", "Макета больше нет")), h)
	}
	state := "unknown"
	tabs, _ := a.L.Tabs()
	for _, t := range tabs {
		if t.TabID == tabID {
			state = t.State
		}
	}
	safe, process := a.Terminal.Front(session)
	if stoppedStates[state] || safe || process == "unknown" {
		return JSON(r, 409, errorBody(locale.Pick("The agent is not running in this conversation: open the conversation and continue there", "Агент в этом разговоре не запущен: откройте разговор и продолжите там")), h)
	}
	msg := AgentMessage(design, txt)
	if multiline {
		msg = AgentBlockMessage(design, txt)
	}
	if err := a.Terminal.Deliver(session, msg, multiline || a.Terminal.NeedsEditor(msg)); err != nil {
		a.warn("design message not delivered", err)
		return text(500, "Internal Server Error")
	}
	return Empty(204, h)
}

// formEncode is URLSearchParams' encoding of a value.
func formEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '*' || c == '-' || c == '.' || c == '_':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteString("%" + strings.ToUpper(hex2(c)))
		}
	}
	return b.String()
}

// designLinkPath is the app's address of a board alone in the window.
func (a *API) designLinkPath(project, board string) string {
	q := "view=design&dproject=" + formEncode(project) + "&dboard=" + formEncode(board) + "&dfull=1"
	q = regexp.MustCompile(`(?i)%3A`).ReplaceAllString(q, ":")
	return a.AppBase + "/?" + q
}

func pathnameOf(board string) string {
	if i := strings.IndexByte(board, '?'); i >= 0 {
		return board[:i]
	}
	return board
}

// NoPhoneError is what the person reads when there is no phone to send to.
func (l *Library) NoPhoneError() string {
	return locale.Pick("No phone with notifications: open "+l.cfg.Texts.Host+" on the phone, install it as an app and turn notifications on",
		"Нет телефона с уведомлениями: откройте "+l.cfg.Texts.Host+" на телефоне, установите как приложение и включите уведомления")
}

// Send is POST {artifact, board, title?, deviceId?}: a push to the person's other devices that opens the board alone in
// the window; {sent} is the number of devices the push services accepted it for.
func (a *API) Send(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	id, okID := strField(body, "artifact")
	board := ParseBoardPath(Field(body, "board"))
	rawTitle, isTitle := strField(body, "title")
	titleOK := !Has(body, "title") || (isTitle && jscompat.Len(rawTitle) <= LimitTitle)
	device, isDevice := strField(body, "deviceId")
	deviceOK := !Has(body, "deviceId") || (isDevice && jscompat.Len(device) <= 64)
	if !okID || !IsLibraryID(id) || board == "" || !titleOK || !deviceOK {
		return JSON(r, 400, errorBody("invalid request"), h)
	}
	art := a.L.ReadArtifact(id)
	if art == nil {
		return JSON(r, 404, errorBody(locale.Pick("The design is gone", "Макета больше нет")), h)
	}
	title := rawTitle
	if !isTitle || jscompat.Trim(rawTitle) == "" {
		title = art.Title
		var exact, same *Board
		for i := range art.Boards {
			b := &art.Boards[i]
			if exact == nil && b.Path == board {
				exact = b
			}
			if same == nil && pathnameOf(b.Path) == pathnameOf(board) {
				same = b
			}
		}
		if exact != nil {
			title = exact.Title
		} else if same != nil {
			title = same.Title
		}
	}
	if a.Push == nil {
		return JSON(r, 409, errorBody(a.L.NoPhoneError()), h)
	}
	statuses := a.Push(PushRequest{Title: push.Clip(title, LimitTitle), URL: a.designLinkPath(id, board), Tag: "design:" + id + ":" + board, DeviceID: device})
	if len(statuses) == 0 {
		return JSON(r, 409, errorBody(a.L.NoPhoneError()), h)
	}
	sent, gone := 0, 0
	for _, s := range statuses {
		if s >= 200 && s < 300 {
			sent++
		}
		if s == 404 || s == 410 {
			gone++
		}
	}
	if sent > 0 {
		return JSON(r, 200, struct {
			Sent int `json:"sent"`
		}{sent}, h)
	}
	if gone == len(statuses) {
		return JSON(r, 409, errorBody(a.L.NoPhoneError()), h)
	}
	return JSON(r, 502, errorBody(locale.Pick("The notification service did not accept the message, try again", "Сервис уведомлений не принял сообщение, попробуйте ещё раз")), h)
}

// Continue is POST {artifact, prompt?}: a new agent conversation in the design's folder.
func (a *API) Continue(r *http.Request, resolveCwd func(string, []string) (string, error)) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	id, ok := strField(body, "artifact")
	if !ok {
		return JSON(r, 400, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), h)
	}
	prompt, isPrompt := strField(body, "prompt")
	if Has(body, "prompt") && (!isPrompt || jscompat.Len(prompt) > 12000) {
		return JSON(r, 400, errorBody(locale.Pick("The task is too long", "Задача слишком длинная")), h)
	}
	res := a.StartProjectDialog(id, prompt, resolveCwd)
	if res.TabID == "" {
		return JSON(r, res.Status, errorBody(res.Error), h)
	}
	return JSON(r, 201, struct {
		TabID string `json:"tabId"`
	}{res.TabID}, h)
}

type cliArtifact struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Boards      int    `json:"boards"`
	ProjectPath string `json:"projectPath"`
}

// CLIDesign is the library for agents: GET lists the designs, ?logs= what a person's preview logged, ?state= what it
// shows; POST {artifact, control} drives the open preview, POST {artifact, boards?, wait?} checks boards.
func (a *API) CLIDesign(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	q := jscompat.Query(r.URL.RawQuery)
	if r.Method == http.MethodGet && len(q["logs"]) == 1 {
		id := q["logs"][0]
		if !IsLibraryID(id) {
			return JSON(r, 400, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), h)
		}
		return JSON(r, 200, struct {
			Artifact string         `json:"artifact"`
			Entries  []LiveLogEntry `json:"entries"`
		}{id, a.L.LiveLog(id)}, h)
	}
	if r.Method == http.MethodGet && len(q["state"]) == 1 {
		id := q["state"][0]
		if !IsLibraryID(id) {
			return JSON(r, 400, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), h)
		}
		return JSON(r, 200, struct {
			Artifact string       `json:"artifact"`
			Shown    *DesignState `json:"shown"`
		}{id, a.L.State(id)}, h)
	}
	if r.Method == http.MethodGet {
		arts := a.L.ListArtifacts()
		list := make([]cliArtifact, 0, len(arts))
		for _, x := range arts {
			list = append(list, cliArtifact{ID: x.ID, Title: x.Title, Boards: len(x.Boards), ProjectPath: x.ProjectPath})
		}
		return JSON(r, 200, struct {
			Rules     string        `json:"rules"`
			Artifacts []cliArtifact `json:"artifacts"`
		}{a.L.GuidePath(), list}, h)
	}
	if r.Method != http.MethodPost {
		return Empty(405, h)
	}
	body := cliBody(raw)
	artifact, ok := strField(body, "artifact")
	if !ok || !IsLibraryID(artifact) {
		return JSON(r, 400, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), h)
	}
	if Has(body, "control") {
		c := ParseControl(Field(body, "control"))
		if c == nil {
			return JSON(r, 400, errorBody(locale.Pick("Empty command: board, view, panel or props", "Пустая команда: board, view, panel или props")), h)
		}
		pages := a.L.SendControl(artifact, *c)
		if pages == 0 {
			return JSON(r, 409, errorBody(locale.Pick("The design is not open in a preview now: ask the person to open the board in the Claude Design tab", "Макет сейчас не открыт в превью: попросите человека открыть доску во вкладке Claude Design")), h)
		}
		time.Sleep(900 * time.Millisecond)
		return JSON(r, 200, struct {
			Pages int          `json:"pages"`
			Shown *DesignState `json:"shown"`
		}{pages, a.L.State(artifact)}, h)
	}
	return a.CLICheck(r, body, artifact)
}

// cliBody is req.body ?? {}: a string body has no fields.
func cliBody(raw any) *jsvalue.Object { return Fields(raw) }

type claimHolder struct {
	Person    string `json:"person"`
	Runtime   string `json:"runtime"`
	ChatTitle string `json:"chatTitle"`
	Since     int64  `json:"since"`
}

// CLIClaims is the agents' board claims: GET ?project= who holds what; POST {action:'claim', session, project, board}
// takes a board for the chat owning the tmux session, {action:'release', session, project?, board?} lets go.
func (a *API) CLIClaims(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return Empty(405, h)
	}
	a.L.StartBackground()
	refuse := func(err error) Answer {
		if ce, ok := err.(*ClaimError); ok {
			return JSON(r, 400, errorBody(ce.Msg), h)
		}
		a.warn("design library request failed", err)
		return JSON(r, 500, errorBody(locale.Pick("Could not run the claims command", "Не удалось выполнить команду заявок")), h)
	}
	if r.Method == http.MethodGet {
		var project any
		if p, ok := Query(r, "project"); ok && p != "" {
			project = p
		}
		lines, err := a.L.WhoHolds(project)
		if err != nil {
			return refuse(err)
		}
		return JSON(r, 200, struct {
			Claims []ClaimLine `json:"claims"`
		}{lines}, h)
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return JSON(r, 415, errorBody(locale.Pick("application/json is required", "Нужен application/json")), h)
	}
	body := Fields(raw)
	session, ok := strField(body, "session")
	if !ok || !sessionRe.MatchString(session) {
		return JSON(r, 400, errorBody(locale.Pick("The session of the agent's tab (tmux) is required", "Нужна сессия вкладки агента (tmux)")), h)
	}
	cli := a.L.cfg.Texts.CLI
	switch Field(body, "action") {
	case "claim":
		project, okP := strField(body, "project")
		board, okB := strField(body, "board")
		if !okP || !okB {
			return JSON(r, 400, errorBody(locale.Pick("A project and a board are required: "+cli+" design claim <project> <board>", "Нужны проект и доска: "+cli+" design claim <проект> <доска>")), h)
		}
		out, err := a.L.ClaimForChat(session, project, board)
		if err != nil {
			return refuse(err)
		}
		if out.Holder != nil {
			o := out.Holder.Owner
			return JSON(r, 409, struct {
				Error  string      `json:"error"`
				Holder claimHolder `json:"holder"`
			}{out.Message, claimHolder{o.User, o.Runtime, o.ChatTitle, out.Holder.Since}}, h)
		}
		return JSON(r, 200, struct {
			OK        bool   `json:"ok"`
			Project   string `json:"project"`
			Board     string `json:"board"`
			Page      string `json:"page,omitempty"`
			Refreshed bool   `json:"refreshed"`
			Since     int64  `json:"since"`
		}{true, out.Claim.Project, out.Claim.Board, out.Page, out.Refreshed, out.Claim.Since}, h)
	case "release":
		pv, bv := Field(body, "project"), Field(body, "board")
		_, pStr := pv.(string)
		_, bStr := bv.(string)
		if (Has(body, "project") && !pStr) || (Has(body, "board") && !bStr) || (!Has(body, "project") && Has(body, "board")) {
			return JSON(r, 400, errorBody(locale.Pick("To release claims: "+cli+" design release [<project> <board>]", "Снять заявки: "+cli+" design release [<проект> <доска>]")), h)
		}
		released, err := a.L.ReleaseForChat(session, pv, bv)
		if err != nil {
			return refuse(err)
		}
		type item struct {
			Project string `json:"project"`
			Board   string `json:"board"`
		}
		list := []item{}
		for _, c := range released {
			list = append(list, item{c.Project, c.Board})
		}
		return JSON(r, 200, struct {
			Released []item `json:"released"`
		}{list}, h)
	}
	return JSON(r, 400, errorBody(locale.Pick("Unknown action: claim or release", "Неизвестное действие: claim или release")), h)
}

// PhoneTargets are the sender's other devices that said they are phones, else those that said nothing.
func PhoneTargets(subs []push.Subscription, sender string) []push.Subscription {
	var others, phones, unknown []push.Subscription
	for _, s := range subs {
		if sender == "" || s.DeviceID != sender {
			others = append(others, s)
		}
	}
	for _, s := range others {
		if s.Mobile != nil && *s.Mobile {
			phones = append(phones, s)
		}
		if s.Mobile == nil {
			unknown = append(unknown, s)
		}
	}
	if len(phones) > 0 {
		return phones
	}
	return unknown
}

// PushMessage is the notification of a board sent to a phone.
func PushMessage(req PushRequest, tagPrefix string) push.Message {
	body := locale.Pick("Take a look at the mobile version: ", "Посмотрите мобильную версию: ") + push.Clip(req.Title, 90)
	return push.Message{Kind: "design", Title: "Claude Design", Body: body, ConversationTitle: "Claude Design", Detail: body,
		Provider: "claude", Tag: tagPrefix + req.Tag, At: nowMs(), URL: req.URL}
}

// Create is POST {title, brief}: the library's template copied to a new project and an agent conversation started in
// it with the brief; a project made whose conversation did not start is an error that still names the project.
func (a *API) Create(r *http.Request, resolveCwd func(string, []string) (string, error)) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	brief, err := CleanBrief(Field(body, "brief"))
	var project CreatedProject
	if err == nil {
		title, _ := strField(body, "title")
		project, err = a.L.CreateProject(title)
	}
	if err != nil {
		var ce *CreateError
		if errors.As(err, &ce) {
			if ce.Conflict {
				return JSON(r, 409, errorBody(ce.Msg), h)
			}
			return JSON(r, 422, errorBody(ce.Msg), h)
		}
		return JSON(r, 500, errorBody(locale.Pick("Could not create the project", "Не удалось создать проект")), h)
	}
	res := a.StartProjectDialog(project.ID, a.L.NewProjectPrompt(project.Title, brief)+"\n\n"+a.L.PagesHint(project.ID), resolveCwd)
	if res.TabID == "" {
		return JSON(r, res.Status, struct {
			Error string `json:"error"`
			ID    string `json:"id"`
		}{locale.Pick("Project \""+project.Title+"\" was created, but the conversation did not start: ", "Проект «"+project.Title+"» создан, но диалог не запустился: ") + res.Error, project.ID}, h)
	}
	return JSON(r, 201, struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		TabID string `json:"tabId"`
	}{project.ID, project.Title, res.TabID}, h)
}
