package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// «From the organization» in Settings → Skills and MCP: the skills and MCP servers the hub handed this machine and how
// the machine agent applied each (the agent keeps them in <link dir>/environment.json, internal/agent/envkeeper.go).
// Who may call what:
//
//	GET <platform>/hub-env        the person's page: a page session
//	PUT <platform>/hub-env/<id>   the person's page: a personal switch of an item that is not pinned
//
// The page gets metadata only (never an item's source: it may name a secret). RUNTIME_HUB_ENV_UI=on turns the section
// on; RUNTIME_HUB_ENV_SWITCH=on lets the page offer the personal switch (see PUT).
//
// The personal switch is the hub's (PUT /api/me/env/{itemId}, internal/hub/env/api_me.go) and needs the person's hub
// session, which the runtime does not hold. Until the hub takes a machine-authenticated switch (HRN-667), PUT answers
// 501 «no-hub-session» and changes nothing.

// hubEnvItem is one row of the section.
type hubEnvItem struct {
	ItemID  string `json:"itemId"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Layer   string `json:"layer"`
	Pinned  bool   `json:"pinned"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type hubEnvAnswer struct {
	Enabled   bool         `json:"enabled"`
	CanSwitch bool         `json:"canSwitch"`
	Items     []hubEnvItem `json:"items"`
}

type hubEnvHeld struct {
	Items   []hubproto.EnvItem    `json:"items"`
	Applied []hubproto.EnvApplied `json:"applied"`
}

// hubEnvKinds are the kinds the section lists: what an agent can reach. Repos, docs, scripts and access are the
// machine's setup, not the person's tools.
var hubEnvKinds = map[string]bool{"skill": true, "mcp": true}

// readHubEnv reads the first link folder that holds a list; none is an empty list.
func readHubEnv(dirs []string) []hubEnvItem {
	out := []hubEnvItem{}
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, "environment.json"))
		if err != nil {
			continue
		}
		var h hubEnvHeld
		if json.Unmarshal(raw, &h) != nil {
			continue
		}
		applied := map[string]hubproto.EnvApplied{}
		for _, a := range h.Applied {
			applied[a.ItemID] = a
		}
		for _, it := range h.Items {
			if !hubEnvKinds[it.Kind] {
				continue
			}
			row := hubEnvItem{ItemID: it.ItemID, Kind: it.Kind, Name: it.Name, Layer: it.Layer, Pinned: it.Pinned, Status: "pending"}
			if a, ok := applied[it.ItemID]; ok {
				row.Status, row.Message = a.Status, a.Message
			}
			out = append(out, row)
		}
		return out
	}
	return out
}

func hubEnvRoutes(d deps) []gate.Route {
	base := platformAPI + "/hub-env"
	dirs := hubLinkDirs(d.DataDir, os.Getenv("AGENT_HUB_OBSERVE_FILE"))
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			notifyJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	list := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			notifyJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		if os.Getenv("RUNTIME_HUB_ENV_UI") != "on" {
			notifyJSON(w, http.StatusOK, hubEnvAnswer{Items: []hubEnvItem{}})
			return
		}
		notifyJSON(w, http.StatusOK, hubEnvAnswer{
			Enabled:   true,
			CanSwitch: os.Getenv("RUNTIME_HUB_ENV_SWITCH") == "on",
			Items:     readHubEnv(dirs),
		})
	}
	one := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodPut {
			notifyJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		notifyJSON(w, http.StatusNotImplemented, map[string]string{"error": "no-hub-session"})
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(list)},
		{Match: func(r *http.Request) bool {
			return strings.HasPrefix(r.URL.Path, base+"/") || strings.HasPrefix(r.URL.Path, "/agents"+base+"/")
		}, Handler: http.HandlerFunc(one)},
	}
}
