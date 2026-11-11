package hubtest_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const replicaSecret = "secretsecretsecretsecret"

// replicas starts two hubs on one database, the way two pods of the deployment share one Postgres.
func replicas(t *testing.T) (a, b *hubtest.Stack) {
	t.Helper()
	a = hubtest.New(t, hubtest.Env("HUB_REPLICAS", "on"), hubtest.Env("HUB_REPLICA_SECRET", replicaSecret))
	b = hubtest.New(t, hubtest.WithDB(a.DB), hubtest.Env("HUB_REPLICAS", "on"), hubtest.Env("HUB_REPLICA_SECRET", replicaSecret))
	return a, b
}

func ownerOf(t *testing.T, s *hubtest.Stack, machine string) (store.LinkRow, bool) {
	t.Helper()
	row, ok, err := s.DB.S().LinkOf(context.Background(), machine, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return row, ok
}

func TestAnotherReplicaReachesTheMachineThroughTheOwner(t *testing.T) {
	a, b := replicas(t)
	owner := a.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Team")
	ag := owner.Machine(spaceID, nil)
	ag.Handle("ping", func(hubtest.Call) (any, error) { return map[string]string{"from": "agent"}, nil })

	if _, ok := ownerOf(t, a, ag.MachineID); !ok {
		t.Fatal("the dialed replica did not enter the link in the directory")
	}
	l := b.Hub.Registry.Get(ag.MachineID)
	if l == nil || !l.Remote() {
		t.Fatalf("replica b should see a stand-in for the link of a, got %v", l)
	}
	raw, err := l.Call(context.Background(), "ping", nil)
	if err != nil || !strings.Contains(string(raw), "agent") {
		t.Fatalf("call through the owner: %s %v", raw, err)
	}
	if len(ag.Calls("ping")) != 1 {
		t.Errorf("the agent saw %d pings", len(ag.Calls("ping")))
	}
	if !contains(b.Hub.Registry.Online(), ag.MachineID) {
		t.Errorf("replica b does not list the machine online: %v", b.Hub.Registry.Online())
	}

	// A tunnel to the runtime is carried over the hop.
	conn, err := l.OpenConn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("GET /x HTTP/1.0\r\nHost: example.com\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(conn)
	if !strings.Contains(string(body), "runtime GET /x") {
		t.Errorf("runtime answer through the hop = %q", body)
	}
}

// hop sends a call to a replica's hop the way another replica does, signed with secret ("" for no signature at all).
func hop(t *testing.T, base, secret string, at time.Time) int {
	t.Helper()
	const path, body = "/internal/replica/v1/call", `{"machine":"x","op":"ping"}`
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	if secret != "" {
		stamp := strconv.FormatInt(at.Unix(), 10)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte("POST\n" + path + "\n" + stamp + "\n" + body))
		req.Header.Set("X-Replica-Time", stamp)
		req.Header.Set("X-Replica-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

func TestTheHopWantsASignature(t *testing.T) {
	a, _ := replicas(t)
	now := time.Now()
	if got := hop(t, a.ReplicaURL, replicaSecret, now); got != http.StatusOK {
		t.Fatalf("a signed call: status %d", got)
	}
	for name, got := range map[string]int{
		"no signature": hop(t, a.ReplicaURL, "", now),
		"wrong secret": hop(t, a.ReplicaURL, "wrong-secret-0123456789", now),
		"an old one":   hop(t, a.ReplicaURL, replicaSecret, now.Add(-10*time.Minute)),
	} {
		if got != http.StatusForbidden {
			t.Errorf("%s: status %d", name, got)
		}
	}
}

func TestThePublicListenerDoesNotServeTheHop(t *testing.T) {
	a, _ := replicas(t)
	if got := hop(t, a.URL, replicaSecret, time.Now()); got != http.StatusNotFound {
		t.Errorf("a signed call on the public listener: status %d, want 404", got)
	}
	res, err := http.Get(a.URL + "/internal/replica/v1/tunnel?machine=x")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("the tunnel on the public listener: status %d, want 404", res.StatusCode)
	}
}

func TestKilledReplicaMachinesReconnectToTheLiveOne(t *testing.T) {
	a, b := replicas(t)
	owner := a.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Team")
	ag := owner.Machine(spaceID, nil)
	ag.Handle("ping", func(hubtest.Call) (any, error) { return map[string]string{"ok": "yes"}, nil })
	first, ok := ownerOf(t, a, ag.MachineID)
	if !ok {
		t.Fatal("no owner")
	}

	// The replica dies: its server stops and its links end.
	a.Server.CloseClientConnections()
	a.Hub.Shutdown()
	a.Server.Close()
	ag.WaitClosed()

	// The agent dials the replica that is alive.
	ag.S = b
	ag.Connect()
	b.Eventually("the live replica owns the machine", func() bool {
		row, ok := ownerOf(t, b, ag.MachineID)
		return ok && row.Owner == b.Hub.Registry.Cluster().ID
	})
	second, _ := ownerOf(t, b, ag.MachineID)
	if second.Fence <= first.Fence {
		t.Errorf("fence went %d -> %d, it must grow with every new owner", first.Fence, second.Fence)
	}
	if l := b.Hub.Registry.Get(ag.MachineID); l == nil || l.Remote() {
		t.Fatalf("the machine should be a local link of b, got %v", l)
	}
	if _, err := b.Hub.Registry.Get(ag.MachineID).Call(context.Background(), "ping", nil); err != nil {
		t.Errorf("work after the failover: %v", err)
	}

	// The dead owner, still holding its old token, writes nothing.
	st := b.DB.S()
	ctx := context.Background()
	old := a.Hub.Registry.Cluster().ID
	if err := st.TouchMachineFenced(ctx, ag.MachineID, time.Now(), old, first.Fence); !errors.Is(err, store.ErrFenced) {
		t.Errorf("stale touch: %v", err)
	}
	if err := st.UpdateMachineInfoFenced(ctx, ag.MachineID, `{"stale":true}`, old, first.Fence); !errors.Is(err, store.ErrFenced) {
		t.Errorf("stale info: %v", err)
	}
	if held, err := st.RenewLink(ctx, ag.MachineID, old, first.Fence, time.Now(), time.Minute); err != nil || held {
		t.Errorf("stale renew = %v %v", held, err)
	}
	m, err := st.MachineByID(ctx, ag.MachineID)
	if err != nil || strings.Contains(m.Info, "stale") {
		t.Errorf("machine info = %q %v", m.Info, err)
	}
	// The live owner's own token still writes.
	if err := st.TouchMachineFenced(ctx, ag.MachineID, time.Now(), second.Owner, second.Fence); err != nil {
		t.Errorf("owner touch: %v", err)
	}
}

func TestTakeoverEndsTheOldOwnersLink(t *testing.T) {
	a, b := replicas(t)
	owner := a.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Team")
	ag := owner.Machine(spaceID, nil)
	first, _ := ownerOf(t, a, ag.MachineID)

	// The agent dials b while a still holds its link (a partition, a paused pod).
	ag.S = b
	ag.Connect()
	b.Eventually("b took the machine over", func() bool {
		row, ok := ownerOf(t, b, ag.MachineID)
		return ok && row.Fence > first.Fence
	})
	// a learns it on its next renewal at the latest, and its writes are refused until then.
	if err := a.DB.S().TouchMachineFenced(context.Background(), ag.MachineID, time.Now(), first.Owner, first.Fence); !errors.Is(err, store.ErrFenced) {
		t.Errorf("write by the old owner: %v", err)
	}
}

func TestWithoutTheFlagNothingChanges(t *testing.T) {
	s := hubtest.New(t)
	if s.Hub.Registry.Cluster() != nil {
		t.Fatal("a hub without HUB_REPLICAS must not join a cluster")
	}
	owner := s.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Team")
	ag := owner.Machine(spaceID, nil)
	if _, ok := ownerOf(t, s, ag.MachineID); ok {
		t.Error("the directory must stay empty without the flag")
	}
	if s.Hub.Internal != nil {
		t.Error("the hop must not be served without the flag")
	}
	if r := s.Browser().Do("POST", "/internal/replica/v1/call", nil); r.Code != http.StatusNotFound {
		t.Errorf("the hop on the public listener without the flag: status %d, want 404", r.Code)
	}
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}
