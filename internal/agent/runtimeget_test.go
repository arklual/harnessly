package agent

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// what the runtime gave is told as it was — a cut answer is not «answered 200».
func TestRuntimeGetSaysWhatWasWrongWithTheAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte(`{"turns":[]}`))
		case "/cut":
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte(`{"turns":[`))
		case "/text":
			_, _ = w.Write([]byte(`not json`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	p, _ := strconv.Atoi(port)
	a := &Agent{cfg: Config{RuntimePort: p, RuntimeCookie: "s"}, secret: []byte("k"), client: srv.Client()}
	if out, err := a.runtimeGet("/ok"); err != nil || string(out) != `{"turns":[]}` {
		t.Fatalf("ok: %s %v", out, err)
	}
	for path, want := range map[string]string{"/cut": "not read whole", "/text": "not JSON", "/none": "answered 404"} {
		if _, err := a.runtimeGet(path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", path, err)
		}
	}
}
