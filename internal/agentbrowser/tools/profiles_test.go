package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/mcpserver"
)

func TestMCPProfilesNeverEnumerateOtherProjects(t *testing.T) {
	store := agentbrowser.OpenProfiles(t.TempDir(), "")
	a := agentbrowser.NewProfileAccess(store, func(conv string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{Project: conv, ProjectName: "Private " + conv, Title: "Chat " + conv}, conv == "a" || conv == "b"
	})
	manager := agentbrowser.New(agentbrowser.Config{DataDir: t.TempDir(), ProfileOf: a.ProfileOf, LookupProfile: store.Lookup})
	defer manager.Close()
	a.Manager = manager
	a.Wait = time.Millisecond
	_, private, err := a.List("a")
	must(t, err)
	service := &Service{Manager: manager, Access: a}
	defer service.Close()
	server := mcpserver.Server{Sets: []mcpserver.Toolset{Tools{S: service, Caller: "b"}}}
	call := func(name string, args *jsvalue.Object) string {
		_, raw := server.Handle(t.Context(), jsvalue.NewObject("jsonrpc", "2.0", "id", float64(1), "method", "tools/call",
			"params", jsvalue.NewObject("name", name, "arguments", args)))
		if strings.Contains(string(raw), private.Name) || strings.Contains(string(raw), private.ID) {
			t.Fatalf("private profile leaked: %s", raw)
		}
		return string(raw)
	}
	list := call("chrome_profiles", jsvalue.NewObject())
	if !strings.Contains(list, "Private b") || strings.Contains(list, "usedBy") {
		t.Fatal(list)
	}
	denied := call("chrome_use_profile", jsvalue.NewObject("name", private.Name))
	missing := call("chrome_use_profile", jsvalue.NewObject("name", "missing"))
	if denied != missing || !strings.Contains(denied, "unavailable to this chat") {
		t.Fatal(denied, missing)
	}
	pending := call("chrome_request_profile", jsvalue.NewObject("why", "Use another account"))
	if !strings.Contains(pending, "waiting") || len(a.Asks.Pending()) != 1 {
		t.Fatal(pending)
	}
	pendingAgain := call("chrome_request_profile", jsvalue.NewObject("why", "Use another account"))
	if !strings.Contains(pendingAgain, "waiting") || len(a.Asks.Pending()) != 1 {
		t.Fatal(pendingAgain)
	}
	must(t, a.Answer(context.Background(), a.Asks.Pending()[0].ID, "", "decline", "", ""))
	if got := call("chrome_request_profile", jsvalue.NewObject("why", "Use another account")); !strings.Contains(got, "declined") {
		t.Fatal(got)
	}
	// Agent arguments cannot choose the caller/project or submit an approval.
	for _, args := range []*jsvalue.Object{
		jsvalue.NewObject("project", "a"), jsvalue.NewObject("conv", "a"), jsvalue.NewObject("scope", "project"),
	} {
		if got := call("chrome_profiles", args); !strings.Contains(got, "isError") {
			t.Fatal(got)
		}
	}
}
