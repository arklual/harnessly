package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Messages between agents of different machines (docs/hub/peers.md). A machine proves itself with its own key; the hub
// says who the sender is (that machine, and a person of its space it can vouch for), finds the other machine among the
// ones the sender may reach (the same space, or a space of the same organization), and hands the message over that
// machine's link. The other machine's runtime decides by its own settings whether to take it.

const (
	// peersPerMinute is how many messages a machine sends, and how many times it lists the others' chats, in a minute.
	peersPerMinute = 30
	// peersDirectoryWait is how long the directory waits for each machine's answer.
	peersDirectoryWait = 5 * time.Second
	// peersMaxMachines bounds the machines one directory request asks.
	peersMaxMachines = 64
	peersMaxBody     = 1 << 20
	// peersDeliverWait is how long a message waits for the other machine's answer.
	peersDeliverWait = 15 * time.Second
)

// peerSender is who the hub says a machine's message comes from. The person the machine names is believed as the site
// session believes it (siteSession): they registered the machine, or opened it through the hub lately, and are still
// members of its space. Otherwise it is the person who registered the machine; with none, the machine cannot send.
func (h *Hub) peerSender(ctx context.Context, m store.Machine, claimed string) (hubproto.PeerSender, bool) {
	s := hubproto.PeerSender{Machine: hubproto.PeerRefPrefix + m.ID, MachineName: m.Name}
	person := func(u store.User) hubproto.PeerSender {
		s.Person, s.PersonName = u.Email, u.Name
		return s
	}
	if email := strings.ToLower(strings.TrimSpace(claimed)); email != "" {
		if u, err := h.db.S().UserByEmail(ctx, email); err == nil && (u.ID == m.CreatedBy || h.Tickets.OpenedRecently(u.ID, m.ID)) {
			if role, err := h.db.S().Role(ctx, m.SpaceID, u.ID); err == nil && role != "" {
				return person(u), true
			}
		}
	}
	if m.CreatedBy == "" {
		return s, false
	}
	u, err := h.db.S().UserByID(ctx, m.CreatedBy)
	if err != nil || u.Email == "" {
		return s, false
	}
	return person(u), true
}

// peerSpaces is the spaces a machine of space may reach: its own, and the other spaces of its organization.
func (h *Hub) peerSpaces(ctx context.Context, spaceID string) (map[string]bool, error) {
	sp, err := h.db.S().SpaceByID(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{sp.ID: true}
	if sp.OrgID == "" {
		return out, nil
	}
	spaces, err := h.db.S().SpacesByOrg(ctx, sp.OrgID)
	if err != nil {
		return nil, err
	}
	for _, s := range spaces {
		out[s.ID] = true
	}
	return out, nil
}

// peerTarget is the machine a ref names, if the sender may reach it and it is on; false says nothing of why.
func (h *Hub) peerTarget(ctx context.Context, from store.Machine, machineID string) (store.Machine, bool) {
	if machineID == "" || machineID == from.ID {
		return store.Machine{}, false
	}
	m, err := h.db.S().MachineByID(ctx, machineID)
	if err != nil || m.RevokedAt != nil || m.DisabledAt != nil {
		return store.Machine{}, false
	}
	spaces, err := h.peerSpaces(ctx, from.SpaceID)
	if err != nil || !spaces[m.SpaceID] {
		return store.Machine{}, false
	}
	return m, true
}

// peerChat rewrites a chat a machine's runtime listed into the hub's terms: the machine is "m-<id>" and its name the
// hub's, so the sender can write back to the ref it was given and never learns the machine's own hostname or fleet id.
func peerChat(c hubproto.PeerChat, m store.Machine) hubproto.PeerChat {
	c.Machine, c.MachineName = hubproto.PeerRefPrefix+m.ID, m.Name
	c.Ref = c.Machine + "/" + c.Conv
	return c
}

// peerRef splits "m-<machine id>/<conversation id>".
func peerRef(ref string) (machineID, conv string, ok bool) {
	machine, conv, found := strings.Cut(strings.TrimSpace(ref), "/")
	machineID, isHub := strings.CutPrefix(machine, hubproto.PeerRefPrefix)
	if !found || !isHub || machineID == "" || conv == "" || strings.Contains(conv, "/") {
		return "", "", false
	}
	return machineID, conv, true
}

// peersSend is POST /agent/peers/send: a machine sends a message to a chat of another machine.
func (h *Hub) peersSend(w http.ResponseWriter, r *http.Request) error {
	from, err := h.Dialer.AdmitPeers(r)
	if err != nil {
		return err
	}
	var body hubproto.PeersSend
	if err := json.NewDecoder(io.LimitReader(r.Body, peersMaxBody)).Decode(&body); err != nil {
		return httpx.Invalid("message", "Не сообщение")
	}
	msg := body.Message
	answer := func(res hubproto.PeerResult) error {
		w.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(w, http.StatusOK, res)
		return nil
	}
	if ok, _ := h.limiter.Allow("peers:"+from.ID, peersPerMinute, time.Minute); !ok {
		return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerRateLimited, ""))
	}
	sender, ok := h.peerSender(r.Context(), from, body.Person)
	if !ok {
		return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerUnverified, "the machine has no person the hub can vouch for"))
	}
	machineID, conv, ok := peerRef(msg.To)
	if !ok {
		return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerBadRef, "a chat of another machine is m-<machine>/<chat>"))
	}
	// An unknown machine, one out of reach and one switched off answer alike: the sender learns nothing of which exist.
	to, ok := h.peerTarget(r.Context(), from, machineID)
	if !ok {
		return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerUnreachable, ""))
	}
	l := h.Registry.Get(to.ID)
	if l == nil {
		return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerUnreachable, "the machine is offline"))
	}
	in := hubproto.PeerInbound{ID: msg.ID, To: conv, FromConv: msg.FromConv, FromTitle: msg.FromTitle, Text: msg.Text, Via: msg.Via}
	ctx, cancel := context.WithTimeout(r.Context(), peersDeliverWait)
	defer cancel()
	var res hubproto.PeerResult
	if err := l.CallInto(ctx, hubproto.OpPeersDeliver, hubproto.PeersDeliver{Sender: sender, Message: in}, &res); err != nil {
		var ae *hubproto.Error
		if errors.As(err, &ae) && ae.Code == hubproto.CodePolicyDenied {
			return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerOff, "messages between machines are off on that machine"))
		}
		return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerUnreachable, ""))
	}
	if res.State != hubproto.PeerDelivered && res.State != hubproto.PeerRefused {
		return answer(hubproto.PeerRefusal(msg.ID, hubproto.PeerUnreachable, "the machine gave no result"))
	}
	res.ID = msg.ID
	if res.To != nil {
		c := peerChat(*res.To, to)
		res.To = &c
	}
	h.log.Info("peer message", "from", from.ID, "to", to.ID, "state", res.State, "reason", res.Reason)
	return answer(res)
}

// peersDirectory is POST /agent/peers/directory: the chats of the other machines the sender may write to, as each of
// those machines' runtimes lists them for this sender.
func (h *Hub) peersDirectory(w http.ResponseWriter, r *http.Request) error {
	from, err := h.Dialer.AdmitPeers(r)
	if err != nil {
		return err
	}
	var body struct {
		Person string `json:"person"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	if ok, retry := h.limiter.Allow("peers:"+from.ID, peersPerMinute, time.Minute); !ok {
		return httpx.RateLimited(int(retry / time.Second))
	}
	out := hubproto.PeerChats{Chats: []hubproto.PeerChat{}}
	sender, ok := h.peerSender(r.Context(), from, body.Person)
	if !ok {
		httpx.WriteJSON(w, http.StatusOK, out)
		return nil
	}
	spaces, err := h.peerSpaces(r.Context(), from.SpaceID)
	if err != nil {
		return err
	}
	var targets []store.Machine
	for sp := range spaces {
		ms, err := h.db.S().MachinesBySpace(r.Context(), sp)
		if err != nil {
			return err
		}
		for _, m := range ms {
			if m.ID != from.ID && m.RevokedAt == nil && m.DisabledAt == nil && h.Registry.Get(m.ID) != nil && len(targets) < peersMaxMachines {
				targets = append(targets, m)
			}
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, m := range targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l := h.Registry.Get(m.ID)
			if l == nil {
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), peersDirectoryWait)
			defer cancel()
			var got hubproto.PeerChats
			if l.CallInto(ctx, hubproto.OpPeersDirectory, hubproto.PeersDirectory{Sender: sender}, &got) != nil {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, c := range got.Chats {
				if c.Conv != "" {
					out.Chats = append(out.Chats, peerChat(c, m))
				}
			}
		}()
	}
	wg.Wait()
	sort.Slice(out.Chats, func(i, j int) bool {
		a, b := out.Chats[i], out.Chats[j]
		if a.MachineName != b.MachineName {
			return a.MachineName < b.MachineName
		}
		return a.Ref < b.Ref
	})
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
