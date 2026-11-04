package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

type browserProfileAction struct {
	Action    string   `json:"action"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Color     string   `json:"color"`
	Path      string   `json:"path"`
	Conv      string   `json:"conv"`
	Project   string   `json:"project"`
	Default   string   `json:"default"`
	Granted   []string `json:"granted"`
	Scope     string   `json:"scope"`
	AskID     string   `json:"askId"`
	NoSandbox bool     `json:"noSandbox"`
}

type browserProfileView struct {
	agentbrowser.Profile
	Path      string   `json:"path"`
	Running   bool     `json:"running"`
	Projects  []string `json:"projects"`
	SizeBytes *int64   `json:"sizeBytes,omitempty"`
}

func agentBrowserProfileRoutes(d deps, a *agentbrowser.ProfileAccess, projectExists func(string) bool) []gate.Route {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if !a.Manager.Enabled() {
			secretJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Agent browser is disabled"})
			return
		}
		if err := a.Store.Err(); err != nil {
			secretJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		switch r.Method {
		case http.MethodGet:
			browserProfilesRead(w, r, d, a, projectExists)
		case http.MethodPost:
			var b browserProfileAction
			if !readSecretBody(r, &b) {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request"})
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
			defer cancel()
			warning, err := browserProfilesChange(ctx, a, b, projectExists)
			if err != nil {
				secretJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"ok": true, "warning": warning})
		default:
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		}
	})
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, "/api/agent-browser/profiles") }, Handler: handler}}
}

func browserProfilesRead(w http.ResponseWriter, r *http.Request, d deps, a *agentbrowser.ProfileAccess, exists func(string) bool) {
	if conv := r.URL.Query().Get("conv"); conv != "" {
		profiles, current, err := a.List(conv)
		if err != nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"profiles": profiles, "current": current, "hasTabs": a.Manager.ConversationHasTabs(conv)})
		return
	}
	_, projects := a.Store.Snapshot()
	// The current protocol has project upserts but no removal event. Reconcile against the runtime's local catalog.
	live := map[string]bool{}
	for id := range projects {
		if exists(id) {
			live[id] = true
		}
	}
	if err := a.ReconcileProjects(live); err != nil {
		secretJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	profiles, projects := a.Store.Snapshot()
	views := make([]browserProfileView, 0, len(profiles))
	for _, p := range profiles {
		used := []string{}
		for id, access := range projects {
			if access.Default == p.ID || slices.Contains(access.Granted, p.ID) {
				used = append(used, id)
			}
		}
		sort.Strings(used)
		p.CDP = "" // Endpoint credentials never belong in settings metadata.
		views = append(views, browserProfileView{Profile: p, Path: filepath.Join(d.DataDir, "agent-browser", "profiles", p.ID, "chrome"),
			Running: a.Manager.ProfileRunning(p.ID), Projects: used, SizeBytes: a.Store.SizeBytes(p.ID)})
	}
	secretJSON(w, http.StatusOK, map[string]any{"profiles": views, "projects": projects, "asks": a.Asks.Pending(), "noSandbox": a.Store.NoSandbox()})
}

func browserProfilesChange(ctx context.Context, a *agentbrowser.ProfileAccess, b browserProfileAction, exists func(string) bool) (string, error) {
	switch b.Action {
	case "create":
		_, err := a.Store.Create(b.Name, b.Color)
		return "", err
	case "update":
		return "", a.Store.Update(b.ID, b.Name, b.Color)
	case "stop":
		return "", a.Manager.StopProfile(ctx, b.ID)
	case "clear":
		return "", a.Manager.ClearProfile(ctx, b.ID)
	case "delete":
		return "", a.Delete(ctx, b.ID)
	case "use":
		return "", a.UseID(ctx, b.Conv, b.ID)
	case "answer":
		return "", a.Answer(ctx, b.AskID, b.ID, b.Scope, b.Name, b.Color)
	case "project":
		if !exists(b.Project) {
			return "", errors.New("Project is unavailable")
		}
		return "", a.SetProject(ctx, b.Project, agentbrowser.ProjectAccess{Default: b.Default, Granted: b.Granted})
	case "sandbox":
		if b.NoSandbox && !a.Store.NoSandbox() {
			if err := browserSandboxFallback(ctx, a.Manager); err != nil {
				return "", err
			}
		}
		if err := a.Store.SetNoSandbox(b.NoSandbox); err != nil {
			return "", err
		}
		a.Manager.Retry(ctx)
		return "", nil
	case "import":
		return a.Store.Import(ctx, b.Path, b.Name, b.Color)
	default:
		return "", errors.New("Unknown profile action")
	}
}

// A sandbox bypass is a repair for an unavailable sandbox, not a general performance setting.
func browserSandboxFallback(ctx context.Context, m *agentbrowser.Manager) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := m.Ready(ctx)
		if status.State != "preparing" {
			if status.State == "error" && strings.HasPrefix(status.Error, "Chrome sandbox is unavailable.") {
				return nil
			}
			return errors.New("Running without a sandbox is available only when Chrome's sandbox is unavailable")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
