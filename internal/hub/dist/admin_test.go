package dist

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

// adminServer mounts the admin routes; the person is an admin when the header X-Admin is set, signed out without X-User.
func adminServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	mux := http.NewServeMux()
	h := New(dir, "https://hub.example.com", "1", "")
	h.Mount(mux)
	h.MountAdmin(mux, func(f func(http.ResponseWriter, *http.Request, bool) error) httpx.Handler {
		return func(w http.ResponseWriter, r *http.Request) error {
			if r.Header.Get("X-User") == "" {
				return httpx.ErrUnauthenticated
			}
			return f(w, r, r.Header.Get("X-Admin") != "")
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, dir
}

func do(t *testing.T, method, url, body string, headers ...string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res.StatusCode, sb.String()
}

func TestAdminNetworkPolicyIsForAdminsAndMachinesSeeIt(t *testing.T) {
	srv, dir := adminServer(t)
	admin := []string{"X-User", "u1", "X-Admin", "1"}
	if code, _ := do(t, "GET", srv.URL+"/api/admin/network", ""); code != 401 {
		t.Errorf("signed out: %d", code)
	}
	if code, _ := do(t, "GET", srv.URL+"/api/admin/network", "", "X-User", "u2"); code != 403 {
		t.Errorf("not an admin: %d", code)
	}
	if code, _ := do(t, "PUT", srv.URL+"/api/admin/network", `{"proxy":"p:1"}`, "X-User", "u2"); code != 403 {
		t.Errorf("not an admin writes: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, NetworkFile)); err == nil {
		t.Fatal("a refused write leaves no file")
	}
	// No policy yet: an empty one, and the machines get 404.
	if code, body := do(t, "GET", srv.URL+"/api/admin/network", "", admin...); code != 200 || strings.TrimSpace(body) != "{}" {
		t.Errorf("empty: %d %s", code, body)
	}
	if res, _ := get(t, srv.URL+"/dl/network.json"); res.StatusCode != 404 {
		t.Errorf("machines before: %d", res.StatusCode)
	}

	put := `{"internalHosts":["crm.bank.local","*.bank.local"],"proxy":"http://proxy.bank.local:3128","noProxy":[".bank.local"]}`
	if code, body := do(t, "PUT", srv.URL+"/api/admin/network", put, admin...); code != 200 {
		t.Fatalf("put: %d %s", code, body)
	}
	// Machines take it from the usual place, with no restart of the hub.
	res, body := get(t, srv.URL+"/dl/network.json")
	if res.StatusCode != 200 || !strings.Contains(body, "crm.bank.local") || !strings.Contains(body, "proxy.bank.local:3128") {
		t.Errorf("machines after: %d %s", res.StatusCode, body)
	}
	if code, body := do(t, "GET", srv.URL+"/api/admin/network", "", admin...); code != 200 || !strings.Contains(body, "*.bank.local") {
		t.Errorf("admin reads it back: %d %s", code, body)
	}
}

func TestAdminNetworkPolicyRefusesWhatIsWrongWithTheFieldNamed(t *testing.T) {
	srv, dir := adminServer(t)
	admin := []string{"X-User", "u1", "X-Admin", "1"}
	code, body := do(t, "PUT", srv.URL+"/api/admin/network", `{"internalHosts":["https://crm.bank.local/x"],"proxy":"ftp://x","requireSigned":true}`, admin...)
	if code != 422 || !strings.Contains(body, `"internalHosts"`) || !strings.Contains(body, `"proxy"`) || !strings.Contains(body, `"requireSigned"`) {
		t.Fatalf("every problem comes back with its field: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(dir, NetworkFile)); err == nil {
		t.Fatal("a policy with problems is not written")
	}
	if code, _ := do(t, "PUT", srv.URL+"/api/admin/network", `{`, admin...); code != 400 {
		t.Errorf("broken JSON: %d", code)
	}
}
