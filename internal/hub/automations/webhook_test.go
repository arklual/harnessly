package automations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
)

// hookLinks is a machine link that records what was passed and answers a status.
type hookLinks struct {
	op     string
	params map[string]any
	status int
	away   bool
}

func (f *hookLinks) Get(string) *link.Link { return nil }

func (f *hookLinks) Do(_ context.Context, _ string, op string, params any, _ time.Duration) (json.RawMessage, error) {
	if f.away {
		return nil, httpx.ErrMachineOffline
	}
	f.op = op
	raw, _ := json.Marshal(params)
	_ = json.Unmarshal(raw, &f.params)
	return json.Marshal(map[string]int{"status": f.status})
}

func TestWebhookPassesToTheMachineOnly(t *testing.T) {
	s := newSvc(t, noon())
	links := &hookLinks{status: http.StatusAccepted}
	s.links = links
	webhookLimits = &limiter{m: map[string]*bucket{}}
	mux := http.NewServeMux()
	s.Mount(mux)
	post := func(path, body string, h map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/platform/automations/hook/m/"+path, strings.NewReader(body))
		for k, v := range h {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	rec := post("mach1/cx0123456789ab", `{"a":1}`, map[string]string{"X-Hub-Signature-256": "sha256=ab", "Cookie": "session=1"})
	if rec.Code != http.StatusAccepted || links.op != "connectors.webhook" || links.params["connection"] != "cx0123456789ab" {
		t.Fatalf("passed: %d %+v", rec.Code, links)
	}
	header, _ := links.params["header"].(map[string]any)
	if header["X-Hub-Signature-256"] != "sha256=ab" || header["Cookie"] != nil {
		t.Fatalf("headers: %+v", header)
	}
	// The machine's 401 is the sender's 401, without words.
	links.status = http.StatusUnauthorized
	rec = post("mach1/cx0123456789ab", `{}`, nil)
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "signature") {
		t.Fatalf("unauthorized: %d %s", rec.Code, rec.Body)
	}
	// A malformed address or secret never reaches a machine.
	links.op = ""
	if rec = post("mach1/../x", `{}`, nil); rec.Code == http.StatusAccepted || links.op != "" {
		t.Fatalf("bad address: %d", rec.Code)
	}
	if rec = post("mach1/cx0123456789ab/short", `{}`, nil); rec.Code != http.StatusUnauthorized || links.op != "" {
		t.Fatalf("bad secret: %d", rec.Code)
	}
	// Too big.
	if rec = post("mach1/cx0123456789ab", strings.Repeat("x", webhookMaxBody+1), nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too big: %d", rec.Code)
	}
	// The machine away: 503, so the sender tries again.
	links.away = true
	if rec = post("mach1/cx0123456789ab", `{}`, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("away: %d", rec.Code)
	}
	// A flood of one address is cut.
	links.away = false
	limited := false
	for range webhookBurst + 5 {
		if post("mach2/cx0123456789ab", `{}`, nil).Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("no rate limit")
	}
}
