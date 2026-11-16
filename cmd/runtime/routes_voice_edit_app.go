package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/voice/quickedit"
)

// The voice's quick edit of an app's object: a board or a document open in its app is changed through the
// app's own quickEdit tools (manifest quickEdit), the plan made by the same fast models as a file's.
//
//	POST <platform>/voice/edit/app        {app, object, selection?, ask, confirm?} -> {said, changed, ms, hint, ids}
//	POST <platform>/voice/edit/app/undo   {app, object}                            -> {changed}
//
// A removal answers 409 edit_needs_yes with the plan's words as detail; the same request with confirm: true applies it.

// appObject is an app's object reached through its quickEdit tools, as the person.
type appObject struct {
	d      deps
	reg    *apps.Registry
	app    string
	object string
	tools  apps.QuickEdit
}

func (o appObject) call(ctx context.Context, tool string, args map[string]any) (json.RawMessage, error) {
	args["object"] = o.object
	raw, err := callAppTool(ctx, o.d, o.reg, o.app, tool, args)
	if err != nil {
		return nil, err
	}
	return quickToolBody(raw)
}

// quickToolBody is the JSON a quickEdit tool answered: its structuredContent, else its first text; an isError result
// is the app's {code, message}.
func quickToolBody(raw json.RawMessage) (json.RawMessage, error) {
	var res struct {
		IsError    bool            `json:"isError"`
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, &quickedit.AppError{Code: "failed", Message: "the app's answer is not a tool result"}
	}
	text := ""
	if len(res.Content) > 0 && res.Content[0].Type == "text" {
		text = res.Content[0].Text
	}
	if res.IsError {
		ae := &quickedit.AppError{}
		if json.Unmarshal([]byte(text), ae) != nil || ae.Code == "" {
			ae = &quickedit.AppError{Code: "failed", Message: clip(text, 300)}
		}
		return nil, ae
	}
	if len(res.Structured) > 0 && string(res.Structured) != "null" {
		return res.Structured, nil
	}
	return json.RawMessage(text), nil
}

func (o appObject) Read(ctx context.Context, selection string) (string, string, error) {
	args := map[string]any{}
	if selection != "" {
		args["selection"] = selection
	}
	body, err := o.call(ctx, o.tools.Read, args)
	if err != nil {
		return "", "", err
	}
	var r struct {
		Text    string `json:"text"`
		Version string `json:"version"`
	}
	if json.Unmarshal(body, &r) != nil || r.Version == "" {
		return "", "", &quickedit.AppError{Code: "failed", Message: "the app read no text and version"}
	}
	return r.Text, r.Version, nil
}

func (o appObject) Apply(ctx context.Context, version string, edits []quickedit.Edit, confirmed bool) (quickedit.Applied, error) {
	args := map[string]any{"version": version, "edits": edits}
	if confirmed {
		args["confirmed"] = true
	}
	body, err := o.call(ctx, o.tools.Apply, args)
	if err != nil {
		return quickedit.Applied{}, err
	}
	var a quickedit.Applied
	if json.Unmarshal(body, &a) != nil {
		return quickedit.Applied{}, &quickedit.AppError{Code: "failed", Message: "the app's apply answer is not JSON"}
	}
	return a, nil
}

func (o appObject) Undo(ctx context.Context, version string) error {
	if o.tools.Undo == "" {
		return &quickedit.AppError{Code: "nothing_to_undo", Message: "the app has no undo"}
	}
	_, err := o.call(ctx, o.tools.Undo, map[string]any{"version": version})
	return err
}

func appQuickEditRoutes(d deps, reg *apps.Registry) []gate.Route {
	var routes []gate.Route
	for _, base := range []string{platformAPI + "/voice/edit/app", "/agents" + platformAPI + "/voice/edit/app"} {
		routes = append(routes, gate.Route{
			Match: func(r *http.Request) bool { return r.URL.Path == base || r.URL.Path == base+"/undo" },
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if sameorigin.Refuse(w, r) {
					return
				}
				if !d.SessionOK(r) {
					secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
					return
				}
				if r.Method != http.MethodPost {
					secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST"})
					return
				}
				var req quickedit.ObjectRequest
				if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.App == "" || req.Object == "" {
					secretJSON(w, http.StatusBadRequest, map[string]string{"error": quickedit.CodeBadRequest})
					return
				}
				a, err := reg.Get(req.App)
				if err != nil || a.Manifest == nil || a.Manifest.QuickEdit == nil || a.Off || a.NeedsConsent() {
					secretJSON(w, http.StatusNotFound, map[string]string{"error": "edit_no_quick_edit"})
					return
				}
				obj := appObject{d: d, reg: reg, app: req.App, object: req.Object, tools: *a.Manifest.QuickEdit}
				s := voiceEditService(d)
				var res quickedit.ObjectResult
				if strings.HasSuffix(r.URL.Path, "/undo") {
					res, err = s.UndoObject(r.Context(), req.App, req.Object, obj)
				} else {
					res, err = s.EditObject(r.Context(), req, obj)
				}
				if err != nil {
					status, body := quickedit.Describe(err)
					secretJSON(w, status, body)
					return
				}
				secretJSON(w, http.StatusOK, res)
			}),
		})
	}
	return routes
}
