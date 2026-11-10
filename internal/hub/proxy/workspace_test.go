package proxy_test

import (
	"html"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// workspaceIndex is the shape of web/dist/index.html: the placeholders the runtime (and the hub) fill in.
const workspaceIndex = `<!doctype html><html><head><base href="/" /><meta name="api-base" content="" />` +
	`<meta name="platform-api" content="/api/platform" /><meta name="hl-config" content="{}" /><title>harnessly</title>` +
	`</head><body><script type="module" src="./assets/app.js"></script></body></html>`

var withWorkspace = hubtest.WithOptions(func(o *api.Options) {
	o.Workspace = fstest.MapFS{
		"index.html":    {Data: []byte(workspaceIndex)},
		"assets/app.js": {Data: []byte("console.log(1)")},
		"sw.js":         {Data: []byte("self")},
	}
})

// pageRuntime is a machine whose page carries settings of its own; everything else tells what reached it.
func pageRuntime(name string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /m/{id}/{$}", func(w http.ResponseWriter, r *http.Request) {
		cfg := `{"productName":"` + name + `","features":{"chats":true},"hubUrl":"https://elsewhere.example.com","basePath":"/evil","platformApi":"/api/evil"}`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<html><head><meta name="platform-api" content="/api/`+strings.ToLower(name)+`" />`+
			`<meta name="hl-config" content="`+html.EscapeString(cfg)+`" /></head></html>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, name+" "+r.URL.RequestURI())
	})
	return mux
}

// workspace is a person with two machines in two spaces, Acme and Beta; home is the one the fleet lists first.
type workspace struct {
	s                   *hubtest.Stack
	owner               *hubtest.Person
	home, other         string
	homeName, otherName string
}

func setupWorkspace(t *testing.T, opts ...hubtest.Option) *workspace {
	t.Helper()
	w := &workspace{s: hubtest.New(t, append([]hubtest.Option{withWorkspace}, opts...)...)}
	w.owner = w.s.SignIn("owner@example.com")
	first, _ := w.owner.NewSpace("Команда")
	second, _ := w.owner.NewSpace("Вторая")
	a := w.owner.Machine(first, pageRuntime("Acme")).MachineID
	b := w.owner.Machine(second, pageRuntime("Beta")).MachineID
	w.home, w.other, w.homeName, w.otherName = a, b, "Acme", "Beta"
	// Which one is home is the fleet's choice (s.home), not the workspace's: ask it.
	r := w.owner.Do("GET", "/agents/api/which", nil)
	if strings.HasPrefix(string(r.Body), "Beta ") {
		w.home, w.other, w.homeName, w.otherName = b, a, "Beta", "Acme"
	}
	return w
}

var navigate = []string{"Accept", "text/html", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "document"}

func TestWorkspaceIsDarkWithoutTheFlag(t *testing.T) {
	w := setupWorkspace(t)
	if r := w.owner.Do("GET", "/agents/", nil, navigate...); r.Code != 404 {
		t.Errorf("page: %s", r)
	}
	if r := w.owner.Do("GET", "/agents/api/platform/bootstrap", nil); r.Code != 404 {
		t.Errorf("bootstrap: %s", r)
	}
	if r := w.owner.Do("GET", "/agents/api/v2/machines", nil); r.Code != 404 {
		t.Errorf("machines: %s", r)
	}
}

func TestWorkspaceServesTheRuntimePageWithTheHomeMachinesSettings(t *testing.T) {
	w := setupWorkspace(t, fleetOn)
	for _, p := range []string{"/agents/", "/agents/chats/42?machine=" + w.other} {
		r := w.owner.Do("GET", p, nil, navigate...)
		if r.Code != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %s", p, r)
		}
		body := string(r.Body)
		for _, want := range []string{`<base href="/agents/" />`, `<meta name="api-base" content="/agents" />`,
			`<meta name="platform-api" content="/api/` + strings.ToLower(w.homeName) + `" />`, `<title>` + w.homeName + `</title>`, `&#34;chats&#34;:true`,
			`&#34;hubUrl&#34;:&#34;` + w.s.URL + `&#34;`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %s in %s", p, want, body)
			}
		}
		for _, bad := range []string{"elsewhere.example.com", "/evil", "/api/evil"} {
			if strings.Contains(body, bad) {
				t.Errorf("%s: the machine's own %s is in the page", p, bad)
			}
		}
		if got := r.Header.Get("X-Frame-Options"); got != "SAMEORIGIN" {
			t.Errorf("%s: X-Frame-Options %q", p, got)
		}
	}
	if r := w.owner.Do("GET", "/agents/assets/app.js", nil); r.Code != 200 || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %s %q", r, r.Header.Get("Cache-Control"))
	}
	if r := w.owner.Do("GET", "/agents/sw.js", nil); r.Code != 200 || r.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("service worker: %s", r)
	}
	if r := w.owner.Do("GET", "/agents/nothing.js", nil); r.Code != 404 {
		t.Errorf("a file that is not there: %s", r)
	}
	if r := w.owner.Do("GET", "/agents", nil); r.Code != 302 || r.Header.Get("Location") != "/agents/" {
		t.Errorf("no slash: %s", r)
	}
}

func TestWorkspaceAPIGoesToTheNamedMachineOrTheHomeOne(t *testing.T) {
	w := setupWorkspace(t, fleetOn)
	if r := w.owner.Do("GET", "/agents/api/platform/bootstrap", nil); r.Code != 200 || string(r.Body) != w.homeName+" /agents/api/platform/bootstrap" {
		t.Errorf("bootstrap goes home: %s", r)
	}
	if r := w.owner.Do("GET", "/agents/api/platform/bootstrap?machine=local&x=1", nil); r.Code != 200 || string(r.Body) != w.homeName+" /agents/api/platform/bootstrap?x=1" {
		t.Errorf("machine=local goes home: %s", r)
	}
	r := w.owner.Do("GET", "/agents/api/platform/design/engine?machine="+w.other, nil)
	if r.Code != 200 || string(r.Body) != w.otherName+" /agents/api/platform/design/engine" {
		t.Errorf("a named machine: %s", r)
	}
	var list fleetItems
	if r := w.owner.Do("GET", "/agents/api/v2/machines", nil); r.Code != 200 {
		t.Fatalf("v2 machines: %s", r)
	} else if r.JSON(&list); len(list) != 2 {
		t.Errorf("v2 machines: %+v", list)
	}
	if r := w.owner.Do("GET", "/agents/api/v2/ping?machine="+w.other, nil); r.Code != 200 || string(r.Body) != w.otherName+" /api/v2/ping" {
		t.Errorf("v2 on a machine: %s", r)
	}
	if r := w.owner.Do("GET", "/agents/api/platform/bootstrap?machine=m_nothing", nil); r.Code != 404 {
		t.Errorf("a machine that is not there: %s", r)
	}
	if r := w.owner.Do("GET", "/agents/api/platform/bootstrap", nil, "Sec-Fetch-Site", "cross-site"); r.Code != 403 || r.Err() != "cross_site" {
		t.Errorf("cross-site: %s", r)
	}
}

func TestWorkspaceAsksAStrangerToSignIn(t *testing.T) {
	w := setupWorkspace(t, fleetOn)
	anon := w.s.Browser()
	if r := anon.Do("GET", "/agents/chats", nil, navigate...); r.Code != 302 || r.Header.Get("Location") != "/signin?next=%2Fagents%2Fchats" {
		t.Errorf("page: %s %q", r, r.Header.Get("Location"))
	}
	if r := anon.Do("GET", "/agents/api/platform/bootstrap", nil); r.Code != 401 {
		t.Errorf("bootstrap: %s", r)
	}
	stranger := w.s.SignIn("stranger@example.com")
	stranger.NewSpace("Чужая")
	if r := stranger.Do("GET", "/agents/api/platform/bootstrap?machine="+w.home, nil); r.Code != 403 {
		t.Errorf("someone else's machine: %s", r)
	}
	if r := stranger.Do("GET", "/agents/api/platform/bootstrap", nil); r.Code != 404 {
		t.Errorf("no machine of their own: %s", r)
	}
}

// What a machine answers in the workspace runs on the hub's origin, so a machine the hub does not trust is not there,
// in path mode and in subdomain mode alike (where /api/v2 still lists it: the machine runs on an origin of its own).
func TestWorkspaceLeavesOutMachinesTheHubDoesNotTrust(t *testing.T) {
	for _, mode := range []struct {
		name string
		opts []hubtest.Option
	}{{"path", nil}, {"subdomain", []hubtest.Option{hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain)}}} {
		t.Run(mode.name, func(t *testing.T) {
			w := setupWorkspace(t, append([]hubtest.Option{fleetOn}, mode.opts...)...)
			member := w.s.SignIn("member@example.com")
			space, _ := member.NewSpace("Своя")
			theirs := member.Machine(space, pageRuntime("Gamma")).MachineID

			var list fleetItems
			if r := member.Do("GET", "/agents/api/v2/machines", nil); r.Code != 200 {
				t.Fatalf("workspace machines: %s", r)
			} else if r.JSON(&list); len(list) != 0 {
				t.Errorf("an untrusted machine is in the workspace: %+v", list)
			}
			if r := member.Do("GET", "/agents/api/platform/bootstrap?machine="+theirs, nil); r.Code != 403 || r.Err() != "machine_untrusted" {
				t.Errorf("named: %s", r)
			}
			if r := member.Do("GET", "/agents/api/v2/ping?machine="+theirs, nil); r.Code != 403 || r.Err() != "machine_untrusted" {
				t.Errorf("v2: %s", r)
			}
			if r := member.Do("GET", "/agents/api/platform/bootstrap", nil); r.Code != 404 {
				t.Errorf("no home: %s", r)
			}
			if r := member.Do("GET", "/agents/", nil, navigate...); r.Code != 200 || strings.Contains(string(r.Body), "Gamma") {
				t.Errorf("the page takes no settings from an untrusted machine: %s", r)
			}
			list = nil
			r := member.Do("GET", "/api/v2/machines", nil)
			r.JSON(&list)
			if want := map[string]int{"path": 0, "subdomain": 1}[mode.name]; len(list) != want {
				t.Errorf("/api/v2/machines lists %d, want %d: %s", len(list), want, r)
			}
		})
	}
}

// hangingRuntime is pageRuntime whose runtime stops answering while hang is set, its agent keeping the link.
func hangingRuntime(name string, hang *atomic.Bool) http.Handler {
	page := pageRuntime(name)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hang.Load() {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		page.ServeHTTP(w, r)
	})
}

// HRN-879: a home machine whose runtime hangs while its agent keeps the link held every load of /agents/ for the whole
// wait on its settings, and kept answering for the person (me) with nothing. The page waits a moment once, the
// machine is not asked again for a while, and the person's other machine that is on answers for them meanwhile.
func TestWorkspaceDoesNotWaitOnAHomeMachineThatHangs(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	first, _ := owner.NewSpace("Команда")
	second, _ := owner.NewSpace("Вторая")
	var hangA, hangB atomic.Bool
	owner.Machine(first, hangingRuntime("Acme", &hangA))
	owner.Machine(second, hangingRuntime("Beta", &hangB))
	hang, otherName := &hangA, "Beta"
	if r := owner.Do("GET", "/agents/api/which", nil); strings.HasPrefix(string(r.Body), "Beta ") {
		hang, otherName = &hangB, "Acme"
	}
	hang.Store(true)
	defer hang.Store(false)

	start := time.Now()
	if r := owner.Do("GET", "/agents/", nil, navigate...); r.Code != 200 {
		t.Fatalf("page: %s", r)
	}
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("the first page waited %s on the machine that hangs", took)
	}
	start = time.Now()
	if r := owner.Do("GET", "/agents/", nil, navigate...); r.Code != 200 {
		t.Fatalf("page again: %s", r)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("the machine that hangs was waited on again: %s", took)
	}
	if r := owner.Do("GET", "/agents/api/which", nil); r.Code != 200 || !strings.HasPrefix(string(r.Body), otherName+" ") {
		t.Errorf("the person's own calls go to the machine that answers: %s", r)
	}
}
