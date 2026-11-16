package connectors

import (
	"context"
	"errors"
	"os"
	"testing"
)

// pushConn is a Receiver with a generated secret: the shape of a webhook.
type pushConn struct{}

func (pushConn) Spec() Spec {
	return Spec{ID: "push", Kind: Webhook, Title: T("Push", "Пуш"),
		Fields: []Field{{ID: "secret", Type: FieldPassword, Secret: true, Generate: true, Label: T("Secret", "Секрет")}},
		Events: []string{"webhook.received"}}
}

func (pushConn) Check(context.Context, Instance) (Account, error) { return Account{Label: "hook"}, nil }

func (pushConn) Receive(_ context.Context, in Instance, req Incoming) ([]Event, error) {
	s, err := in.Secret("secret")
	if err != nil || s == "" || string(req.Header.Get("X-Key")) != s {
		return nil, ErrUnauthorized
	}
	if err := os.MkdirAll(in.Dir, 0o700); err != nil {
		return nil, err
	}
	return []Event{{Type: "webhook.received", ID: string(req.Body)}}, nil
}

func init() { Register(pushConn{}) }

func TestGeneratedSecretShownOnceReissuedAndReceived(t *testing.T) {
	safe := &memSafe{m: map[string]string{}}
	s := newSvc(t, t.TempDir(), safe)
	conn, err := s.Connect(context.Background(), "push", "", map[string]string{"secret": "ignored"})
	if err != nil || len(conn.Shown) != 1 || len(conn.Shown[0].Value) != 64 || conn.Shown[0].Value == "ignored" {
		t.Fatalf("connect: %+v %v", conn, err)
	}
	if kept, _ := s.Get(conn.ID); len(kept.Shown) != 0 {
		t.Fatal("shown values must not be kept")
	}
	first := conn.Shown[0].Value
	req := func(key, body string) (int, error) {
		in := Incoming{Header: map[string][]string{"X-Key": {key}}, Body: []byte(body)}
		return s.Receive(context.Background(), conn.ID, in)
	}
	if n, err := req(first, "a"); err != nil || n != 1 {
		t.Fatalf("receive: %d %v", n, err)
	}
	again, err := s.Reissue(conn.ID)
	if err != nil || len(again.Shown) != 1 || again.Shown[0].Value == first {
		t.Fatalf("reissue: %+v %v", again, err)
	}
	if _, err := req(first, "b"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("the old secret must stop: %v", err)
	}
	if n, err := req(again.Shown[0].Value, "b"); err != nil || n != 1 {
		t.Fatalf("new secret: %d %v", n, err)
	}
	if st := s.runner.load(conn.ID); len(st.Seen) != 2 {
		t.Fatalf("seen: %+v", st.Seen)
	}
	if err := s.Remove(conn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.dataDir(conn.ID)); !os.IsNotExist(err) {
		t.Fatal("the connection's folder must go with it")
	}
}
