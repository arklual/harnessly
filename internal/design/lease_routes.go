package design

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Leases is the web UI's side of the board leases, the library of this runtime answering for
// whichever backend it uses (the design center, or the library's .claims folder).
//
//	GET  ?project=      {mode, leases: LeaseView[]}  the live leases (of a project, or of the projects the person may see)
//	POST {action: "take"|"release"|"force"|"ask", project, board}
//	  take     the person edits the board in the page (client: the page's id): take or renew the lease, call on every edit;
//	           409 {error, holder} when another chat or person holds it
//	  release  let go of the person's own lease; 409 {code: "turn_running"} while that chat's agent works
//	  force    take the person's own lease off at once ("Снять — это зависло"); the chat is told on its next edit
//	  ask      ask the other person to release (they see it in LeaseView.requests); 200 {ok: true}
//
// A lease of another person is never taken by these calls; 403 {code: "not_yours"} when force/release name one.
func (a *API) Leases(r *http.Request) Answer {
	h := noStore()
	mode := LeasesFolder
	if a.L.center() != nil {
		mode = LeasesCenter
	}
	refuse := func(err error) Answer {
		switch e := err.(type) {
		case *LeaseTurnRunningError:
			return JSON(r, http.StatusConflict, struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}{e.Error(), "turn_running"}, h)
		case *ClaimError:
			return JSON(r, http.StatusBadRequest, errorBody(e.Msg), h)
		}
		a.warn("design lease request failed", err)
		return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not run the lease command", "Не удалось выполнить команду аренды")), h)
	}
	if r.Method == http.MethodGet {
		if _, no := a.Guard(r, http.MethodGet); no != nil {
			return *no
		}
		project := QueryString(r, "project")
		if no := a.gate(r, project, RoleView); no != nil {
			return *no
		}
		a.L.StartBackground()
		views, err := a.L.LeaseViews(project)
		if err != nil {
			return refuse(err)
		}
		visible := []LeaseView{}
		for _, v := range views {
			if a.L.Can(v.Project, RoleView) {
				visible = append(visible, v)
			}
		}
		return JSON(r, http.StatusOK, struct {
			Mode   string      `json:"mode"`
			Leases []LeaseView `json:"leases"`
		}{mode, visible}, h)
	}
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	project, okP := strField(body, "project")
	board, okB := strField(body, "board")
	action, _ := strField(body, "action")
	if !okP || !okB || (action != "release" && action != "force" && action != "ask" && action != "take") {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("A project, a board and an action (take, release, force, ask) are required", "Нужны проект, доска и действие (take, release, force, ask)")), h)
	}
	if no := a.gate(r, project, RoleEdit); no != nil {
		return *no
	}
	a.L.StartBackground()
	var err error
	switch action {
	case "take":
		client, _ := strField(body, "client")
		out, err := a.L.TakeForPerson(project, board, client)
		if err != nil {
			return refuse(err)
		}
		if out.Holder != nil {
			o := out.Holder.Owner
			return JSON(r, http.StatusConflict, struct {
				Error  string      `json:"error"`
				Holder claimHolder `json:"holder"`
			}{out.Message, claimHolder{o.User, o.Runtime, o.ChatTitle, out.Holder.Since, o.Machine, out.Holder.LastEdit, out.Holder.TurnRunning}}, h)
		}
		return JSON(r, http.StatusOK, struct {
			OK      bool  `json:"ok"`
			Offline bool  `json:"offline,omitempty"`
			Since   int64 `json:"since"`
		}{true, out.Offline, out.Claim.Since}, h)
	case "release":
		_, err = a.L.ReleaseOwnLease(project, board)
	case "force":
		_, err = a.L.ForceReleaseLease(project, board)
	case "ask":
		err = a.L.RequestLeaseRelease(project, board)
	}
	if err != nil {
		if ce, ok := err.(*ClaimError); ok && ce.Msg == notYoursText() {
			return JSON(r, http.StatusForbidden, struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}{ce.Msg, "not_yours"}, h)
		}
		return refuse(err)
	}
	return JSON(r, http.StatusOK, map[string]bool{"ok": true}, h)
}

func notYoursText() string {
	return locale.Pick("The board is held by another person: ask them to release it", "Доску держит другой человек: попросите его освободить")
}
