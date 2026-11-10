package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// A server whose tools follow its chat says so at initialize (HRN-975); one that does not says nothing of it.
func TestInitializeListChanged(t *testing.T) {
	init := func(s *Server) string {
		msg, _ := jsvalue.Parse([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`))
		_, body := s.Handle(context.Background(), msg)
		return string(body)
	}
	if got := init(&Server{Name: "harnessly-app", ListChanged: true}); !strings.Contains(got, `"tools":{"listChanged":true}`) {
		t.Fatalf("%s", got)
	}
	if got := init(&Server{Name: "x"}); !strings.Contains(got, `"tools":{}`) {
		t.Fatalf("%s", got)
	}
	if got := string(ToolsListChanged()); got != `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}` {
		t.Fatalf("%s", got)
	}
}
