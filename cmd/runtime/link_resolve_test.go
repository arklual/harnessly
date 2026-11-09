package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/connections"
	"github.com/ly-solutions/harnessly/internal/docprovider"
	"github.com/ly-solutions/harnessly/internal/integrations"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// docsFake is a documents provider that resolves a few addresses of docs.example.com and nothing else.
type docsFake struct {
	docprovider.Provider
	asked int
}

func (d *docsFake) Resolve(_ context.Context, address string) (docprovider.Resolved, bool, error) {
	d.asked++
	no := false
	switch address {
	case "https://docs.example.com/app/board/Ab-Cd_9":
		return docprovider.Resolved{Kind: "board", ID: "Ab-Cd_9"}, true, nil
	case "https://docs.example.com/wiki/Q3-Plan":
		return docprovider.Resolved{Kind: "document", ID: "Q3-Plan"}, true, nil
	case "https://docs.example.com/wiki/Legacy":
		return docprovider.Resolved{Kind: "document", ID: "Legacy", Embed: &no}, true, nil
	case "https://docs.example.com/wiki/Secret":
		return docprovider.Resolved{}, false, &integrations.UpstreamError{Status: http.StatusForbidden, Text: "denied"}
	case "https://docs.example.com/wiki/Down":
		return docprovider.Resolved{}, false, errors.New("unreachable")
	}
	return docprovider.Resolved{}, false, nil
}

func connectFake(t *testing.T, cs *connections.Service, host string) connections.Conn {
	t.Helper()
	det := connections.Detection{Kind: fakeKind, BaseURL: "https://" + host, Deployment: connections.Cloud,
		Methods:      []connections.Method{{ID: "pat", Fields: []connections.Field{{ID: connections.FieldToken, Secret: true}}}},
		Capabilities: []connections.Capability{connections.Issues, connections.Pages}}
	c, err := cs.Connect(context.Background(), connections.ConnectInput{Detection: det, Method: "pat",
		Cred: connections.Credentials{connections.FieldToken: connFakeToken}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLinkResolverOpensEachConnectionsOwnIssueAndPage(t *testing.T) {
	_, d := connRig(t, nil)
	cs := connectionsFor(d)
	one := connectFake(t, cs, "one.example.com")
	two := connectFake(t, cs, "two.example.com")
	targets := panel.Targets{}
	targets.Resolve = linkResolver(cs, nil, targets)
	ctx := context.Background()

	// The same key on two trackers: each address opens with its own connection.
	a := targets.ClassifyLink(ctx, "https://one.example.com/browse/DEMO-123")
	b := targets.ClassifyLink(ctx, "https://two.example.com/browse/DEMO-123")
	if a.Tab != "task" || a.Target != panel.ConnTarget(one.ID, "issue", "DEMO-123") ||
		b.Tab != "task" || b.Target != panel.ConnTarget(two.ID, "issue", "DEMO-123") || a.Target == b.Target {
		t.Fatalf("same key, two hosts: %+v %+v", a, b)
	}
	// The key is the provider's, in its case and with every hyphen, never split.
	if l := targets.ClassifyLink(ctx, "https://one.example.com/browse/Demo-Mixed-Case-9"); l.Target != panel.ConnTarget(one.ID, "issue", "Demo-Mixed-Case-9") {
		t.Fatalf("mixed case: %+v", l)
	}
	conn, kind, key, ok := panel.ParseConnTarget(panel.ConnTarget(one.ID, "issue", "Demo-Mixed:Case-9"))
	if !ok || conn != one.ID || kind != "issue" || key != "Demo-Mixed:Case-9" {
		t.Fatalf("round trip: %q %q %q %v", conn, kind, key, ok)
	}
	// A wiki page opens in the same tab as a page.
	if l := targets.ClassifyLink(ctx, "https://two.example.com/pages/Space-Ab12"); l.Tab != "task" || l.Target != panel.ConnTarget(two.ID, "page", "Space-Ab12") {
		t.Fatalf("page: %+v", l)
	}
	// An address no connection reads is a site; the connections never pull it into the pane.
	if l := targets.ClassifyLink(ctx, "https://three.example.com/browse/DEMO-1"); l.Tab != "browser" {
		t.Fatalf("a host without a connection: %+v", l)
	}
	if l := targets.ClassifyLink(ctx, "https://one.example.com/somewhere/else"); l.Tab != "browser" {
		t.Fatalf("an address the connection does not claim: %+v", l)
	}
	// The agent's open command is consistent with the click.
	cmd, refusal := targets.Parse(jsonObj(t, `{"kind":"open","tab":"task","target":"https://two.example.com/browse/DEMO-123"}`))
	if refusal != "" || cmd.Tab != "task" || cmd.Target != b.Target {
		t.Fatalf("open task <url>: %+v %q", cmd, refusal)
	}
}

func TestConnLinkAsksToConnectWhenTheSignInWasRefused(t *testing.T) {
	c := connections.Conn{ID: "c1", State: "denied"}
	got := connLink(c, connections.LinkTarget{Kind: "issue", Key: "DEMO-1"}, "https://one.example.com/browse/DEMO-1")
	if got.Tab != "link" || got.Reason != panel.ReasonConnect || got.Target != "https://one.example.com/browse/DEMO-1" {
		t.Fatalf("%+v", got)
	}
	if got := connLink(connections.Conn{ID: "c1", State: "unreachable"}, connections.LinkTarget{Kind: "issue", Key: "A-1"}, "x"); got.Tab != "task" {
		t.Fatalf("an unreachable instance still opens the card, which tells so: %+v", got)
	}
	if got := connLink(c, connections.LinkTarget{Kind: "folder", Key: "x"}, "x"); got.Tab != "" {
		t.Fatalf("%+v", got)
	}
}

func TestLinkResolverOpensDocumentsAndBoardsThroughTheProvider(t *testing.T) {
	docs := &docsFake{}
	targets := panel.Targets{ReaderPath: "/agents/docs/"}
	targets.Resolve = linkResolver(nil, func() docprovider.Provider { return docs }, targets)
	ctx := context.Background()

	board := targets.ClassifyLink(ctx, "https://docs.example.com/app/board/Ab-Cd_9")
	if board.Tab != "artifacts" || board.Target != "/agents/docs/"+docprovider.Alias("Ab-Cd_9") || !strings.HasPrefix(board.Target, "/agents/docs/x-") {
		t.Fatalf("a board of any path: %+v", board)
	}
	if doc := targets.ClassifyLink(ctx, "https://docs.example.com/wiki/Q3-Plan"); doc.Tab != "artifacts" || doc.Target != "/agents/docs/"+docprovider.Alias("Q3-Plan") {
		t.Fatalf("a document: %+v", doc)
	}
	if l := targets.ClassifyLink(ctx, "https://docs.example.com/wiki/Legacy"); l.Tab != "link" || l.Reason != panel.ReasonEmbed || l.Target != "https://docs.example.com/wiki/Legacy" {
		t.Fatalf("unsupported embedding: %+v", l)
	}
	if l := targets.ClassifyLink(ctx, "https://docs.example.com/wiki/Secret"); l.Tab != "link" || l.Reason != panel.ReasonConnect {
		t.Fatalf("denied: %+v", l)
	}
	// Not found, unreachable and an unclaimed address keep today's behaviour.
	for _, u := range []string{"https://docs.example.com/wiki/Nope", "https://docs.example.com/wiki/Down"} {
		if l := targets.ClassifyLink(ctx, u); l.Tab != "browser" {
			t.Fatalf("%s: %+v", u, l)
		}
	}
	// A provider that is no resolver (the built-in catalog, an adapter without `resolve`) is not asked.
	plain := panel.Targets{}
	plain.Resolve = linkResolver(nil, func() docprovider.Provider { return docprovider.Builtin{} }, plain)
	if l := plain.ClassifyLink(ctx, "https://docs.example.com/app/board/Ab-Cd_9"); l.Tab != "browser" {
		t.Fatalf("no capability: %+v", l)
	}
}

func TestOpenAnswersLinkInsteadOfTheBrowserForWhatNeedsAConnect(t *testing.T) {
	docs := &docsFake{}
	targets := panel.Targets{ReaderPath: "/agents/docs/"}
	targets.Resolve = linkResolver(nil, func() docprovider.Provider { return docs }, targets)
	o := v2Open{record: func(string) (wire.Conversation, bool) { return wire.Conversation{}, true }, targets: targets}
	got := o.open(context.Background(), wire.Conversation{}, "https://docs.example.com/wiki/Legacy")
	if got == nil || got.Tab != wire.OpenLink || got.Reason != wire.OpenReasonEmbed || got.Validate() != nil {
		t.Fatalf("%+v", got)
	}
	if got := o.open(context.Background(), wire.Conversation{}, "https://docs.example.com/app/board/Ab-Cd_9"); got == nil || got.Tab != wire.OpenArtifacts {
		t.Fatalf("%+v", got)
	}
	if got := o.open(context.Background(), wire.Conversation{}, "https://docs.example.org/page"); got == nil || got.Tab != wire.OpenBrowser {
		t.Fatalf("%+v", got)
	}
}

func jsonObj(t *testing.T, raw string) any {
	t.Helper()
	v, err := jsvalue.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
