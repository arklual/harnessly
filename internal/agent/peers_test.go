package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestPolicyPeersKey(t *testing.T) {
	// A file that does not mention `peers` does not allow it; no file at all does.
	if p, _ := ParsePolicy("terminal = true\n", "x"); p.Peers {
		t.Fatal("a policy file without `peers` allows messages between machines")
	}
	p, err := ParsePolicy("peers = true\n", "x")
	if err != nil || !p.Peers {
		t.Fatalf("peers = true is not on: %+v %v", p, err)
	}
	if !DefaultPolicy().Peers {
		t.Fatal("peers is off when there is no file")
	}
	off, _ := ParsePolicy("peers = false\n", "x")
	if !Narrowed(DefaultPolicy(), off) || meet(DefaultPolicy(), off).Peers {
		t.Fatal("peers is not part of narrowing")
	}
}

// peerRuntime answers the runtime's peers API, keeping the sender each inbox token names.
type peerRuntime struct {
	mu      sync.Mutex
	senders []map[string]any
	paths   []string
}

func (p *peerRuntime) route(w http.ResponseWriter, r *http.Request) bool {
	if !strings.Contains(r.URL.Path, "/api/peers/") {
		return false
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	c, ok := VerifiedAudience([]byte(hubTestSecret), token, PeerInboxAudience, time.Now())
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(hubproto.PeerRefusal("", hubproto.PeerUnverified, ""))
		return true
	}
	p.mu.Lock()
	p.senders = append(p.senders, c)
	p.paths = append(p.paths, r.URL.Path)
	p.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/directory") {
		_ = json.NewEncoder(w).Encode(hubproto.PeerChats{Chats: []hubproto.PeerChat{{Ref: "host/c-1", Conv: "c-1", Title: "Deploy"}}})
		return true
	}
	var msg hubproto.PeerInbound
	_ = json.NewDecoder(r.Body).Decode(&msg)
	_ = json.NewEncoder(w).Encode(hubproto.PeerResult{ID: msg.ID, State: hubproto.PeerDelivered,
		To: &hubproto.PeerChat{Ref: "host/" + msg.To, Conv: msg.To}})
	return true
}

var peerSenderParams = hubproto.PeerSender{Person: "owner@example.com", PersonName: "Owner", Machine: "m-other", MachineName: "other"}

// The hub hands a message to this machine: the agent signs the sender the hub vouches for and gives it to the runtime.
func TestPeersDeliverToTheRuntime(t *testing.T) {
	rt := &peerRuntime{}
	r := startHubAgent(t, rigOptions{runtimeRoute: rt.route})
	c := r.connect()
	res := decode[hubproto.PeerResult](t, c.mustCall(hubproto.OpPeersDeliver, map[string]any{"sender": peerSenderParams,
		"message": hubproto.PeerInbound{ID: "msg-1", To: "c-to", Text: "hello"}}))
	if res.State != hubproto.PeerDelivered || res.ID != "msg-1" || res.To == nil || res.To.Conv != "c-to" {
		t.Fatalf("result %+v", res)
	}
	chats := decode[hubproto.PeerChats](t, c.mustCall(hubproto.OpPeersDirectory, map[string]any{"sender": peerSenderParams}))
	if len(chats.Chats) != 1 || chats.Chats[0].Conv != "c-1" {
		t.Fatalf("chats %+v", chats)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.senders) != 2 || rt.senders[0]["sub"] != "owner@example.com" || rt.senders[0]["machine"] != "m-other" {
		t.Fatalf("senders %+v", rt.senders)
	}
	base := r.machine.RuntimeBasePath + "/api/peers"
	if rt.paths[0] != base+"/inbox" || rt.paths[1] != base+"/directory" {
		t.Fatalf("paths %v", rt.paths)
	}
	// A sender without a person is not one.
	if _, e := c.call(hubproto.OpPeersDeliver, map[string]any{"sender": hubproto.PeerSender{Machine: "m-other"}}); e == "" {
		t.Fatal("a message with no person was handed over")
	}
}

// The policy's `peers = false` refuses the hub's messages, and the hub reads that as "off".
func TestPeersOffByPolicy(t *testing.T) {
	r := startHubAgent(t, rigOptions{policy: "terminal = true\n"})
	c := r.connect()
	if _, e := c.call(hubproto.OpPeersDeliver, map[string]any{"sender": peerSenderParams}); !strings.HasPrefix(e, "policy_denied: ") {
		t.Fatalf("%q", e)
	}
}

// An observe link is off unless the operator opts in on the machine.
func TestPeersOnAnObserveLinkNeedTheOptIn(t *testing.T) {
	t.Setenv("AGENT_HUB_PEERS", "")
	o := startHubAgent(t, rigOptions{observe: true})
	if _, e := o.connect().call(hubproto.OpPeersDeliver, map[string]any{"sender": peerSenderParams}); !strings.HasPrefix(e, "policy_denied: ") {
		t.Fatalf("%q", e)
	}
	t.Setenv("AGENT_HUB_PEERS", "on")
	rt := &peerRuntime{}
	o2 := startHubAgent(t, rigOptions{observe: true, runtimeRoute: rt.route})
	res := decode[hubproto.PeerResult](t, o2.connect().mustCall(hubproto.OpPeersDeliver, map[string]any{"sender": peerSenderParams,
		"message": hubproto.PeerInbound{ID: "msg-2", To: "c"}}))
	if res.State != hubproto.PeerDelivered {
		t.Fatalf("%+v", res)
	}
}

// The relay on the loopback takes only its own runtime's token, and says so when the hub does not carry messages.
func TestPeersRelayOnTheLoopback(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	r.connect()
	send := func(token string) (int, hubproto.PeerResult) {
		body, _ := json.Marshal(hubproto.PeerOutbound{ID: "msg-3", To: "m-x/c", Text: "hi"})
		req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/peers/send", r.agent.cfg.Port), bytes.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out hubproto.PeerResult
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	claims := map[string]any{"sub": "owner@example.com", "machine": "host"}
	for _, token := range []string{"", "garbage", SignPayload([]byte(hubTestSecret), PeerInboxAudience, claims, PeerTokenTTL),
		SignPayload([]byte("another-secret-another-secret-xx"), PeerRelayAudience, claims, PeerTokenTTL)} {
		if code, out := send(token); code != http.StatusForbidden || out.Reason != hubproto.PeerUnverified {
			t.Fatalf("token %.12q: %d %+v", token, code, out)
		}
	}
	// The fake hub serves no /agent/peers: the runtime hears that the hub does not carry messages.
	code, out := send(SignPayload([]byte(hubTestSecret), PeerRelayAudience, claims, PeerTokenTTL))
	if code != http.StatusOK || out.State != hubproto.PeerRefused || out.Reason != hubproto.PeerNoRelay || out.ID != "msg-3" {
		t.Fatalf("%d %+v", code, out)
	}
}
