package extsdk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ly-solutions/harnessly/extsdk"
	"github.com/ly-solutions/harnessly/internal/hub/ext"
)

const proto = "harnessly.test/v1"

// The adapter built from the SDK is what the hub's client expects: describe, the token, and a replay of a repeated key.
func TestAdapterAnswersTheHubClient(t *testing.T) {
	s := extsdk.New(extsdk.Description{Protocol: proto, Name: "demo", Version: "0.1.0", Capabilities: []string{"make"}}, extsdk.Options{Token: "tok"})
	var works atomic.Int32
	s.Handle("POST /v1/things", s.Idempotent(func(w http.ResponseWriter, _ *http.Request) {
		works.Add(1)
		extsdk.WriteJSON(w, http.StatusAccepted, map[string]string{"op": "op-1"})
	}))
	srv := httptest.NewServer(s)
	defer srv.Close()

	// without the token the hub is refused
	bare, _ := ext.NewClient(ext.Config{BaseURL: srv.URL, Attempts: 1})
	if _, err := bare.Describe(context.Background(), proto); err == nil {
		t.Fatal("no token was accepted")
	}

	dir := t.TempDir()
	tokFile := dir + "/tok"
	if err := writeFile(tokFile, "tok\n"); err != nil {
		t.Fatal(err)
	}
	c, err := ext.NewClient(ext.Config{BaseURL: srv.URL, TokenFile: tokFile, Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	d, err := c.Describe(context.Background(), proto)
	if err != nil || d.Name != "demo" || !d.Has("make") {
		t.Fatalf("describe: %+v %v", d, err)
	}

	ctx := ext.WithIdempotencyKey(context.Background(), "k1")
	var a, b struct{ Op string }
	if err := c.Do(ctx, http.MethodPost, "/v1/things", struct{}{}, true, &a); err != nil {
		t.Fatal(err)
	}
	if err := c.Do(ctx, http.MethodPost, "/v1/things", struct{}{}, true, &b); err != nil {
		t.Fatal(err)
	}
	if a.Op != "op-1" || a != b || works.Load() != 1 {
		t.Fatalf("a repeat with the same key did the work again: %+v %+v, %d times", a, b, works.Load())
	}
	// another key is another job
	if err := c.Do(ext.WithIdempotencyKey(context.Background(), "k2"), http.MethodPost, "/v1/things", struct{}{}, true, &b); err != nil || works.Load() != 2 {
		t.Fatalf("a new key: %v, %d", err, works.Load())
	}
}

func TestNotSupportedIsReadAsUnsupported(t *testing.T) {
	s := extsdk.New(extsdk.Description{Protocol: proto, Name: "d", Version: "1"}, extsdk.Options{})
	s.Handle("GET /v1/x", extsdk.NotSupported)
	srv := httptest.NewServer(s)
	defer srv.Close()
	c, _ := ext.NewClient(ext.Config{BaseURL: srv.URL, Attempts: 1})
	if err := c.Do(context.Background(), http.MethodGet, "/v1/x", nil, false, nil); err != ext.ErrUnsupported {
		t.Fatalf("got %v", err)
	}
	var d extsdk.Description
	r, _ := http.Get(srv.URL + "/v1/describe")
	_ = json.NewDecoder(r.Body).Decode(&d)
	if d.Capabilities == nil {
		t.Error("capabilities must be a list, not null")
	}
}
