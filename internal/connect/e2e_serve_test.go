package connect

import (
	"fmt"
	"net/http"
	"os"
	"testing"
)

// TestE2EServe is a stand-in runtime for trying the real binaries: CONNECT_E2E_ADDR=127.0.0.1:PORT.
func TestE2EServe(t *testing.T) {
	addr := os.Getenv("CONNECT_E2E_ADDR")
	if addr == "" {
		t.Skip()
	}
	srv := &Server{Dir: t.TempDir(), Logf: func(f string, a ...any) { fmt.Printf(f+"\n", a...) }}
	mux := http.NewServeMux()
	mux.HandleFunc("/connect/v1/register", srv.ServeRegister)
	mux.HandleFunc("/connect/v1/dial", srv.ServeDial)
	mux.HandleFunc("/api/cli/connect/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-cli-token") != "tok" {
			http.Error(w, "no", 401)
			return
		}
		srv.ServeExec(w, r)
	})
	code, _ := srv.IssueCode()
	fmt.Println("CODE", code)
	t.Fatal(http.ListenAndServe(addr, mux))
}
