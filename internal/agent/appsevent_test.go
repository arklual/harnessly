package agent

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func recvFrame(t *testing.T, l *link) string {
	t.Helper()
	select {
	case f := <-l.control:
		return string(f)
	case <-time.After(time.Second):
		t.Fatal("no frame")
		return ""
	}
}

func TestAppsEventRoute(t *testing.T) {
	a := testAgent(t)
	hash := strings.Repeat("ab", 32)
	exp := time.Now().Add(time.Hour).UnixMilli()
	created := `{"event":"apps.guest.created","id":"g1","hash":"` + hash + `","expiresAt":` + itoa(exp) + `}`
	if rec := post(a, "/apps/event", "", created); rec.Code != 403 {
		t.Errorf("without a token: %d", rec.Code)
	}
	for name, body := range map[string]string{
		"unknown":   `{"event":"apps.nope"}`,
		"short":     `{"event":"apps.guest.created","id":"g1","hash":"ab","expiresAt":` + itoa(exp) + `}`,
		"no expiry": `{"event":"apps.guest.created","id":"g1","hash":"` + hash + `"}`,
		"no id":     `{"event":"apps.guest.revoked"}`,
	} {
		if rec := post(a, "/apps/event", "local-secret", body); rec.Code != 400 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	l := &link{control: make(chan []byte, 8), data: make(chan []byte, 8), done: make(chan struct{})}
	a.clients[l] = struct{}{}
	if rec := post(a, "/apps/event", "local-secret", `{"event":"apps.team.changed"}`); rec.Code != 202 {
		t.Fatalf("team: %d", rec.Code)
	}
	if f := recvFrame(t, l); f != `{"event":"apps.team.changed"}` {
		t.Errorf("team frame: %s", f)
	}
	if rec := post(a, "/apps/event", "local-secret", `{"event":"apps.public.changed"}`); rec.Code != 202 {
		t.Fatalf("public: %d", rec.Code)
	}
	if f := recvFrame(t, l); f != `{"event":"apps.public.changed"}` {
		t.Errorf("public frame: %s", f)
	}
	if rec := post(a, "/apps/event", "local-secret", created); rec.Code != 202 {
		t.Fatalf("created: %d", rec.Code)
	}
	if f := recvFrame(t, l); f != created {
		t.Errorf("created frame: %s", f)
	}
	// A link that comes up later is told of every live link, and of a revoked one no more.
	late := &link{control: make(chan []byte, 8), data: make(chan []byte, 8), done: make(chan struct{})}
	a.replayAppsGuests(late)
	if f := recvFrame(t, late); f != created {
		t.Errorf("replay: %s", f)
	}
	if rec := post(a, "/apps/event", "local-secret", `{"event":"apps.guest.revoked","id":"g1"}`); rec.Code != 202 {
		t.Fatalf("revoked: %d", rec.Code)
	}
	if f := recvFrame(t, l); f != `{"event":"apps.guest.revoked","id":"g1"}` {
		t.Errorf("revoked frame: %s", f)
	}
	again := &link{control: make(chan []byte, 8), data: make(chan []byte, 8), done: make(chan struct{})}
	a.replayAppsGuests(again)
	if len(again.control) != 0 {
		t.Errorf("a revoked link was replayed")
	}
	// Ended links are not replayed, and the memory survives the agent.
	expired := `{"event":"apps.guest.created","id":"g2","hash":"` + hash + `","expiresAt":1}`
	if rec := post(a, "/apps/event", "local-secret", expired); rec.Code != 202 {
		t.Fatalf("expired: %d", rec.Code)
	}
	post(a, "/apps/event", "local-secret", created)
	reborn := newAppsGuests(a.cfg.DataDir)
	if live := reborn.live(); len(live) != 1 || live[0].ID != "g1" {
		t.Errorf("after a restart: %+v", live)
	}
	// A sync replaces the set: g1 is told revoked, g3 created.
	for len(l.control) > 0 {
		<-l.control
	}
	sync := `{"event":"apps.guest.sync","links":[{"id":"g3","hash":"` + hash + `","expiresAt":` + itoa(exp) + `}]}`
	if rec := post(a, "/apps/event", "local-secret", sync); rec.Code != 202 {
		t.Fatalf("sync: %d", rec.Code)
	}
	if f := recvFrame(t, l); f != `{"event":"apps.guest.revoked","id":"g1"}` {
		t.Errorf("sync revokes: %s", f)
	}
	if f := recvFrame(t, l); !strings.Contains(f, `"id":"g3"`) || strings.Contains(f, "token") {
		t.Errorf("sync creates: %s", f)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestAgentRelaysAppsOpsToTheRuntime(t *testing.T) {
	a := testAgent(t)
	seen := relayTarget(t, a)
	for _, op := range []string{"apps.team", "apps.team.snapshot", "apps.public", "apps.public.snapshot"} {
		var req map[string]json.RawMessage
		if err := json.Unmarshal([]byte(`{"op":"`+op+`","at":5,"entries":[{"id":"acme.demo","archive":"AAEC"}]}`), &req); err != nil {
			t.Fatal(err)
		}
		if res, _, err := a.handle(nil, req); err != nil || string(res.(json.RawMessage)) != `{"ok":true}` {
			t.Fatalf("%s: %v %v", op, res, err)
		}
	}
	want := []string{"/api/plat/apps/team/sync ", "/api/plat/apps/team/snapshot ", "/api/plat/apps/public/sync ", "/api/plat/apps/public/snapshot "}
	if len(*seen) != len(want) {
		t.Fatalf("relayed: %v", *seen)
	}
	for i, w := range want {
		if !strings.HasPrefix((*seen)[i], w) {
			t.Errorf("relay %d: %s", i, (*seen)[i])
		}
	}
	if !strings.Contains((*seen)[2], `"archive":"AAEC"`) || strings.Contains((*seen)[2], `"op"`) {
		t.Errorf("the list goes as it came, without the op: %s", (*seen)[2])
	}
	// A hub link does not get the closed cluster's ops.
	if !closedClusterOps["apps.public"] || !closedClusterOps["apps.public.snapshot"] {
		t.Error("a hub link must refuse the public catalog's ops")
	}
}
