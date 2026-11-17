package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agent"
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
//	POST <platform>/hub-env/own        the person's page: their own skill or MCP server {kind, name, hint, source}
//	DELETE <platform>/hub-env/own/<id> the person's page: their own item taken out
//
// The page gets metadata only (never an item's source: it may name a secret). RUNTIME_HUB_ENV_UI=on turns the section
// on; RUNTIME_HUB_ENV_OWN=on lets the page switch items and add its own.
//
// A switch and one's own items are the person's own layer at the hub (docs/hub/environment.md «Own layer»). The runtime
// holds no hub session: it asks the machine agent (POST /env/own on its local port), which sends env.own over the
// machine's link; the hub changes the layer of the person the machine is theirs, and only when the organization has the
// own layer. The answer is the hub's: its status and words. The new list reaches the machine as any list does.
//
// A switched-off item leaves the machine's list, so the runtime remembers what was switched off here
// (<data dir>/hub-env-off.json) to offer switching it back on; an item back in the list is forgotten there.

// hubEnvItem is one row of the section.
type hubEnvItem struct {
	ItemID  string `json:"itemId"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Layer   string `json:"layer"`
	Pinned  bool   `json:"pinned"`
	Status  string `json:"status"`
	Message string `json:"message"`
	// Own: the person's own item (layer "user"): theirs to remove. Off: switched off from here, not on the machine.
	Own bool `json:"own,omitempty"`
	Off bool `json:"off,omitempty"`
}

type hubEnvAnswer struct {
	Enabled   bool         `json:"enabled"`
	CanSwitch bool         `json:"canSwitch"`
	CanOwn    bool         `json:"canOwn"`
	Items     []hubEnvItem `json:"items"`
}

// hubEnvOwnOn: RUNTIME_HUB_ENV_OWN=on (RUNTIME_HUB_ENV_SWITCH=on, its earlier name, too).
func hubEnvOwnOn() bool {
	return os.Getenv("RUNTIME_HUB_ENV_OWN") == "on" || os.Getenv("RUNTIME_HUB_ENV_SWITCH") == "on"
}

// envOwnCaller sends one change of the own layer to the machine agent.
type envOwnCaller func(hubproto.EnvOwnRequest) (hubproto.EnvOwnResult, error)

func agentEnvOwn(dataDir string) envOwnCaller {
	header := os.Getenv("RUNTIME_AGENT_TOKEN_HEADER")
	return func(p hubproto.EnvOwnRequest) (hubproto.EnvOwnResult, error) {
		var out hubproto.EnvOwnResult
		status, body, err := agent.CallLocal(nil, dataDir, header, "/env/own", p, envProposeTimeout)
		if err != nil {
			return out, err
		}
		if status != http.StatusOK || json.Unmarshal(body, &out) != nil || out.Status == 0 {
			return out, errors.New("the machine's agent did not pass the change on")
		}
		return out, nil
	}
}

// hubEnvOffs is what was switched off from this machine's page, by item id.
type hubEnvOffs struct {
	path string
	mu   sync.Mutex
}

func (o *hubEnvOffs) read() map[string]hubEnvItem {
	out := map[string]hubEnvItem{}
	if raw, err := os.ReadFile(o.path); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (o *hubEnvOffs) set(it hubEnvItem, off bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	all := o.read()
	if off {
		it.Off, it.Status, it.Message = true, "off", ""
		all[it.ItemID] = it
	} else {
		delete(all, it.ItemID)
	}
	if raw, err := json.Marshal(all); err == nil {
		_ = os.WriteFile(o.path, raw, 0o600)
	}
}

// with adds the items switched off here that are not in the machine's list; one that is back is forgotten.
func (o *hubEnvOffs) with(items []hubEnvItem) []hubEnvItem {
	o.mu.Lock()
	defer o.mu.Unlock()
	all := o.read()
	if len(all) == 0 {
		return items
	}
	held := map[string]bool{}
	for _, it := range items {
		held[it.ItemID] = true
	}
	changed := false
	for id, it := range all {
		if held[id] {
			delete(all, id)
			changed = true
			continue
		}
		items = append(items, it)
	}
	if changed {
		if raw, err := json.Marshal(all); err == nil {
			_ = os.WriteFile(o.path, raw, 0o600)
		}
	}
	return items
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
			row := hubEnvItem{ItemID: it.ItemID, Kind: it.Kind, Name: it.Name, Layer: it.Layer, Pinned: it.Pinned, Status: "pending", Own: it.Layer == "user"}
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
	return hubEnvRoutesWith(d, agentEnvOwn(d.DataDir))
}

func hubEnvRoutesWith(d deps, call envOwnCaller) []gate.Route {
	base := platformAPI + "/hub-env"
	dirs := hubLinkDirs(d.DataDir, os.Getenv("AGENT_HUB_OBSERVE_FILE"))
	offs := &hubEnvOffs{path: filepath.Join(d.DataDir, "hub-env-off.json")}
	// answer passes the hub's answer on: its status and its words.
	answer := func(w http.ResponseWriter, res hubproto.EnvOwnResult, err error) bool {
		if err != nil {
			notifyJSON(w, http.StatusBadGateway, map[string]string{"error": "agent-unreachable", "message": "Агент машины не ответил"})
			return false
		}
		if res.Status >= 300 {
			notifyJSON(w, res.Status, map[string]string{"error": res.Code, "message": res.Message})
			return false
		}
		return true
	}
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
		on := hubEnvOwnOn()
		items := readHubEnv(dirs)
		if on {
			items = offs.with(items)
		}
		notifyJSON(w, http.StatusOK, hubEnvAnswer{Enabled: true, CanSwitch: on, CanOwn: on, Items: items})
	}
	one := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if os.Getenv("RUNTIME_HUB_ENV_UI") != "on" || !hubEnvOwnOn() {
			notifyJSON(w, http.StatusNotFound, map[string]string{"error": "not-enabled"})
			return
		}
		rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents"), base+"/")
		switch {
		case rest == "own" && r.Method == http.MethodPost:
			var in struct {
				Kind   string          `json:"kind"`
				Name   string          `json:"name"`
				Hint   string          `json:"hint"`
				Source json.RawMessage `json:"source"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024)).Decode(&in) != nil || !hubEnvKinds[in.Kind] {
				notifyJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": "Нужны kind (skill или mcp), name и source"})
				return
			}
			res, err := call(hubproto.EnvOwnRequest{Action: hubproto.EnvOwnAdd, Kind: in.Kind, Name: &in.Name, Hint: &in.Hint, Source: in.Source})
			if answer(w, res, err) {
				notifyJSON(w, http.StatusCreated, map[string]string{"itemId": res.ItemID})
			}
		case strings.HasPrefix(rest, "own/") && r.Method == http.MethodDelete:
			id := strings.TrimPrefix(rest, "own/")
			res, err := call(hubproto.EnvOwnRequest{Action: hubproto.EnvOwnRemove, ItemID: id})
			if answer(w, res, err) {
				offs.set(hubEnvItem{ItemID: id}, false)
				w.WriteHeader(http.StatusNoContent)
			}
		case rest != "" && !strings.Contains(rest, "/") && r.Method == http.MethodPut:
			var in struct {
				Off *bool `json:"off"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4*1024)).Decode(&in) != nil || in.Off == nil {
				notifyJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request", "message": "Нужно off: true или false"})
				return
			}
			var row hubEnvItem
			for _, it := range offs.with(readHubEnv(dirs)) {
				if it.ItemID == rest {
					row = it
				}
			}
			if row.ItemID == "" {
				notifyJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
				return
			}
			res, err := call(hubproto.EnvOwnRequest{Action: hubproto.EnvOwnSwitch, ItemID: rest, Off: *in.Off})
			if answer(w, res, err) {
				offs.set(row, *in.Off)
				notifyJSON(w, http.StatusOK, map[string]bool{"off": *in.Off})
			}
		default:
			notifyJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		}
	}
	return []gate.Route{
		{Match: func(r *http.Request) bool { return pathIs(r, base) }, Handler: http.HandlerFunc(list)},
		{Match: func(r *http.Request) bool {
			return strings.HasPrefix(r.URL.Path, base+"/") || strings.HasPrefix(r.URL.Path, "/agents"+base+"/")
		}, Handler: http.HandlerFunc(one)},
	}
}
