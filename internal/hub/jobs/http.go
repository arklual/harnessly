package jobs

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Routes are the service's two faces: the web page's under a person's session, and the machines' signed ones.
type Routes struct {
	S *Service
	// Guard is the tenant guard of the hub; Admit checks a machine's signed request (link.Dialer.AdmitJobs).
	Guard tenant.Guard
	Admit func(r *http.Request) (store.Machine, error)
}

// Mount registers the routes. sub is the spaces router (its paths start with /api/spaces/{spaceId}).
func (rt *Routes) Mount(mux, sub *http.ServeMux) {
	g := rt.Guard
	sub.Handle("GET /api/spaces/{spaceId}/builders", g.Space(tenant.Member, rt.webBuilders))
	sub.Handle("GET /api/spaces/{spaceId}/jobs", g.Space(tenant.Member, rt.webList))
	sub.Handle("POST /api/spaces/{spaceId}/jobs", g.Space(tenant.Member, rt.webSubmit))
	sub.Handle("GET /api/spaces/{spaceId}/jobs/{jobId}", g.Space(tenant.Member, rt.webGet))
	sub.Handle("POST /api/spaces/{spaceId}/jobs/{jobId}/cancel", g.Space(tenant.Member, rt.webCancel))
	sub.Handle("GET /api/spaces/{spaceId}/jobs/{jobId}/out", g.Space(tenant.Member, rt.webOut))
	mux.Handle("PUT /api/machines/{machineId}/builder", g.Machine(tenant.Owner, rt.setRole))

	mux.Handle("GET "+hubproto.JobsPath+"/builders", httpx.Handler(rt.agentBuilders))
	mux.Handle("GET "+hubproto.JobsPath, httpx.Handler(rt.agentList))
	mux.Handle("POST "+hubproto.JobsPath, httpx.Handler(rt.agentSubmit))
	mux.Handle("GET "+hubproto.JobsPath+"/{jobId}", httpx.Handler(rt.agentGet))
	mux.Handle("POST "+hubproto.JobsPath+"/{jobId}/cancel", httpx.Handler(rt.agentCancel))
	mux.Handle("POST "+hubproto.JobsPath+"/{jobId}/finish", httpx.Handler(rt.agentFinish))
	mux.Handle("PUT "+hubproto.JobsPath+"/{jobId}/input", httpx.Handler(rt.agentPutInput))
	mux.Handle("GET "+hubproto.JobsPath+"/{jobId}/input", httpx.Handler(rt.agentGetInput))
	mux.Handle("PUT "+hubproto.JobsPath+"/{jobId}/out", httpx.Handler(rt.agentPutOut))
	mux.Handle("GET "+hubproto.JobsPath+"/{jobId}/out", httpx.Handler(rt.agentGetOut))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, v)
}

// ---- the web page

func (rt *Routes) webBuilders(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	bs, err := rt.S.Builders(r.Context(), t.SpaceID)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"builders": bs})
	return nil
}

func (rt *Routes) views(r *http.Request, js []store.HeavyJob) []JobView {
	out := make([]JobView, 0, len(js))
	for _, j := range js {
		out = append(out, rt.S.View(r.Context(), j))
	}
	return out
}

func (rt *Routes) webList(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	js, err := rt.S.Jobs(r.Context(), t.SpaceID, limit)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"jobs": rt.views(r, js)})
	return nil
}

func (rt *Routes) webSubmit(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	var req Request
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	// From a page there is no machine of the caller's own to run a local fallback on: the job fails with the reason.
	j, err := rt.S.Submit(r.Context(), Actor{SpaceID: t.SpaceID, UserID: t.Person.User.ID}, req)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"job": rt.S.View(r.Context(), j)})
	return nil
}

func (rt *Routes) webGet(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	j, err := rt.S.Job(r.Context(), t.SpaceID, r.PathValue("jobId"))
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"job": rt.S.View(r.Context(), j)})
	return nil
}

func (rt *Routes) webCancel(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	j, err := rt.S.Job(r.Context(), t.SpaceID, r.PathValue("jobId"))
	if err != nil {
		return err
	}
	if j.UserID != t.Person.User.ID && t.Role != store.RoleOwner {
		return httpx.ErrForbidden
	}
	j, err = rt.S.Cancel(r.Context(), j)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"job": rt.S.View(r.Context(), j)})
	return nil
}

func (rt *Routes) webOut(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	j, err := rt.S.Job(r.Context(), t.SpaceID, r.PathValue("jobId"))
	if err != nil {
		return err
	}
	return rt.serveOut(w, r, j)
}

func (rt *Routes) serveOut(w http.ResponseWriter, r *http.Request, j store.HeavyJob) error {
	fh, size, err := rt.S.files.OpenOut(j.ID, r.URL.Query().Get("path"))
	if err != nil {
		return httpx.ErrNotFound
	}
	defer fh.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "attachment")
	_, _ = io.Copy(w, fh)
	return nil
}

type roleRequest struct {
	Role  string   `json:"role"`
	Caps  []string `json:"caps"`
	Slots int      `json:"slots"`
}

func (rt *Routes) setRole(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	var req roleRequest
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	b, err := rt.S.SetRole(r.Context(), *t.Machine, req.Role, req.Caps, req.Slots, t.Person.User.ID)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"role": b.Role, "caps": nonNil(b.Caps), "slots": b.Slots})
	return nil
}

// ---- the machines' signed requests

// actor is who a machine's request speaks for: the person who registered the machine, while they are in its space.
func (rt *Routes) actor(r *http.Request) (store.Machine, Actor, error) {
	m, err := rt.Admit(r)
	if err != nil {
		return m, Actor{}, err
	}
	a := Actor{SpaceID: m.SpaceID, UserID: m.CreatedBy, MachineID: m.ID}
	if m.SpaceID == "" || m.CreatedBy == "" {
		return m, a, httpx.ErrForbidden
	}
	if role, err := rt.S.db.S().Role(r.Context(), m.SpaceID, m.CreatedBy); err != nil || role == "" {
		return m, a, httpx.ErrForbidden
	}
	return m, a, nil
}

func (rt *Routes) agentBuilders(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	bs, err := rt.S.Builders(r.Context(), a.SpaceID)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"builders": bs})
	return nil
}

func (rt *Routes) agentList(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	js, err := rt.S.Jobs(r.Context(), a.SpaceID, limit)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"jobs": rt.views(r, js)})
	return nil
}

func (rt *Routes) agentSubmit(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	var req Request
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	j, err := rt.S.Submit(r.Context(), a, req)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"job": rt.S.View(r.Context(), j)})
	return nil
}

// own is a job of the caller's space, and when the caller is a machine of its own, one that machine submitted or runs.
func (rt *Routes) own(r *http.Request, a Actor, asBuilder bool) (store.HeavyJob, error) {
	j, err := rt.S.Job(r.Context(), a.SpaceID, r.PathValue("jobId"))
	if err != nil {
		return j, err
	}
	switch {
	case asBuilder && (j.Local || j.MachineID != a.MachineID):
		return j, httpx.ErrNotFound
	case !asBuilder && j.FromMachine != a.MachineID && j.UserID != a.UserID:
		return j, httpx.ErrNotFound
	}
	return j, nil
}

func (rt *Routes) agentGet(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	j, err := rt.own(r, a, false)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"job": rt.S.View(r.Context(), j)})
	return nil
}

func (rt *Routes) agentCancel(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	j, err := rt.own(r, a, false)
	if err != nil {
		return err
	}
	j, err = rt.S.Cancel(r.Context(), j)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"job": rt.S.View(r.Context(), j)})
	return nil
}

func (rt *Routes) agentFinish(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	j, err := rt.own(r, a, false)
	if err != nil {
		return err
	}
	var body struct {
		ExitCode int    `json:"exitCode"`
		Error    string `json:"error"`
		Log      string `json:"log"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	j, err = rt.S.ReportLocal(r.Context(), j, body.ExitCode, body.Error, body.Log)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"job": rt.S.View(r.Context(), j)})
	return nil
}

func (rt *Routes) agentPutInput(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	j, err := rt.own(r, a, false)
	if err != nil {
		return err
	}
	if !j.HasInput || j.Local || j.Status != hubproto.HeavyQueued {
		return httpx.Conflict("no_input_wanted", "Этой задаче файлы не нужны или она уже запущена")
	}
	n, err := rt.S.files.PutInput(j.ID, r.Body)
	if err != nil {
		return httpx.Invalid("input", err.Error())
	}
	rt.S.Wake()
	writeJSON(w, map[string]any{"size": n})
	return nil
}

func (rt *Routes) agentGetInput(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	j, err := rt.own(r, a, true)
	if err != nil {
		return err
	}
	fh, size, err := rt.S.files.OpenInput(j.ID)
	if err != nil {
		return httpx.ErrNotFound
	}
	defer fh.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, fh)
	return nil
}

func (rt *Routes) agentPutOut(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	j, err := rt.own(r, a, true)
	if err != nil {
		return err
	}
	if !hubproto.HeavyActive(j.Status) {
		return httpx.Conflict("job_over", "Задача уже закончилась")
	}
	n, err := rt.S.files.PutOut(j.ID, r.URL.Query().Get("path"), r.Body)
	if err != nil {
		return httpx.Invalid("path", err.Error())
	}
	writeJSON(w, map[string]any{"size": n})
	return nil
}

func (rt *Routes) agentGetOut(w http.ResponseWriter, r *http.Request) error {
	_, a, err := rt.actor(r)
	if err != nil {
		return err
	}
	j, err := rt.own(r, a, false)
	if err != nil {
		return err
	}
	return rt.serveOut(w, r, j)
}
