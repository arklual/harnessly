package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func v1Server(t *testing.T, describe bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if describe {
		mux.HandleFunc("GET /v1/describe", func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(Description{Protocol: ProtocolV1, Name: "x", Version: "1", Capabilities: []string{CapCreate, CapPower}})
		})
		mux.HandleFunc("POST /v1/machines", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(CreateRef{Op: "op1", Machine: "m1"})
		})
	}
	mux.HandleFunc("POST /machines/{id}/reboot", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(OpRef{ID: "v0op"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestAutoPicksV1(t *testing.T) {
	a, err := NewAuto(HTTPConfig{BaseURL: v1Server(t, true).URL}, ProtoAuto)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.Create(context.Background(), CreateRequest{Name: "n"})
	if err != nil || ref.Machine != "m1" || a.Name() != "http/v1" {
		t.Fatalf("create: %+v %v name %s", ref, err, a.Name())
	}
	// a capability that is not listed is never sent
	if _, err := a.Resize(context.Background(), Machine{ID: "m"}, Resources{CPU: 1, RAMGB: 1, DiskGB: 10}); err != ErrUnsupported {
		t.Fatalf("resize without the capability: %v", err)
	}
}

func TestAutoFallsBackToV0(t *testing.T) {
	a, err := NewAuto(HTTPConfig{BaseURL: v1Server(t, false).URL}, ProtoAuto)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := a.Reboot(context.Background(), Machine{ID: "m"})
	if err != nil || ref.ID != "v0op" || a.Name() != "http/v0" {
		t.Fatalf("reboot: %+v %v name %s", ref, err, a.Name())
	}
	if _, err := a.Create(context.Background(), CreateRequest{Name: "n"}); err != ErrUnsupported {
		t.Fatalf("create on v0: %v", err)
	}
}

func TestV1ModeRefusesV0(t *testing.T) {
	a, err := NewAuto(HTTPConfig{BaseURL: v1Server(t, false).URL}, ProtoV1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reboot(context.Background(), Machine{ID: "m"}); err == nil {
		t.Fatal("v1 mode spoke to a v0 service")
	}
}
