package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// The apps' events travel runtime → this agent over the loopback, and agent → cluster as events:
//
//	apps.team.changed                          the team's catalog or installs changed on this machine
//	apps.public.changed                        this machine put an app in the public catalog
//	apps.guest.created {id, hash, expiresAt}   a guest link was made (only the hash of its token goes)
//	apps.guest.revoked {id}                    a guest link ended
//
// The agent keeps the live guest links (appsGuests), so a link that comes up later learns of them all.

const (
	AppsTeamChanged   = "apps.team.changed"
	AppsPublicChanged = "apps.public.changed"
	AppsGuestCreate   = "apps.guest.created"
	AppsGuestRevoke   = "apps.guest.revoked"
	// AppsGuestSync is runtime → agent only: the full set of live links, which replaces what the agent holds.
	AppsGuestSync = "apps.guest.sync"
)

// AppsGuest is one live guest link as the cluster learns of it.
type AppsGuest struct {
	ID        string `json:"id"`
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expiresAt"`
}

type evAppsTeam struct {
	Event string `json:"event"`
}

type evAppsGuestCreated struct {
	Event     string `json:"event"`
	ID        string `json:"id"`
	Hash      string `json:"hash"`
	ExpiresAt int64  `json:"expiresAt"`
}

type evAppsGuestRevoked struct {
	Event string `json:"event"`
	ID    string `json:"id"`
}

// AppsEvent is the body of the runtime's POST /apps/event.
type AppsEvent struct {
	Event string      `json:"event"`
	ID    string      `json:"id,omitempty"`
	Hash  string      `json:"hash,omitempty"`
	At    int64       `json:"expiresAt,omitempty"`
	Links []AppsGuest `json:"links,omitempty"`
}

// appsGuests are the live guest links, kept in memory and in apps-guest-links.json of the data directory.
type appsGuests struct {
	mu    sync.Mutex
	file  string
	links map[string]AppsGuest
	now   func() time.Time
}

func newAppsGuests(dataDir string) *appsGuests {
	g := &appsGuests{file: filepath.Join(dataDir, "apps-guest-links.json"), links: map[string]AppsGuest{}, now: time.Now}
	if raw, err := os.ReadFile(g.file); err == nil {
		var list []AppsGuest
		if json.Unmarshal(raw, &list) == nil {
			for _, l := range list {
				g.links[l.ID] = l
			}
		}
	}
	return g
}

func (g *appsGuests) save() {
	list := g.live()
	raw, _ := json.Marshal(list)
	tmp := g.file + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, g.file)
	}
}

// live are the links that have not ended, by id.
func (g *appsGuests) live() []AppsGuest {
	now := g.now().UnixMilli()
	out := []AppsGuest{}
	for _, l := range g.links {
		if l.ExpiresAt > now {
			out = append(out, l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func validGuest(l AppsGuest) bool {
	if l.ID == "" || len(l.ID) > 64 || l.ExpiresAt <= 0 || len(l.Hash) != 64 {
		return false
	}
	for _, c := range l.Hash {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// guestStore is the live guest links, read from the data directory on first use.
func (a *Agent) guestStore() *appsGuests {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.appsGuests == nil {
		a.appsGuests = newAppsGuests(a.cfg.DataDir)
	}
	return a.appsGuests
}

// appsEventRoute is POST /apps/event: the event goes to every cluster link, and the guest links are remembered.
func (a *Agent) appsEventRoute(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(r.Header.Get(a.cfg.LocalTokenHeader), a.localToken) {
		reply(w, 403, map[string]string{"error": "no local agent token (agent.json in the data directory)"})
		return
	}
	raw := readObject(r, 4<<20)
	if raw == nil {
		reply(w, 400, map[string]string{"error": "an event is required"})
		return
	}
	var ev AppsEvent
	body, _ := json.Marshal(raw)
	if json.Unmarshal(body, &ev) != nil {
		reply(w, 400, map[string]string{"error": "bad event"})
		return
	}
	g := a.guestStore()
	switch ev.Event {
	case AppsTeamChanged:
		a.broadcast(evAppsTeam{Event: AppsTeamChanged})
	case AppsPublicChanged:
		a.broadcast(evAppsTeam{Event: AppsPublicChanged})
	case AppsGuestCreate:
		l := AppsGuest{ID: ev.ID, Hash: ev.Hash, ExpiresAt: ev.At}
		if !validGuest(l) {
			reply(w, 400, map[string]string{"error": "id, hash (sha-256, hex) and expiresAt are required"})
			return
		}
		g.mu.Lock()
		g.links[l.ID] = l
		g.save()
		g.mu.Unlock()
		a.broadcast(evAppsGuestCreated{Event: AppsGuestCreate, ID: l.ID, Hash: l.Hash, ExpiresAt: l.ExpiresAt})
	case AppsGuestRevoke:
		if ev.ID == "" {
			reply(w, 400, map[string]string{"error": "id is required"})
			return
		}
		g.mu.Lock()
		delete(g.links, ev.ID)
		g.save()
		g.mu.Unlock()
		a.broadcast(evAppsGuestRevoked{Event: AppsGuestRevoke, ID: ev.ID})
	case AppsGuestSync:
		next := map[string]AppsGuest{}
		for _, l := range ev.Links {
			if validGuest(l) {
				next[l.ID] = l
			}
		}
		g.mu.Lock()
		var gone []string
		for _, l := range g.live() {
			if _, ok := next[l.ID]; !ok {
				gone = append(gone, l.ID)
			}
		}
		g.links = next
		g.save()
		live := g.live()
		g.mu.Unlock()
		for _, id := range gone {
			a.broadcast(evAppsGuestRevoked{Event: AppsGuestRevoke, ID: id})
		}
		for _, l := range live {
			a.broadcast(evAppsGuestCreated{Event: AppsGuestCreate, ID: l.ID, Hash: l.Hash, ExpiresAt: l.ExpiresAt})
		}
	default:
		reply(w, 400, map[string]string{"error": "unknown event"})
		return
	}
	reply(w, 202, map[string]bool{"ok": true, "sent": a.connected()})
}

// replayAppsGuests tells a link that has just come up of every live guest link.
func (a *Agent) replayAppsGuests(l *link) {
	g := a.guestStore()
	g.mu.Lock()
	live := g.live()
	g.mu.Unlock()
	for _, x := range live {
		if frame, err := marshal(evAppsGuestCreated{Event: AppsGuestCreate, ID: x.ID, Hash: x.Hash, ExpiresAt: x.ExpiresAt}); err == nil {
			l.sendText(frame)
		}
	}
}

// EmitAppsEvent asks the machine agent whose agent.json lies in dataDir to send an apps event to the cluster. It never
// blocks the caller and never reports a failure: no agent, no event.
func EmitAppsEvent(dataDir, tokenHeader string, ev AppsEvent) {
	if dataDir == "" || ev.Event == "" {
		return
	}
	go func() {
		if err := SendAppsEvent(dataDir, tokenHeader, ev); err != nil {
			Log("warn", "apps event not handed to the agent", F{"event", ev.Event}, F{"error", err.Error()})
		}
	}()
}

// SendAppsEvent hands one event to the machine agent and says whether it took it.
func SendAppsEvent(dataDir, tokenHeader string, ev AppsEvent) error {
	port, token, ok := localAgent(dataDir)
	if !ok {
		return errNoLocalAgent
	}
	body, _ := json.Marshal(ev)
	ctx, cancel := context.WithTimeout(context.Background(), designEventTO)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:"+port+"/apps/event", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	if tokenHeader == "" {
		tokenHeader = "x-agent-local-token"
	}
	req.Header.Set(tokenHeader, token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		return fmt.Errorf("the agent answered %d", res.StatusCode)
	}
	return nil
}

var errNoLocalAgent = errors.New("no machine agent on this machine")
