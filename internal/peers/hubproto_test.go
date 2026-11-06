package peers

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// roundTrip marshals in and reads it back as T.
func roundTrip[T any](t *testing.T, in any) T {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The hub's messages between machines carry this package's JSON: what the agent passes on, the runtime reads as is.
func TestHubprotoSpeaksThePeersJSON(t *testing.T) {
	chat := Chat{Ref: "m-1/c", Conv: "c", Title: "Deploy", Machine: "m-1", MachineName: "host", Owner: "owner@example.com", State: ChatIdle}
	if got := roundTrip[Chat](t, roundTrip[hubproto.PeerChat](t, chat)); got != chat {
		t.Fatalf("chat %+v", got)
	}
	out := Outbound{ID: "1", To: "m-1/c", FromConv: "f", FromTitle: "Release", Text: "hi", Via: "agent"}
	if got := roundTrip[Outbound](t, roundTrip[hubproto.PeerOutbound](t, out)); got != out {
		t.Fatalf("outbound %+v", got)
	}
	in := Inbound{ID: "1", To: "c", FromConv: "f", FromTitle: "Release", Text: "hi", Via: "agent"}
	if got := roundTrip[Inbound](t, roundTrip[hubproto.PeerInbound](t, in)); got != in {
		t.Fatalf("inbound %+v", got)
	}
	res := Result{ID: "1", State: StateRefused, Reason: RefuseUnknownChat, Detail: "x", To: &chat}
	if got := roundTrip[Result](t, roundTrip[hubproto.PeerResult](t, res)); !reflect.DeepEqual(got, res) {
		t.Fatalf("result %+v", got)
	}
	s := Sender{Person: "owner@example.com", PersonName: "Owner", Machine: "m-1", MachineName: "host"}
	if got := roundTrip[Sender](t, roundTrip[hubproto.PeerSender](t, s)); got != s {
		t.Fatalf("sender %+v", got)
	}
	// The states and reasons the hub and the agent give themselves are the runtime's.
	for hub, own := range map[string]string{hubproto.PeerDelivered: string(StateDelivered), hubproto.PeerRefused: string(StateRefused),
		hubproto.PeerOff: string(RefuseOff), hubproto.PeerRateLimited: string(RefuseRateLimited), hubproto.PeerBadRef: string(RefuseBadRef),
		hubproto.PeerNoRelay: string(RefuseNoRelay), hubproto.PeerUnreachable: string(RefuseUnreachable),
		hubproto.PeerUnverified: string(RefuseUnverified), hubproto.PeerEmpty: string(RefuseEmpty), hubproto.PeerTooLong: string(RefuseTooLong)} {
		if hub != own {
			t.Errorf("hub %q, runtime %q", hub, own)
		}
	}
}

// The token the agent signs for a message from another machine is one this runtime's inbox takes, and the relay token
// this runtime signs is one the agent takes.
func TestAgentTokensAreThePeersTokens(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	token := agent.SignPayload(secret, agent.PeerInboxAudience, map[string]any{"sub": "owner@example.com", "name": "Owner",
		"machine": "m-1", "machineName": "host"}, agent.PeerTokenTTL)
	s, err := VerifyInbox(secret, token, time.Now())
	if err != nil || s.Person != "owner@example.com" || s.Machine != "m-1" || s.MachineName != "host" {
		t.Fatalf("%+v %v", s, err)
	}
	relay := SignRelay(secret, Sender{Person: "owner@example.com", Machine: "host"})
	if c, ok := agent.VerifiedAudience(secret, relay, agent.PeerRelayAudience, time.Now()); !ok || c["sub"] != "owner@example.com" {
		t.Fatalf("the agent does not take the relay token: %v", c)
	}
	if _, ok := agent.VerifiedAudience(secret, relay, agent.PeerInboxAudience, time.Now()); ok {
		t.Fatal("a relay token passes for an inbox token")
	}
}
