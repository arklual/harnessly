package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/cdp"
)

func TestHandoffInspectionFailures(t *testing.T) {
	value := "generated-" + strings.Repeat("p9", 17)
	for _, fail := range []string{"", "Page.resetNavigationHistory", "Page.getFrameTree", "Target.setAutoAttach", "Page.createIsolatedWorld", "Runtime.evaluate", "DOM.describeNode", "DOM.resolveNode", "Runtime.callFunctionOn", "Runtime.releaseObjectGroup", "malformed", "exception"} {
		name := fail
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			var calls []string
			masked := false
			call := func(_ context.Context, _, method string, _ any) (json.RawMessage, error) {
				calls = append(calls, method)
				if method == fail {
					return nil, errors.New("generated CDP failure")
				}
				switch method {
				case "Page.getFrameTree":
					return json.RawMessage(`{"frameTree":{"frame":{"id":"main","loaderId":"document"}}}`), nil
				case "Page.createIsolatedWorld":
					return json.RawMessage(`{"executionContextId":3}`), nil
				case "Runtime.evaluate":
					return json.RawMessage(`{"result":{"objectId":"document-object"}}`), nil
				case "DOM.describeNode":
					return json.RawMessage(`{"node":{"backendNodeId":1,"nodeName":"#document","children":[{"backendNodeId":2,"nodeName":"INPUT"}]}}`), nil
				case "DOM.resolveNode":
					return json.RawMessage(`{"object":{"objectId":"input-object"}}`), nil
				case "Runtime.callFunctionOn":
					if fail == "malformed" {
						return json.RawMessage(`{"result":{"value":5}}`), nil
					}
					if fail == "exception" {
						return json.RawMessage(`{"exceptionDetails":{}}`), nil
					}
					raw, err := json.Marshal(map[string]any{"result": map[string]any{"value": value}})
					return raw, err
				}
				return json.RawMessage(`{}`), nil
			}
			marker, touched, err := inspectHandoff(t.Context(), call, func() []AgentFrame { return nil }, func(s string) {
				if s != value {
					t.Fatal("wrong password masked")
				}
				masked = true
			})
			if (err != nil) != (fail != "") {
				t.Fatalf("failure not closed for %s: %v", fail, err)
			}
			if errors.Is(err, ErrHandoffHistory) != (fail == "Page.resetNavigationHistory") {
				t.Fatal("history failure lost its distinct handoff outcome")
			}
			if marker == "" || fail == "Page.getFrameTree" && marker != unknownHandoffDocument {
				t.Fatal("missing durable failure marker")
			}
			if fail == "" && (!touched || !masked || marker != "document") {
				t.Fatal("password did not seal and mask its browser document")
			}
			if calls[0] != "Page.resetNavigationHistory" {
				t.Fatal("inspection preceded history reset")
			}
		})
	}
}

func TestHandoffFailureSealsBeforeLoginCompletion(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	x := &loginRequest{ask: LoginAsk{Tab: "tab"}, instance: i, answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "https://example.org/account", Control: "person"})
	p.login = x
	i.pages[p.ID] = p
	connectHandoffFixture(t, i, p, "Page.getFrameTree")
	observed := make(chan Tab, 1)
	go func() {
		<-x.done
		observed <- i.Tabs("chat")[0]
	}()
	// History resets successfully, but frame discovery fails on a private transport.
	must(t, i.SetControl(p.ID, "agent"))
	tab := <-observed
	if tab.Control != "agent" || !tab.Sealed || !tab.SecretTouched || !tab.SecretUntracked || tab.SecretDocument != unknownHandoffDocument {
		t.Fatal("login completion preceded the fail-closed handoff")
	}
	raw, err := os.ReadFile(filepath.Join(i.dir, "tabs.json"))
	must(t, err)
	if strings.Contains(string(raw), "example.org") {
		t.Fatal("failed handoff persisted private navigation metadata")
	}
	restored := newInstance(m, DefaultProfile)
	defer restored.cancel()
	if tab := restored.Tabs("chat")[0]; !tab.Sealed || tab.SecretDocument != unknownHandoffDocument || !tab.SecretTouched || !tab.SecretUntracked {
		t.Fatal("failed handoff did not survive restart")
	}
}

func TestHandoffHistoryFailureKeepsPersonControl(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	x := &loginRequest{answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	p := newPage(Tab{ID: "tab", URL: "about:blank", Conversation: "chat", Control: "person"})
	p.login = x
	i.pages[p.ID] = p
	connectHandoffFixture(t, i, p, "Page.resetNavigationHistory")
	if err := i.SetControl(p.ID, "agent"); !errors.Is(err, ErrHandoffHistory) {
		t.Fatalf("history failure returned %v", err)
	}
	if p.Control != "person" || !p.Sealed || !p.SecretUntracked || !p.SecretTouched {
		t.Fatal("history failure released personal browser contents")
	}
	if err := i.AgentCheck("chat", p.ID, "Page.navigateToHistoryEntry"); !errors.Is(err, ErrPersonControl) {
		t.Fatal("agent can restore personal history after failed handoff")
	}
	select {
	case <-x.done:
		t.Fatal("history failure completed the login")
	default:
	}
	restored := newInstance(m, DefaultProfile)
	defer restored.cancel()
	if tab := restored.Tabs("chat")[0]; tab.Control != "person" || !tab.SecretUntracked || !tab.Sealed {
		t.Fatal("history failure did not persist its person-control guard")
	}
}

func TestRestoredSealRequiresDocumentProof(t *testing.T) {
	if p := newPage(Tab{ID: "tab", Sealed: true, SecretDocument: "document"}); !p.SecretUntracked {
		t.Fatal("restored seal trusted node references from the previous runtime")
	}
}

func TestHandoffPersistenceFailureKeepsPersonControl(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	blocked := filepath.Join(t.TempDir(), "blocked")
	must(t, os.WriteFile(blocked, []byte("fixture"), 0600))
	i.dir = filepath.Join(blocked, "profile")
	x := &loginRequest{answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	p := newPage(Tab{ID: "tab", URL: "about:blank", Conversation: "chat", Control: "person"})
	p.login = x
	i.pages[p.ID] = p
	if err := i.SetControl(p.ID, "agent"); err == nil {
		t.Fatal("failed persistence returned control to the agent")
	}
	if p.Control != "person" || !p.Sealed || p.SecretDocument == "" {
		t.Fatal("failed persistence lost the in-memory guard")
	}
	select {
	case <-x.done:
		t.Fatal("failed handoff completed the login")
	default:
	}
}

// The unit fixtures exercise SetControl without a Chrome process or a real CDP
// endpoint. Successful history reset can be distinguished from later failures.
func connectHandoffFixture(t *testing.T, i *Instance, p *page, fail string) {
	t.Helper()
	transport := &handoffFixtureTransport{replies: make(chan []byte, 1), done: make(chan struct{}), fail: fail}
	c := cdp.New(transport, nil)
	i.conn = c
	p.binding = binding{conn: c, session: "fixture-session", target: p.ID}
	go func() { _ = c.Run(t.Context()) }()
	t.Cleanup(func() { _ = c.Close() })
}

type handoffFixtureTransport struct {
	replies chan []byte
	done    chan struct{}
	fail    string
}

func (x *handoffFixtureTransport) Read(ctx context.Context) ([]byte, error) {
	select {
	case raw := <-x.replies:
		return raw, nil
	case <-x.done:
		return nil, cdp.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (x *handoffFixtureTransport) Write(ctx context.Context, raw []byte) error {
	var request struct {
		ID     uint64 `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &request); err != nil {
		return err
	}
	result := json.RawMessage(`{}`)
	switch request.Method {
	case "Page.getFrameTree":
		result = json.RawMessage(`{"frameTree":{"frame":{"id":"main","loaderId":"document"}}}`)
	case "Page.createIsolatedWorld":
		result = json.RawMessage(`{"executionContextId":3}`)
	case "Runtime.evaluate":
		result = json.RawMessage(`{"result":{"objectId":"document-object"}}`)
	case "DOM.describeNode":
		result = json.RawMessage(`{"node":{"backendNodeId":1,"nodeName":"#document"}}`)
	}
	reply := map[string]any{"id": request.ID, "result": result}
	if request.Method == x.fail {
		reply = map[string]any{"id": request.ID, "error": map[string]any{"message": "Fixture failure"}}
	}
	raw, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	select {
	case x.replies <- raw:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-x.done:
		return cdp.ErrClosed
	}
}

func (x *handoffFixtureTransport) Close() error { close(x.done); return nil }
