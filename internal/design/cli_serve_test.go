package design

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestServeForCLI serves the golden fixture's API for a CLI parity run: DESIGN_SERVE=<file> gets the port, <file>.stop
// ends it; DESIGN_SERVE_TOKEN_HEADER names the header the CLI sends its token in. The same CLI commands then run against testdata/gen's GEN_SERVE and against this, and their output is diffed.
func TestServeForCLI(t *testing.T) {
	out := os.Getenv("DESIGN_SERVE")
	if out == "" {
		t.Skip()
	}
	rig := newGoldenRig(t)
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/agents") {
			path = path[len("/agents"):]
		}
		if h := os.Getenv("DESIGN_SERVE_TOKEN_HEADER"); h != "" {
			r.Header.Set("X-Cli-Token", r.Header.Get(h))
		}
		h := rig.routes[strings.TrimSuffix(path, "/")]
		if h == nil {
			w.WriteHeader(404)
			return
		}
		writeGateAnswer(w, h(r))
	})}
	go srv.Serve(ln)
	_ = os.WriteFile(out, []byte(strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)), 0o644)
	for i := 0; i < 600; i++ {
		if _, err := os.Stat(out + ".stop"); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	srv.Close()
}
