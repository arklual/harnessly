package machines_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
)

// cloud is a provider that makes a server per create and remembers what it was asked.
type cloud struct {
	provider.None
	mu       sync.Mutex
	made     int
	attached []string
}

func (c *cloud) Name() string { return "cloud" }

func (c *cloud) Describe(context.Context) (provider.Description, error) {
	return provider.Description{}, nil
}

func (c *cloud) Create(context.Context, provider.CreateRequest) (provider.CreateRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.made++
	return provider.CreateRef{Op: fmt.Sprintf("op-%d", c.made)}, nil
}

func (c *cloud) Attach(_ context.Context, ref string, _ provider.AttachRequest) (provider.MachineState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.attached = append(c.attached, ref)
	return provider.MachineState{ID: ref}, nil
}

func (c *cloud) State(_ context.Context, ref string) (provider.MachineState, error) {
	return provider.MachineState{ID: ref}, nil
}

func (c *cloud) Reboot(_ context.Context, m provider.Machine) (provider.OpRef, error) {
	return provider.OpRef{ID: "reboot-" + m.ExternalID}, nil
}

// Op answers a finished create with the server it made: op-N made srv-N.
func (c *cloud) Op(_ context.Context, ref provider.OpRef) (provider.OpStatus, error) {
	st := provider.OpStatus{State: provider.StateDone}
	var n int
	if _, err := fmt.Sscanf(ref.ID, "op-%d", &n); err == nil {
		st.Machine = fmt.Sprintf("srv-%d", n)
	}
	return st, nil
}

// A machine is tied only to a server its own space made, a server to one machine, and an operation is read only by
// the space that started it. An administrator of the installation takes over a server the hub did not make.
func TestProviderServersStayWithTheirSpace(t *testing.T) {
	c := &cloud{}
	s := hubtest.New(t, hubtest.Env("HUB_PROVIDER_PROTOCOL", "v1"), hubtest.WithOptions(func(o *api.Options) { o.Provider = c }))
	root := s.SignIn("root@example.com") // the first account administers the installation
	north, south := s.SignIn("north@example.com"), s.SignIn("south@example.com")
	northID, _ := north.NewSpace("North")
	southID, _ := south.NewSpace("South")
	nm, sm := north.Machine(northID, nil), south.Machine(southID, nil)

	var made struct{ Op string }
	if r := north.Do("POST", "/api/spaces/"+northID+"/machines/provision", map[string]any{"name": "box"}); r.Code != 202 {
		t.Fatalf("provision: %s", r)
	} else {
		r.JSON(&made)
	}
	// The operation is the space's own.
	if r := south.Do("GET", "/api/spaces/"+southID+"/machines/provision/"+made.Op, nil); r.Code != 404 {
		t.Errorf("another space reads the operation: %s", r)
	}
	if r := south.Do("GET", "/api/spaces/"+northID+"/machines/provision/"+made.Op, nil); r.Code == 200 {
		t.Errorf("a stranger reads the operation of the space: %s", r)
	}
	if r := north.Do("GET", "/api/spaces/"+northID+"/machines/provision/"+made.Op, nil); r.Code != 200 {
		t.Fatalf("the space reads its operation: %s", r)
	}
	attach := func(p *hubtest.Person, machine, ext string) hubtest.Resp {
		return p.Do("POST", "/api/machines/"+machine+"/provider/attach", map[string]any{"externalId": ext})
	}
	// The server of another space, and one nobody made, are not there to tie to; the provider is not asked.
	for _, ext := range []string{"srv-1", "srv-77"} {
		if r := attach(south, sm.MachineID, ext); r.Code != 404 {
			t.Errorf("another space ties its machine to %s: %s", ext, r)
		}
	}
	if len(c.attached) != 0 {
		t.Errorf("the provider was asked to attach %v", c.attached)
	}
	if r := attach(north, nm.MachineID, "srv-1"); r.Code != 200 {
		t.Fatalf("the space ties its machine to its server: %s", r)
	}
	// One machine per server.
	nm2 := north.Machine(northID, nil)
	if r := attach(north, nm2.MachineID, "srv-1"); r.Code != 409 {
		t.Errorf("a second machine on the same server: %s", r)
	}
	// An operation on a machine is read through that machine only.
	var power struct{ Op string }
	if r := north.Do("POST", "/api/machines/"+nm.MachineID+"/provider/power", map[string]any{"action": "reboot"}); r.Code != 202 {
		t.Fatalf("power: %s", r)
	} else {
		r.JSON(&power)
	}
	if r := north.Do("GET", "/api/machines/"+nm.MachineID+"/provider/ops/"+power.Op, nil); r.Code != 200 {
		t.Errorf("the machine's operation: %s", r)
	}
	// An administrator takes over a server the hub did not make, for the space of their machine; then it is that
	// space's and nobody else's.
	rootID, _ := root.NewSpace("Root")
	rm := root.Machine(rootID, nil)
	if r := attach(root, rm.MachineID, "srv-77"); r.Code != 200 {
		t.Errorf("an administrator takes a server over: %s", r)
	}
	if r := attach(south, sm.MachineID, "srv-77"); r.Code != 404 {
		t.Errorf("a server taken over by another space: %s", r)
	}
	if r := south.Do("GET", "/api/machines/"+sm.MachineID+"/provider/ops/"+power.Op, nil); r.Code == 200 {
		t.Errorf("an operation of another space's machine is read: %s", r)
	}
	if r := south.Do("GET", "/api/machines/"+sm.MachineID+"/provider/ops/"+made.Op, nil); r.Code == 200 {
		t.Errorf("the creation of another space's server is read through a machine: %s", r)
	}
}

// A space asks the provider for no more servers than the hub's setting; the provider is not asked for the one over.
func TestServerCapIsConfigured(t *testing.T) {
	c := &cloud{}
	s := hubtest.New(t, hubtest.Env("HUB_PROVIDER_PROTOCOL", "v1"), hubtest.Env("HUB_MAX_SERVERS_PER_SPACE", "1"),
		hubtest.WithOptions(func(o *api.Options) { o.Provider = c }))
	north, south := s.SignIn("north@example.com"), s.SignIn("south@example.com")
	northID, _ := north.NewSpace("North")
	southID, _ := south.NewSpace("South")
	if r := north.Do("POST", "/api/spaces/"+northID+"/machines/provision", map[string]any{"name": "one"}); r.Code != 202 {
		t.Fatalf("the first server: %s", r)
	}
	if r := north.Do("POST", "/api/spaces/"+northID+"/machines/provision", map[string]any{"name": "two"}); r.Code != 409 || !strings.Contains(string(r.Body), "server_limit") {
		t.Errorf("a server over the cap: %s", r)
	}
	if r := south.Do("POST", "/api/spaces/"+southID+"/machines/provision", map[string]any{"name": "one"}); r.Code != 202 {
		t.Errorf("another space's first server: %s", r)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.made != 2 {
		t.Errorf("the provider made %d servers, want 2", c.made)
	}
}
