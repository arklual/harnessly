package extproviders

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func higgsfieldServer(t *testing.T, answer func(w http.ResponseWriter)) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		_, _ = io.Copy(io.Discard, r.Body)
		answer(w)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestHiggsfieldCheckAsksThePriceWithTheKeyHeader(t *testing.T) {
	srv, seen := higgsfieldServer(t, func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"usd":"0.094"}`) })
	got := (&Prober{}).Check(context.Background(), srv.URL, APIHiggsfield, "kid:ksecret")
	if !got.OK || len(got.Models) == 0 || got.Models[0].ID != HiggsfieldCatalog[0].ID {
		t.Fatalf("%+v", got)
	}
	if len(*seen) != 1 || (*seen)[0] != "POST /estimate/"+checkModel+" Key kid:ksecret" {
		t.Errorf("requests %v", *seen)
	}
}

func TestHiggsfieldCheckSaysWhatIsWrong(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header string
		body   string
		ok     bool
		reason string
		words  string
	}{
		{"bad key", 401, "", `{"detail":"Invalid credentials"}`, false, CheckUnauthorized, ""},
		{"key off", 403, "no-store", `{"detail":"Key is disabled"}`, false, CheckUnauthorized, ""},
		{"empty balance", 403, "", `{"detail":"Insufficient balance"}`, true, "", "balance"},
		{"rate", 429, "", `{}`, false, CheckRateLimited, ""},
		{"moved model is still a good key", 422, "", `{"detail":[{"msg":"field required"}]}`, true, "", ""},
		{"not higgsfield", 404, "", `<html>nope</html>`, false, CheckWrongAPI, ""},
		{"server", 500, "", `oops`, false, CheckRejected, ""},
	}
	for _, c := range cases {
		srv, _ := higgsfieldServer(t, func(w http.ResponseWriter) {
			if c.header != "" {
				w.Header().Set("Cache-Control", c.header)
			}
			w.WriteHeader(c.status)
			_, _ = io.WriteString(w, c.body)
		})
		got := (&Prober{}).Check(context.Background(), srv.URL, APIHiggsfield, "a:b")
		if got.OK != c.ok || got.Reason != c.reason || (c.words != "" && !strings.Contains(got.Message, c.words)) {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
	got := (&Prober{}).Check(context.Background(), "http://127.0.0.1:1", APIHiggsfield, "a:b")
	if got.OK || got.Reason != CheckUnreachable {
		t.Errorf("unreachable: %+v", got)
	}
}

func TestAHiggsfieldKeyIsAnIDAndASecret(t *testing.T) {
	svc, sec := newService(t)
	for _, bad := range []string{"just-a-secret", "id:", ":secret", "id: secret"} {
		if _, err := svc.Create(Input{Name: "Higgsfield", BaseURL: HiggsfieldBase, API: APIHiggsfield, Preset: "higgsfield", Key: bad}); ReasonOf(err) != ReasonBadMediaKey {
			t.Errorf("%q: %v", bad, err)
		}
	}
	view, err := svc.Create(Input{Name: "Higgsfield", BaseURL: HiggsfieldBase, API: APIHiggsfield, Preset: "higgsfield", Key: "kid:ksecret"})
	if err != nil {
		t.Fatal(err)
	}
	// A provider of video: no chat runs on it, and it starts with the catalog's models.
	if !view.HasKey || view.ClaudeCLI || view.Proxied || len(view.Models) != len(HiggsfieldCatalog) {
		t.Errorf("view %+v", view)
	}
	if sec["provider-higgsfield-key"] != "kid:ksecret" {
		t.Errorf("the key is not in the safe: %v", sec)
	}
	p, _ := svc.Get(view.ID)
	if !p.Media() || !p.HasMediaModel("bytedance/seedance-2.5/image-to-video") {
		t.Errorf("provider %+v", p)
	}
	if h := Headers(APIHiggsfield, "kid:ksecret"); h.Get("Authorization") != "Key kid:ksecret" {
		t.Errorf("headers %v", h)
	}
}

func TestFindMediaModel(t *testing.T) {
	for ref, want := range map[string]string{
		"kling": "kling-video/v3.0/std/image-to-video", "seedance": "bytedance/seedance-2.5/image-to-video",
		"ltx": "lightricks/ltx-2.5/image-to-video/fast", "z-image/turbo": "z-image/turbo",
	} {
		if m, ok := FindMediaModel(ref, KindVideo, true); !ok || m.ID != want {
			t.Errorf("%s: %+v %v", ref, m, ok)
		}
	}
	if m, ok := FindMediaModel("kling", KindVideo, false); !ok || m.ID != DefaultTextVideo {
		t.Errorf("text kling: %+v", m)
	}
	if m, ok := FindMediaModel("soul", KindImage, false); !ok || m.ID != DefaultImageModel {
		t.Errorf("soul: %+v", m)
	}
	if _, ok := FindMediaModel("nope", KindVideo, true); ok {
		t.Error("a model that is not there was found")
	}
}
