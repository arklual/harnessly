package connectors

import (
	"context"
	"strings"
	"testing"
)

// pickConn has two ways to connect (a field shown only for one of them), a discovered choice of several, and refuses
// its action for one way before a card is shown.
type pickConn struct{}

func (pickConn) Spec() Spec {
	return Spec{
		ID: "pick", Kind: Calendar, Title: T("Pick", "Выбор"),
		Fields: []Field{
			{ID: "way", Type: FieldChoice, Default: "login", Label: T("Way", "Способ"),
				Choices: []Choice{{Value: "login", Label: T("Login", "Вход")}, {Value: "link", Label: T("Link", "Ссылка")}}},
			{ID: "password", Type: FieldPassword, Secret: true, Label: T("Password", "Пароль"), When: &When{Field: "way", Values: []string{"login"}}},
			{ID: "link", Type: FieldURL, Secret: true, Label: T("Link", "Ссылка"), When: &When{Field: "way", Values: []string{"link"}}},
			{ID: "lists", Type: FieldChoices, Discover: true, Optional: true, Label: T("Lists", "Списки"), When: &When{Field: "way", Values: []string{"login"}}},
		},
		Actions: []Action{{Name: "make", Summary: "make one", Title: T("Make", "Сделать")}},
	}
}

func (pickConn) Check(ctx context.Context, in Instance) (Account, error) {
	return Account{Label: "me"}, nil
}

func (pickConn) Discover(ctx context.Context, in Instance, field string) ([]Choice, error) {
	pw, _ := in.Secret("password")
	if pw != "right" {
		return nil, Sayf("wrong password")
	}
	return []Choice{{Value: "/a/", Label: T("A", "А")}, {Value: "/b/", Label: T("B", "Б")}}, nil
}

func (pickConn) Act(ctx context.Context, in Instance, action string, args map[string]string) (Result, error) {
	return Result{Text: "made"}, nil
}

func (pickConn) CanAct(in Instance, action string) error {
	if in.Config["way"] == "link" {
		return Sayf("read only")
	}
	return nil
}

func init() { Register(pickConn{}) }

func TestWhenDiscoverAndActGate(t *testing.T) {
	ctx := context.Background()
	safe := &memSafe{m: map[string]string{}}
	s := newSvc(t, t.TempDir(), safe)
	choices, err := s.Discover(ctx, "pick", "lists", map[string]string{"password": "right"})
	if err != nil || len(choices) != 2 {
		t.Fatalf("discover: %v %v", choices, err)
	}
	if _, err := s.Discover(ctx, "pick", "lists", map[string]string{"password": "bad"}); err == nil || !strings.Contains(Say(err), "wrong password") {
		t.Fatalf("discover's words: %v", err)
	}
	// The link way: the password is neither asked nor kept, the link is required.
	if _, err := s.Connect(ctx, "pick", "", map[string]string{"way": "link"}); err == nil {
		t.Fatal("a shown required field was not asked")
	}
	conn, err := s.Connect(ctx, "pick", "", map[string]string{"way": "link", "link": "https://example.com/x.ics", "password": "stray", "lists": "/a/"})
	if err != nil || conn.Config["lists"] != "" || len(conn.Secrets) != 1 || conn.Secrets[0] != "link" {
		t.Fatalf("hidden fields kept: %+v %v", conn, err)
	}
	if _, err := s.Propose(conn.ID, "make", nil, "why", By{}); err == nil || Say(err) != "read only" {
		t.Fatalf("the gate did not refuse: %v", err)
	}
	conn2, err := s.Connect(ctx, "pick", "", map[string]string{"password": "right", "lists": " /a/, ,/b/"})
	if err != nil || conn2.Config["lists"] != "/a/,/b/" {
		t.Fatalf("choices: %+v %v", conn2, err)
	}
	if _, err := s.Propose(conn2.ID, "make", nil, "why", By{}); err != nil {
		t.Fatal(err)
	}
}
