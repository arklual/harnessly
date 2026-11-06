package tools

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestLoginArguments(t *testing.T) {
	a := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(t.TempDir(), ""), func(string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{}, true
	})
	set := Tools{S: &Service{Access: a}, Caller: "chat"}
	for _, tt := range []struct {
		args *jsvalue.Object
		want string
	}{
		{jsvalue.NewObject("url", "file:///tmp/test", "why", "Sign in"), "URL refused"},
		{jsvalue.NewObject("url", "https://example.org", "why", ""), "A reason is required"},
		{jsvalue.NewObject("url", "https://example.org", "why", "Sign in", "conv", "other"), "Unexpected login argument"},
		{jsvalue.NewObject("url", float64(1), "why", "Sign in"), "Login arguments must be strings"},
	} {
		out := set.loginCall(t.Context(), tt.args)
		if !strings.Contains(string(jsvalue.Stringify(out, "")), tt.want) {
			t.Fatal(out)
		}
	}
}
