package main

import (
	"net/http"
	"path/filepath"

	"harnessly/internal/checkpoints"
	"harnessly/internal/gate"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/machine"
	"harnessly/internal/nodejson"
)

// The checkpoints of the timeline slice: the platform pages' route over a conversation's snapshots, and the snapshot
// taken when an agent's turn ends. The shadow repositories are the previous runtime's own, so the slice can flip either
// way at any moment.

// checkpointStore is where the core keeps the snapshots; the deployment names the refs as the previous runtime did.
func (c *core) checkpointStore() *checkpoints.Store {
	return &checkpoints.Store{Dir: filepath.Join(c.cfg.DataDir, "checkpoints"), Home: c.cfg.Home,
		Namespace: env("RUNTIME_CHECKPOINT_NAMESPACE", "harnessly"), TmpTag: env("RUNTIME_CHECKPOINT_TMP_TAG", "hs")}
}

// turnEnd snapshots a tab's folder once its agent's turn ended: here while the timeline slice runs, else by the old
// runtime, which keeps the checkpoints then.
func (c *core) turnEnd(tabID, cwd string) {
	if c.timelineOn() {
		c.checkpointStore().SnapshotTurnEnd(tabID, cwd)
		return
	}
	c.oldPost("turn-end", map[string]string{"tabId": tabID, "cwd": cwd})
}

// checkpointRoutes: GET (shadowed under "timeline-shadow") and POST of the checkpoints route, while the core runs.
func (c *core) checkpointRoutes(f *gate.Gate, d deps) []gate.Route {
	api := &checkpoints.API{
		Store: c.checkpointStore(),
		Tab: func(tabID string) (string, string, bool) {
			m := c.manager()
			if m == nil {
				return "", "", false
			}
			for _, s := range m.AgentSources() {
				if s.TabID == tabID {
					return s.TmuxSession, s.CliState, true
				}
			}
			return "", "", false
		},
		Cwd: func(session string) string { return machine.SessionDir(c.cfg.Tmux, session) },
	}
	return f.AsRoutes(checkpointsRoute(d, func() bool { return c.manager() != nil }, api))
}

func checkpointsRoute(d deps, coreOn func() bool, api *checkpoints.API) gate.JSONRoute {
	return gate.JSONRoute{
		Slice: "timeline",
		Match: func(r *http.Request) bool { return pathIs(r, platformAPI+"/checkpoints") && coreOn() },
		Compute: func(r *http.Request) (int, any, http.Header) {
			if status, v, h, refused := d.APIRefusal(r); refused {
				return status, v, h
			}
			body, refusal := readPromptBody(r)
			if refusal != nil {
				return refusal.status, gate.Raw{ContentType: "text/plain", Body: []byte(refusal.text)}, nil
			}
			if !d.SessionOK(r) { // a cli-token alone is not a person
				return answerJSON(r, http.StatusForbidden, map[string]string{"error": "Forbidden"}, nil)
			}
			req := checkpoints.Request{Method: r.Method, ContentType: r.Header.Get("Content-Type")}
			if r.Method == http.MethodPost {
				v, _ := jsvalue.Parse([]byte(jscompat.DecodeUTF8(body)))
				if o, ok := v.(*jsvalue.Object); ok {
					req.TabID.Value, req.TabID.Present = o.Get("tabId")
					req.ID.Value, req.ID.Present = o.Get("id")
				}
			} else {
				q := jscompat.Query(r.URL.RawQuery)
				req.TabID = queryParam(q["tabId"])
				req.ID = queryParam(q["id"])
			}
			status, v, h := api.Serve(req)
			switch x := v.(type) {
			case nil:
				return status, nil, h
			case checkpoints.TextAnswer:
				return status, gate.Raw{ContentType: "text/plain", Body: []byte(x)}, h
			}
			return answerJSON(r, status, v, h)
		},
	}
}

// answerJSON is res.json of the old dispatcher: the ETag of the bytes, and a 304 that keeps the JSON content type.
func answerJSON(r *http.Request, status int, v any, h http.Header) (int, any, http.Header) {
	status, body, h := nodejson.Answer(r, status, v, h)
	if status == http.StatusNotModified {
		h.Set("Content-Type", "application/json; charset=utf-8")
	}
	return status, body, h
}

// queryParam is a query value as the previous runtime's handler took it: a string, or an array when repeated.
func queryParam(vs []string) checkpoints.Param {
	switch len(vs) {
	case 0:
		return checkpoints.Param{}
	case 1:
		return checkpoints.Param{Value: vs[0], Present: true}
	}
	return checkpoints.Param{Value: jsvalue.StringArray(vs), Present: true}
}
