package providers

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestParseVendorModels(t *testing.T) {
	raw := `{"object":"list","data":[{"id":"grok-4.8","object":"model"},{"id":"grok-4.7","display_name":"Grok 4.7"},` +
		`{"id":"grok-4.8"},{"id":"bad id"},{"id":"glm-5.4","name":"GLM-5.4"}]}`
	got := ParseVendorModels([]byte(raw))
	want := []VendorModel{{ID: "grok-4.8"}, {ID: "grok-4.7", Name: "Grok 4.7"}, {ID: "glm-5.4", Name: "GLM-5.4"}}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v", got)
	}
	if ParseVendorModels([]byte("not json")) != nil {
		t.Fatal("garbage parsed")
	}
}

func TestWithVendorModelsAddsNewerSiblingsOnly(t *testing.T) {
	p := Preset{ID: "xai", Models: []Model{
		{ID: "grok-build-0.1", Label: "Grok Build 0.1", Window: 256 << 10},
		{ID: "grok-4.7", Label: "Grok 4.7", Window: 500_000, Efforts: grokEfforts, DefaultEffort: wire.EffortHigh},
	}}
	listed := []VendorModel{
		{ID: "grok-4.7"}, {ID: "grok-3"}, {ID: "grok-4.8"}, {ID: "grok-imagine-image-5"}, {ID: "grok-build-0.2", Name: "Grok Build 0.2 β"},
		{ID: "grok-4.8-fast-reasoning"}, {ID: "embedder"}, {ID: "grok-5"},
	}
	got := WithVendorModels(p, listed)
	var ids []string
	for _, m := range got.Models {
		ids = append(ids, m.ID)
	}
	if strings.Join(ids, ",") != "grok-build-0.2,grok-build-0.1,grok-5,grok-4.8,grok-4.7" {
		t.Fatalf("ids %v", ids)
	}
	m, _ := got.ModelOf("grok-4.8")
	if m.Label != "Grok 4.8" || m.Window != 500_000 || m.DefaultEffort != wire.EffortHigh || len(m.Efforts) != len(grokEfforts) {
		t.Fatalf("grok-4.8 = %+v", m)
	}
	if b, _ := got.ModelOf("grok-build-0.2"); b.Label != "Grok Build 0.2 β" {
		t.Fatalf("label %q", b.Label)
	}
	if len(p.Models) != 2 {
		t.Fatal("the preset itself changed")
	}
}

func TestWithVendorModelsSuffixFamilies(t *testing.T) {
	p := Preset{Models: []Model{{ID: "glm-5.3", Label: "GLM 5.3"}, {ID: "glm-5.3-flash", Label: "GLM 5.3 Flash"}}}
	got := WithVendorModels(p, []VendorModel{{ID: "glm-5.4-flash"}, {ID: "glm-4.5-air"}, {ID: "glm-5.4"}})
	var ids []string
	for _, m := range got.Models {
		ids = append(ids, m.ID+"="+m.Label)
	}
	if strings.Join(ids, ",") != "glm-5.4=GLM 5.4,glm-5.3=GLM 5.3,glm-5.4-flash=GLM 5.4 Flash,glm-5.3-flash=GLM 5.3 Flash" {
		t.Fatalf("got %v", ids)
	}
}

func TestRefreshVendorModelsKeepsAndApplies(t *testing.T) {
	var auth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-4.9"}]}`))
	}))
	defer srv.Close()
	old := vendorModelsURL
	t.Cleanup(func() { vendorModelsURL = old })
	// Only xai, on the test server's host: the host check needs the preset's BaseURL on it too.
	vendorModelsURL = map[string]string{"xai": "https://api.x.ai/v1/models"}
	tr := srv.Client().Transport.(*http.Transport).Clone()
	addr := strings.TrimPrefix(srv.URL, "https://")
	tr.Proxy = nil
	tr.TLSClientConfig.InsecureSkipVerify = true // the test server stands in for api.x.ai
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	client := &http.Client{Transport: tr}

	dir := t.TempDir()
	st := &Store{Dir: dir}
	if _, err := st.Set("xai", "xai-test-key-1234567890"); err != nil {
		t.Fatal(err)
	}
	if !RefreshVendorModels(context.Background(), st, client, true) {
		t.Fatal("nothing changed")
	}
	if auth != "Bearer xai-test-key-1234567890" {
		t.Fatalf("auth %q", auth)
	}
	p, _ := Find("xai")
	if _, ok := p.ModelOf("grok-4.9"); !ok || p.Models[1].ID != "grok-4.9" {
		t.Fatalf("models %+v", p.Models)
	}
	// Kept on the disk: a new process reads it back.
	vendorModels.Lock()
	vendorModels.loaded, vendorModels.Lists = false, nil
	vendorModels.Unlock()
	loadVendorModels(dir)
	if p, _ := Find("xai"); p.Models[1].ID != "grok-4.9" {
		t.Fatal("the list was not kept")
	}
	// Not due again: no request.
	auth = ""
	RefreshVendorModels(context.Background(), st, client, false)
	if auth != "" {
		t.Fatal("asked again before it was due")
	}
	vendorModels.Lock()
	vendorModels.Lists = map[string][]VendorModel{}
	vendorModels.Unlock()
}
