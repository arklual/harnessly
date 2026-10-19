package checkpoints

import (
	"net/http"
	"strings"

	"harnessly/internal/agent"
	"harnessly/internal/jsvalue"
)

// API is the checkpoints route of the platform's pages, past the API's own checks (a caller with a page session): GET
// ?tabId lists a conversation's snapshots and rewinds, GET ?tabId&id is the diff from the working folder to one, POST
// {tabId, id} returns the working folder to it. The folder is the tab's own, never a parameter.
type API struct {
	Store *Store
	// Tab is an agent tab: its tmux session and the agent's state; ok false when there is no such agent tab.
	Tab func(tabID string) (session, cliState string, ok bool)
	// Cwd is a tmux session's working folder, "" when unknown.
	Cwd func(session string) string
}

// Param is a request value as the previous runtime's handler saw it: absent, or any JSON value (a query parameter is a
// string, a repeated one an array).
type Param struct {
	Value   any
	Present bool
}

// Request is what the handler reads of a request.
type Request struct {
	Method      string
	ContentType string
	TabID, ID   Param
}

// TextAnswer is a plain-text answer (the dispatcher's own failure).
type TextAnswer string

const busyText = "Агент работает: дождитесь конца хода"

func errBody(text string) *jsvalue.Object { return jsvalue.NewObject("error", text) }

func busyState(state string) bool { return state == "busy" || state == "needs-input" }

// scope is the tab's folder from its own terminal, and whether its agent is working (a rewind would race it).
func (a *API) scope(tabID string) (*Scope, bool) {
	session, state, ok := a.Tab(tabID)
	if !ok {
		return nil, false
	}
	cwd := a.Cwd(session)
	if cwd == "" {
		return nil, false
	}
	sc, err := a.Store.Scope(tabID, cwd)
	if err != nil {
		return nil, false
	}
	return sc, busyState(state)
}

// Serve answers a request: status, body (a JSON value, a TextAnswer, or nil for none) and headers.
func (a *API) Serve(req Request) (int, any, http.Header) {
	h := http.Header{"Cache-Control": {"no-store"}}
	if req.Method != http.MethodGet && req.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return http.StatusMethodNotAllowed, nil, h
	}
	post := req.Method == http.MethodPost
	if post && !strings.HasPrefix(req.ContentType, "application/json") {
		return http.StatusUnsupportedMediaType, errBody("Нужен application/json"), h
	}
	tabID, ok := req.TabID.Value.(string)
	if !req.TabID.Present || !ok || !ValidTabID(tabID) {
		return http.StatusBadRequest, errBody("Нужен tabId"), h
	}
	id, idString := req.ID.Value.(string)
	if (post || req.ID.Present) && !(req.ID.Present && idString && ValidID(id)) {
		return http.StatusBadRequest, errBody("Неверная точка отката"), h
	}
	sc, busy := a.scope(tabID)
	if sc == nil {
		return http.StatusNotFound, errBody("Вкладка не найдена"), h
	}
	if !post && !req.ID.Present {
		list, err := sc.List()
		if err != nil {
			agent.Log("error", "checkpoints: list failed", agent.F{K: "err", V: err.Error()})
			return http.StatusInternalServerError, TextAnswer("Internal Server Error"), h // the handler threw: the dispatcher's answer
		}
		views := make([]any, len(list))
		for i, c := range list {
			views[i] = c.View()
		}
		return http.StatusOK, jsvalue.NewObject("checkpoints", views, "rewinds", sc.Rewinds(), "busy", busy), h
	}
	if !post {
		diff, err := sc.DiffTo(id)
		if err != nil {
			return http.StatusUnprocessableEntity, errBody("Не удалось сравнить с текущим состоянием"), h
		}
		if diff == nil {
			return http.StatusNotFound, errBody("Точка отката не найдена"), h
		}
		return http.StatusOK, diff.JSON(), h
	}
	if busy {
		return http.StatusConflict, errBody(busyText), h
	}
	result, err := sc.Rewind(id, Hooks{IsBusy: func() bool {
		_, state, ok := a.Tab(tabID)
		return !ok || busyState(state)
	}})
	if err != nil {
		return http.StatusInternalServerError, errBody("Откат не удался"), h
	}
	switch result.Reason {
	case "":
		return http.StatusOK, result.JSON(), h
	case Busy:
		return http.StatusConflict, errBody(busyText), h
	case NotFound:
		return http.StatusNotFound, errBody("Точка отката не найдена"), h
	}
	return http.StatusUnprocessableEntity, errBody("Не удалось сохранить текущее состояние, откат не выполнен"), h
}

// SnapshotTurnEnd snapshots a tab's folder once its agent's turn ended; a failure is logged and never reaches the agent.
func (s *Store) SnapshotTurnEnd(tabID, cwd string) {
	if cwd == "" {
		return
	}
	sc, err := s.Scope(tabID, cwd)
	if err != nil {
		agent.Log("warn", "checkpoint failed", agent.F{K: "tabId", V: tabID}, agent.F{K: "err", V: err.Error()})
		return
	}
	if _, reason := sc.Take(s.now()); reason != "" && reason != Throttled {
		agent.Log("warn", "checkpoint skipped", agent.F{K: "tabId", V: tabID}, agent.F{K: "reason", V: reason})
	}
}
