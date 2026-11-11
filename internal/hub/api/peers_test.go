package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func peersPost(t *testing.T, s *hubtest.Stack, m *hubtest.Agent, path string, body any) hubtest.Resp {
	t.Helper()
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	auth, _ := hubproto.SignPeers(m.Priv, m.MachineID, s.Clock.Now(), host)
	return s.Browser().Do("POST", path, body, "Authorization", auth)
}

func peerSend(t *testing.T, s *hubtest.Stack, m *hubtest.Agent, person, to, text string) hubproto.PeerResult {
	t.Helper()
	r := peersPost(t, s, m, "/agent/peers/send", hubproto.PeersSend{Person: person,
		Message: hubproto.PeerOutbound{ID: "msg-1", To: to, FromConv: "c-from", FromTitle: "Release", Text: text, Via: "agent"}})
	if r.Code != http.StatusOK {
		t.Fatalf("send: %s", r)
	}
	var res hubproto.PeerResult
	r.JSON(&res)
	return res
}

// deliverer answers peers.deliver as a runtime that takes every message, naming its chat with its own machine id.
func deliverer(c hubtest.Call) (any, error) {
	var msg hubproto.PeerInbound
	_ = json.Unmarshal(c.Params["message"], &msg)
	return hubproto.PeerResult{ID: msg.ID, State: hubproto.PeerDelivered,
		To: &hubproto.PeerChat{Ref: "host-b/" + msg.To, Conv: msg.To, Machine: "host-b", MachineName: "host-b", Title: "Deploy"}}, nil
}

// A machine sends to a chat of another machine of its space: the hub names the sender itself (the machine the signature
// proves, the person who registered it) and gives the receiver's chat back in its own terms.
func TestPeersMessageBetweenMachinesOfASpace(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_PEERS", "on"))
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Acme")
	a := owner.Machine(id, nil)
	b := owner.Machine(id, nil)
	b.Handle(hubproto.OpPeersDeliver, deliverer)

	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	dial, _ := hubproto.SignDial(a.Priv, a.MachineID, s.Clock.Now(), host)
	if r := s.Browser().Do("POST", "/agent/peers/send", hubproto.PeersSend{}, "Authorization", dial); r.Code != http.StatusUnauthorized {
		t.Fatalf("a dial signature sent a message: %s", r)
	}

	// The machine claims a person who never opened it: the hub names the one who registered it instead.
	res := peerSend(t, s, a, "stranger@example.com", hubproto.PeerRefPrefix+b.MachineID+"/c-to", "hello")
	if res.State != hubproto.PeerDelivered || res.ID != "msg-1" {
		t.Fatalf("result %+v", res)
	}
	if res.To == nil || res.To.Machine != hubproto.PeerRefPrefix+b.MachineID || res.To.Ref != hubproto.PeerRefPrefix+b.MachineID+"/c-to" {
		t.Fatalf("the receiver's chat is not in the hub's terms: %+v", res.To)
	}
	calls := b.Calls(hubproto.OpPeersDeliver)
	if len(calls) != 1 {
		t.Fatalf("deliveries: %d", len(calls))
	}
	var got hubproto.PeersDeliver
	raw, _ := json.Marshal(calls[0].Params)
	_ = json.Unmarshal(raw, &got)
	if got.Sender.Person != "owner@example.com" || got.Sender.Machine != hubproto.PeerRefPrefix+a.MachineID {
		t.Fatalf("sender %+v", got.Sender)
	}
	if got.Message.To != "c-to" || got.Message.FromConv != "c-from" || got.Message.Text != "hello" {
		t.Fatalf("message %+v", got.Message)
	}

	// Writing to itself, to an unknown machine and to a ref that is not the hub's are refused without telling why.
	for _, to := range []string{hubproto.PeerRefPrefix + a.MachineID + "/c", hubproto.PeerRefPrefix + "nosuchmachine/c"} {
		if res := peerSend(t, s, a, "", to, "x"); res.State != hubproto.PeerRefused || res.Reason != hubproto.PeerUnreachable {
			t.Fatalf("%s: %+v", to, res)
		}
	}
	if res := peerSend(t, s, a, "", "host-b/c", "x"); res.Reason != hubproto.PeerBadRef {
		t.Fatalf("a fleet ref: %+v", res)
	}
}

// A machine reaches the machines of its space and of the spaces of its organization, nothing else; a machine whose
// policy is off says so.
func TestPeersScopeAndPolicy(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_PEERS", "on"))
	owner := s.SignIn("owner@example.com")
	acme, _ := owner.NewSpace("Acme")
	beta, _ := owner.NewSpace("Beta")
	gamma, _ := owner.NewSpace("Gamma")
	a := owner.Machine(acme, nil)
	b := owner.Machine(beta, nil)
	c := owner.Machine(gamma, nil)
	b.Handle(hubproto.OpPeersDeliver, deliverer)
	c.Handle(hubproto.OpPeersDeliver, deliverer)

	if res := peerSend(t, s, a, "", hubproto.PeerRefPrefix+b.MachineID+"/c", "x"); res.Reason != hubproto.PeerUnreachable {
		t.Fatalf("a machine of another space was reached: %+v", res)
	}
	ctx := context.Background()
	org := &store.Org{Slug: "acme-org", Name: "Acme"}
	if err := s.DB.S().CreateOrg(ctx, org); err != nil {
		t.Fatal(err)
	}
	for _, sp := range []string{acme, beta} {
		if err := s.DB.S().SetSpaceOrg(ctx, sp, org.ID); err != nil {
			t.Fatal(err)
		}
	}
	if res := peerSend(t, s, a, "", hubproto.PeerRefPrefix+b.MachineID+"/c", "x"); res.State != hubproto.PeerDelivered {
		t.Fatalf("a machine of the same organization: %+v", res)
	}
	if res := peerSend(t, s, a, "", hubproto.PeerRefPrefix+c.MachineID+"/c", "x"); res.Reason != hubproto.PeerUnreachable {
		t.Fatalf("a machine out of the organization was reached: %+v", res)
	}
	if len(c.Calls(hubproto.OpPeersDeliver)) != 0 {
		t.Fatal("the hub called a machine out of reach")
	}

	b.Handle(hubproto.OpPeersDeliver, func(hubtest.Call) (any, error) {
		return nil, hubtest.Fail(hubproto.CodePolicyDenied, "off")
	})
	if res := peerSend(t, s, a, "", hubproto.PeerRefPrefix+b.MachineID+"/c", "x"); res.Reason != hubproto.PeerOff {
		t.Fatalf("policy off: %+v", res)
	}
	b.Close()
	s.Eventually("b offline", func() bool {
		return peerSend(t, s, a, "", hubproto.PeerRefPrefix+b.MachineID+"/c", "x").Reason == hubproto.PeerUnreachable
	})
}

// The directory lists what each reachable machine's runtime offers this sender, in the hub's refs.
func TestPeersDirectory(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_PEERS", "on"))
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Acme")
	other, _ := owner.NewSpace("Beta")
	a := owner.Machine(id, nil)
	b := owner.Machine(id, nil)
	c := owner.Machine(other, nil)
	list := func(hubtest.Call) (any, error) {
		return hubproto.PeerChats{Chats: []hubproto.PeerChat{{Ref: "host/c-1", Conv: "c-1", Title: "Deploy", Machine: "host"}}}, nil
	}
	a.Handle(hubproto.OpPeersDirectory, list)
	b.Handle(hubproto.OpPeersDirectory, list)
	c.Handle(hubproto.OpPeersDirectory, list)

	r := peersPost(t, s, a, "/agent/peers/directory", map[string]string{"person": "owner@example.com"})
	if r.Code != http.StatusOK {
		t.Fatalf("directory: %s", r)
	}
	var got hubproto.PeerChats
	r.JSON(&got)
	if len(got.Chats) != 1 || got.Chats[0].Ref != hubproto.PeerRefPrefix+b.MachineID+"/c-1" || got.Chats[0].Machine != hubproto.PeerRefPrefix+b.MachineID {
		t.Fatalf("chats %+v", got.Chats)
	}
	if len(a.Calls(hubproto.OpPeersDirectory)) != 0 || len(c.Calls(hubproto.OpPeersDirectory)) != 0 {
		t.Fatal("the directory asked the sender itself or a machine out of reach")
	}
}

// Without HUB_PEERS the paths are not there: the agent reads the 404 as "this hub does not carry messages".
func TestPeersOffByDefault(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Acme")
	a := owner.Machine(id, nil)
	if r := peersPost(t, s, a, "/agent/peers/send", hubproto.PeersSend{}); r.Code != http.StatusNotFound {
		t.Fatalf("send with peers off: %s", r)
	}
}

// A machine sends at most so many messages a minute.
func TestPeersRateLimit(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_PEERS", "on"))
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Acme")
	a := owner.Machine(id, nil)
	b := owner.Machine(id, nil)
	b.Handle(hubproto.OpPeersDeliver, deliverer)
	limited := false
	for i := 0; i < 40 && !limited; i++ {
		limited = peerSend(t, s, a, "", hubproto.PeerRefPrefix+b.MachineID+"/c", "x").Reason == hubproto.PeerRateLimited
	}
	if !limited {
		t.Fatal("40 messages in a minute were never limited")
	}
}

// A message reaches a machine only when the person it comes from may open that machine: a machine with an access list
// that does not name the sender is out of reach, and is not asked for its chats either.
func TestPeersKeepTheMachineAccessList(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_PEERS", "on"), hubtest.Env("HUB_ORGS", "on"), hubtest.Env("HUB_ACCESS_ENFORCE", "on"))
	owner, guest := s.SignIn("owner@example.com"), s.SignIn("guest@example.com")
	id, _ := owner.NewSpace("Acme")
	owner.Join(id, guest)
	a := owner.Machine(id, nil)
	b := owner.Machine(id, nil)
	ctx := context.Background()
	// The sending machine is the guest's: its messages come from the guest.
	if _, err := s.DB.Exec(ctx, `UPDATE machines SET created_by = ? WHERE id = ?`, guest.ID, a.MachineID); err != nil {
		t.Fatal(err)
	}
	b.Handle(hubproto.OpPeersDeliver, deliverer)
	b.Handle(hubproto.OpPeersDirectory, func(hubtest.Call) (any, error) {
		return hubproto.PeerChats{Chats: []hubproto.PeerChat{{Ref: "host/c-1", Conv: "c-1", Title: "Deploy", Machine: "host"}}}, nil
	})
	to := hubproto.PeerRefPrefix + b.MachineID + "/c"
	chats := func() int {
		t.Helper()
		r := peersPost(t, s, a, "/agent/peers/directory", map[string]string{})
		if r.Code != http.StatusOK {
			t.Fatalf("directory: %s", r)
		}
		var got hubproto.PeerChats
		r.JSON(&got)
		return len(got.Chats)
	}

	// No list: the machine is open to its space.
	if res := peerSend(t, s, a, "", to, "x"); res.State != hubproto.PeerDelivered {
		t.Fatalf("a machine open to the space: %+v", res)
	}
	if n := chats(); n != 1 {
		t.Fatalf("chats of an open machine: %d", n)
	}
	delivered := len(b.Calls(hubproto.OpPeersDeliver))
	asked := len(b.Calls(hubproto.OpPeersDirectory))

	// A list without the sender closes it.
	if err := s.DB.S().SetMachineAccess(ctx, b.MachineID, []store.MachineAccessEntry{{Kind: store.AccessUser, RefID: owner.ID}}, s.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if res := peerSend(t, s, a, "", to, "x"); res.State != hubproto.PeerRefused || res.Reason != hubproto.PeerUnreachable {
		t.Fatalf("a machine closed to the sender was reached: %+v", res)
	}
	if n := chats(); n != 0 {
		t.Fatalf("chats of a closed machine: %d", n)
	}
	if len(b.Calls(hubproto.OpPeersDeliver)) != delivered || len(b.Calls(hubproto.OpPeersDirectory)) != asked {
		t.Fatal("the hub called a machine the sender may not open")
	}

	// The sender on the list reaches it again.
	if err := s.DB.S().SetMachineAccess(ctx, b.MachineID, []store.MachineAccessEntry{{Kind: store.AccessUser, RefID: owner.ID}, {Kind: store.AccessUser, RefID: guest.ID}}, s.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if res := peerSend(t, s, a, "", to, "x"); res.State != hubproto.PeerDelivered {
		t.Fatalf("a sender on the list: %+v", res)
	}
}
