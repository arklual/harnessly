package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Messages between agents of different machines (docs/hub/peers.md). The runtime of this machine is the relay's client
// (RUNTIME_PEER_RELAY_URL=http://127.0.0.1:<agent port>/peers): it signs its request with the pair's secret, the agent
// checks it and hands it to the hub signed with the machine's key. The hub delivers to the other machine's agent over
// its link (peers.deliver), which signs the message for its runtime's inbox with that pair's secret.

// The audiences of the two tokens of a message between machines (the runtime's peers package uses the same).
const (
	// PeerRelayAudience: a runtime asks its relay to send a message or list the others' chats.
	PeerRelayAudience = "harnessly-peer-relay"
	// PeerInboxAudience: a message (or a directory request) forwarded to a runtime, naming the sender the relay checked.
	PeerInboxAudience = "harnessly-peer"
	// PeerTokenTTL: a token is made for one request.
	PeerTokenTTL = 60 * time.Second
)

const peersTimeout = 20 * time.Second

// peersAllowed says whether this machine takes part in messages between machines. An own link follows the policy's
// `peers`; an observe link (the machine is run by its own cluster) is off unless the operator opted in on the machine
// with AGENT_HUB_PEERS=on. The kill switch turns it off either way.
func (h *hub) peersAllowed() bool {
	if h.policy.Disabled() {
		return false
	}
	if h.observe {
		return os.Getenv("AGENT_HUB_PEERS") == "on"
	}
	return h.policy.Current().Peers
}

func peerClaim(c map[string]any, k string) string {
	s, _ := c[k].(string)
	return strings.TrimSpace(s)
}

// relaySender is the sender a relay token of this machine's runtime names; false when the token is not one. Only the
// person is passed on, and the hub believes it only as far as it can check it.
func (a *Agent) relaySender(r *http.Request) (hubproto.PeerSender, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	secret := a.sessionSecret()
	if !ok || token == "" || secret == nil {
		return hubproto.PeerSender{}, false
	}
	c, ok := VerifiedAudience(secret, token, PeerRelayAudience, time.Now())
	if !ok {
		return hubproto.PeerSender{}, false
	}
	s := hubproto.PeerSender{Person: peerClaim(c, "sub"), PersonName: peerClaim(c, "name"),
		Machine: peerClaim(c, "machine"), MachineName: peerClaim(c, "machineName")}
	// No person (RUNTIME_OWNER_EMAIL unset) is fine: the hub then names the person who registered the machine.
	return s, true
}

// peersRoute is POST /peers/send and GET /peers/directory on the loopback: the relay of this machine's runtime.
func (a *Agent) peersRoute(w http.ResponseWriter, r *http.Request) {
	sender, ok := a.relaySender(r)
	if !ok {
		reply(w, http.StatusForbidden, hubproto.PeerRefusal("", hubproto.PeerUnverified, ""))
		return
	}
	h := a.hub
	if r.URL.RequestURI() == "/peers/directory" {
		if h == nil || !h.peersAllowed() {
			reply(w, http.StatusOK, hubproto.PeerChats{Chats: []hubproto.PeerChat{}})
			return
		}
		var out hubproto.PeerChats
		if err := h.peersCall(r.Context(), "/agent/peers/directory", map[string]string{"person": sender.Person}, &out); err != nil {
			reply(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if out.Chats == nil {
			out.Chats = []hubproto.PeerChat{}
		}
		reply(w, http.StatusOK, out)
		return
	}
	var msg hubproto.PeerOutbound
	if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&msg) != nil {
		reply(w, http.StatusBadRequest, hubproto.PeerRefusal("", hubproto.PeerBadRef, "body"))
		return
	}
	switch {
	case h == nil:
		reply(w, http.StatusOK, hubproto.PeerRefusal(msg.ID, hubproto.PeerNoRelay, "this machine is not in a hub"))
		return
	case !h.peersAllowed():
		reply(w, http.StatusOK, hubproto.PeerRefusal(msg.ID, hubproto.PeerOff, "messages between machines are off on this machine"))
		return
	}
	var res hubproto.PeerResult
	err := h.peersCall(r.Context(), "/agent/peers/send", hubproto.PeersSend{Person: sender.Person, Message: msg}, &res)
	switch {
	case errors.Is(err, errPeersNotServed):
		res = hubproto.PeerRefusal(msg.ID, hubproto.PeerNoRelay, "the hub does not carry messages between machines")
	case err != nil:
		res = hubproto.PeerRefusal(msg.ID, hubproto.PeerUnreachable, err.Error())
	case res.State != hubproto.PeerDelivered && res.State != hubproto.PeerRefused:
		res = hubproto.PeerRefusal(msg.ID, hubproto.PeerUnreachable, "the hub gave no result")
	}
	h.audit.Record("peers.send", res.State == hubproto.PeerDelivered, map[string]any{"reason": res.Reason})
	reply(w, http.StatusOK, res)
}

var errPeersNotServed = errors.New("this hub does not carry messages between machines")

// peersCall posts body to the hub's path, signed with the machine's key, and reads the answer into out.
func (h *hub) peersCall(ctx context.Context, path string, body, out any) error {
	m := h.machine
	host, err := hubproto.DialHost(m.DialURL)
	if err != nil {
		return err
	}
	u, err := url.Parse(m.DialURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "wss":
		u.Scheme = "https"
	case "ws":
		u.Scheme = "http"
	}
	u.Path, u.RawQuery = path, ""
	auth, err := hubproto.SignPeers(m.SigningKey(), m.MachineID, time.Now(), host)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, peersTimeout)
	defer cancel()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: peersTimeout}).Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("could not reach the hub: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	switch res.StatusCode {
	case http.StatusOK:
		if json.Unmarshal(data, out) != nil {
			return errors.New("the hub gave an answer that is not JSON")
		}
		return nil
	case http.StatusNotFound:
		return errPeersNotServed
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("the hub did not accept this machine")
	}
	return fmt.Errorf("the hub answered HTTP %d", res.StatusCode)
}

// opPeers serves peers.deliver and peers.directory: the hub hands this machine's runtime a message from another machine,
// or asks which of its chats another machine may write to. The runtime decides by its own settings; the agent only
// signs the sender the hub vouches for.
func (h *hub) opPeers(op string, req map[string]json.RawMessage) (any, func(), error) {
	if err := h.allow(op, h.peersAllowed(), "messages between machines"); err != nil {
		return nil, nil, err
	}
	sender, _ := field[hubproto.PeerSender](req, "sender")
	if sender.Person == "" || sender.Machine == "" {
		return nil, nil, invalid("sender must name a person and a machine")
	}
	secret := h.a.sessionSecret()
	if secret == nil || h.a.cfg.RuntimePort == 0 {
		return nil, nil, hubproto.Errf(hubproto.CodeUnsupported, "no runtime to hand messages to on this machine")
	}
	token := SignPayload(secret, PeerInboxAudience, map[string]any{"sub": sender.Person, "name": sender.PersonName,
		"machine": sender.Machine, "machineName": sender.MachineName}, PeerTokenTTL)
	ctx, cancel := context.WithTimeout(h.ctx, peersTimeout)
	defer cancel()
	if op == hubproto.OpPeersDirectory {
		var out hubproto.PeerChats
		status, err := h.runtimePeers(ctx, http.MethodGet, "/directory", token, nil, &out)
		if err != nil {
			return nil, nil, err
		}
		if status != http.StatusOK || out.Chats == nil {
			out.Chats = []hubproto.PeerChat{}
		}
		return out, nil, nil
	}
	msg, _ := field[hubproto.PeerInbound](req, "message")
	var res hubproto.PeerResult
	if _, err := h.runtimePeers(ctx, http.MethodPost, "/inbox", token, msg, &res); err != nil || res.State == "" {
		res = hubproto.PeerRefusal(msg.ID, hubproto.PeerUnreachable, "the runtime did not answer")
	}
	h.audit.Record(op, res.State == hubproto.PeerDelivered, map[string]any{"from": sender.Machine, "reason": res.Reason})
	return res, nil, nil
}

// runtimePeers calls the runtime's peers API with an inbox token; out is read from any answer that is JSON.
func (h *hub) runtimePeers(ctx context.Context, method, path, token string, body, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.a.runtimeURL(h.a.cfg.PeersPath+path), rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := (&http.Client{Timeout: peersTimeout}).Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if json.Unmarshal(data, out) != nil {
		return res.StatusCode, fmt.Errorf("the runtime answered HTTP %d", res.StatusCode)
	}
	return res.StatusCode, nil
}
