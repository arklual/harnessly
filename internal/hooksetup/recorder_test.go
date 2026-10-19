package hooksetup

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

type recorder struct {
	port    int
	headers http.Header
}

// newRecorder answers the hook route on loopback and passes each body on.
func newRecorder(t *testing.T, got chan<- string) *recorder {
	t.Helper()
	r := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		if req.URL.Path == "/api/status/hook" {
			r.headers = req.Header.Clone()
			select {
			case got <- string(raw):
			default:
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	r.port, _ = strconv.Atoi(port)
	return r
}
