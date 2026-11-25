package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestInboxTriageRoutesOnlyToTheEnabledOwnersMachine(t *testing.T) {
	key := "triage-" + strings.Repeat("k", 40)
	s := hubtest.New(t, withWorkspace, fleetOn, hubtest.Env("HUB_LAUNCH_TOKEN", launchKey), hubtest.Env("HUB_INBOX_TRIAGE_TOKEN", key))
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Owner")
	var ran atomic.Int32
	app := http.NewServeMux()
	app.Handle("/", pageRuntime("Acme"))
	app.HandleFunc("POST /api/acme"+hubproto.InboxTriageSuffix, func(w http.ResponseWriter, r *http.Request) {
		ran.Add(1)
		var got hubproto.InboxTriageInput
		if json.NewDecoder(r.Body).Decode(&got) != nil || got.System != "classify" || got.Prompt != "example" || r.Header.Get("Cookie") != "hl_runtime=fixture-triage-grant" || r.Header.Get("Authorization") != "" {
			t.Error("runtime received the wrong request or credential")
		}
		_, _ = io.WriteString(w, `{"used":true,"text":"verdict"}`)
	})
	a := owner.Machine(space, app)
	a.Handle(hubproto.OpInboxTriageGrant, func(hubtest.Call) (any, error) {
		return hubproto.SessionGrant{CookieName: "hl_runtime", Token: "fixture-triage-grant"}, nil
	})
	guest := s.SignIn("guest@example.com")
	guestSpace, _ := guest.NewSpace("Guest")
	g := guest.Machine(guestSpace, pageRuntime("Someone else"))
	body := map[string]string{"person": "owner@example.com", "machineId": a.MachineID, "system": "classify", "prompt": "example"}
	post := func(key string) hubtest.Resp {
		return owner.Do("POST", "/api/platform/inbox/triage", body, "Authorization", "Bearer "+key)
	}
	if got := post(launchKey); got.Code != 403 {
		t.Fatalf("launch credential: %s", got)
	}
	if got := post(key); got.Code != 503 {
		t.Fatalf("switch off: %s", got)
	}
	if got := owner.Do("PUT", "/api/admin/switches", map[string]any{"feature": "inbox-triage", "enabled": true, "person": "owner@example.com", "machineId": a.MachineID}); got.Code != 204 {
		t.Fatalf("switch: %s", got)
	}
	body["machineId"] = g.MachineID
	if got := post(key); got.Code != 404 {
		t.Fatalf("foreign machine: %s", got)
	}
	if ran.Load() != 0 || len(g.Calls(hubproto.OpInboxTriageGrant)) != 0 {
		t.Fatal("refused requests reached a machine")
	}
	body["machineId"] = a.MachineID
	if got := post(key); got.Code != 200 || !strings.Contains(string(got.Body), "verdict") {
		t.Fatalf("triage: %s", got)
	}
	if ran.Load() != 1 || len(a.Calls(hubproto.OpInboxTriageGrant)) != 1 {
		t.Fatal("selected machine did not get one scoped call")
	}
}
