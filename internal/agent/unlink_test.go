package agent

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestUnlinkSignsForTheHubAndReadsItsAnswer(t *testing.T) {
	keys, err := newMachineKeys()
	if err != nil {
		t.Fatal(err)
	}
	status := http.StatusNoContent
	var verified error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/agent/v1" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		a, err := hubproto.ParseDialHeader(r.Header.Get("Authorization"))
		if err != nil {
			t.Error(err)
		} else {
			pub := ed25519.NewKeyFromSeed(keys.seed).Public().(ed25519.PublicKey)
			host, _ := hubproto.DialHost("http://" + r.Host)
			verified = hubproto.VerifyUnlink(pub, a, host, time.Now())
			// The same header is no dial.
			if hubproto.VerifyDial(pub, a, host, time.Now()) == nil {
				t.Error("an unlink signature passed as a dial")
			}
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	m := keys.file(hubproto.RegisterResponse{MachineID: "abcdefghijklmnopqrst", HubURL: srv.URL, DialURL: "ws" + strings.TrimPrefix(srv.URL, "http") + "/agent/v1", RuntimeBasePath: "/m/abcdefghijklmnopqrst"}, hubproto.ProfileFull, time.Now())
	if err := Unlink(context.Background(), m, nil, time.Now()); err != nil || verified != nil {
		t.Fatalf("%v %v", err, verified)
	}
	status = http.StatusForbidden // removed in the hub already
	if err := Unlink(context.Background(), m, nil, time.Now()); err != nil {
		t.Errorf("a machine gone already is not an error: %v", err)
	}
	status = http.StatusUnauthorized
	if err := Unlink(context.Background(), m, nil, time.Now()); err == nil {
		t.Error("a refused signature was taken as done")
	}
	srv.Close()
	if err := Unlink(context.Background(), m, nil, time.Now()); err == nil || !strings.Contains(err.Error(), "could not reach the hub") {
		t.Errorf("%v", err)
	}
}
