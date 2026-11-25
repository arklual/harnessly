package connections

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const syncKind Kind = "sync-fake"

type syncProvider struct{}

func (syncProvider) Kind() Kind { return syncKind }
func (syncProvider) Detect(context.Context, *Prober, *url.URL) (*Detection, error) {
	return nil, nil
}
func (syncProvider) Open(_ Conn, cred Credentials, _ *HTTP) Client {
	return syncClient{cred[FieldToken]}
}
func (syncProvider) Link(Conn, *url.URL) (LinkTarget, bool) { return LinkTarget{}, false }

type syncClient struct{ token string }

func (c syncClient) Me(context.Context) (*Person, error) {
	if !strings.HasPrefix(c.token, "tok-") {
		return nil, ErrDenied
	}
	return &Person{ID: "u1", Login: "ann"}, nil
}

func init() { Register(syncProvider{}) }

// person is what the hub keeps for one person: their folder of the safe (the credentials and the record).
type person struct {
	mu      sync.Mutex
	record  string
	secrets map[string]string
	down    bool
	denied  bool
	reads   int
}

func newPerson() *person { return &person{secrets: map[string]string{}} }

func (p *person) ReadIndex() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads++
	if p.denied {
		return "", fmt.Errorf("%w: the switch is off", ErrDenied)
	}
	if p.down {
		return "", errors.New("the hub is away")
	}
	return p.record, nil
}

func (p *person) WriteIndex(v string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.down || p.denied {
		return errors.New("the hub is away")
	}
	p.record = v
	return nil
}

func (p *person) Put(name, value string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.secrets[name] = value
	return nil
}

func (p *person) Reveal(name string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.secrets[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (p *person) Remove(name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.secrets, name)
	return nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) tick()          { c.t = c.t.Add(time.Second) }

// machine is a runtime of the person: its own folder, the person's safe and record, syncs at once.
func machine(t *testing.T, p *person, clk *clock) *Service {
	t.Helper()
	s := New(t.TempDir(), p, nil)
	s.now, s.kick = clk.now, func(f func()) { f() }
	s.SetShared(p)
	return s
}

func connectSync(t *testing.T, s *Service, clk *clock, token, name string) Conn {
	t.Helper()
	clk.tick()
	d := Detection{Kind: syncKind, BaseURL: "https://tracker.example", Deployment: Cloud,
		Methods: []Method{{ID: "pat", Fields: []Field{{ID: FieldEmail}, {ID: FieldToken, Secret: true}}}}}
	c, err := s.Connect(context.Background(), ConnectInput{Detection: d, Method: "pat", Name: name,
		Cred: Credentials{FieldEmail: "ann@example.com", FieldToken: token}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func ids(t *testing.T, s *Service) []string {
	t.Helper()
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range list {
		out = append(out, c.ID+":"+c.Name)
	}
	return out
}

func tokenOf(t *testing.T, s *Service, id string) string {
	t.Helper()
	c, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := s.credentials(c)
	if err != nil {
		return ""
	}
	return cred[FieldToken]
}

func TestSharedConnectionsReachTheSecondMachine(t *testing.T) {
	clk := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	ann := newPerson()
	first, second := machine(t, ann, clk), machine(t, ann, clk)

	// First use: the second machine, with no list of its own, has the connection the first made, and its token.
	c := connectSync(t, first, clk, "tok-1", "Tracker")
	if got := ids(t, second); len(got) != 1 || got[0] != c.ID+":Tracker" {
		t.Fatalf("second machine: %v", got)
	}
	if tok := tokenOf(t, second, c.ID); tok != "tok-1" {
		t.Fatalf("second machine reads the token %q", tok)
	}
	if strings.Contains(ann.record, "tok-1") {
		t.Fatal("the shared record holds a credential")
	}

	// A rename on the second machine reaches the first.
	clk.tick()
	if _, err := second.Rename(c.ID, "Work tracker"); err != nil {
		t.Fatal(err)
	}
	if err := first.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, first); len(got) != 1 || got[0] != c.ID+":Work tracker" {
		t.Fatalf("rename on the first machine: %v", got)
	}

	// Provider rotation: connecting again with a new token keeps the connection and both machines use the new token.
	again := connectSync(t, first, clk, "tok-2", "")
	if again.ID != c.ID {
		t.Fatalf("a reconnect made a new connection %s", again.ID)
	}
	if err := second.Sync(); err != nil {
		t.Fatal(err)
	}
	if tok := tokenOf(t, second, c.ID); tok != "tok-2" {
		t.Fatalf("after the rotation the second machine reads %q", tok)
	}

	// Revocation: removed on the second machine, it leaves the first and its credentials leave the safe.
	clk.tick()
	if err := second.Remove(c.ID); err != nil {
		t.Fatal(err)
	}
	if err := first.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, first); len(got) != 0 {
		t.Fatalf("removed on the second machine, the first still has %v", got)
	}
	if _, ok := ann.secrets[secretName(c.ID)]; ok {
		t.Fatal("the removed connection's credentials stay in the safe")
	}
}

func TestSharedConnectionsWaitOutADisconnectedHub(t *testing.T) {
	clk := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	ann := newPerson()
	first, second := machine(t, ann, clk), machine(t, ann, clk)
	kept := connectSync(t, first, clk, "tok-1", "Kept")
	if err := second.Sync(); err != nil {
		t.Fatal(err)
	}

	// The hub goes away: each machine keeps its list, and what is made meanwhile stays on its machine.
	ann.down = true
	clk.tick()
	if _, err := first.Rename(kept.ID, "Kept offline"); err != nil {
		t.Fatal(err)
	}
	if err := second.Sync(); err == nil {
		t.Fatal("a sync with the hub away answered no error")
	}
	if got := ids(t, second); len(got) != 1 || got[0] != kept.ID+":Kept" {
		t.Fatalf("with the hub away the second machine has %v", got)
	}

	// Back: the next sync of the first machine gives the record what it changed, and the second takes it.
	ann.down = false
	if err := first.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := second.Sync(); err != nil {
		t.Fatal(err)
	}
	if got := ids(t, second); len(got) != 1 || got[0] != kept.ID+":Kept offline" {
		t.Fatalf("after the reconnect the second machine has %v", got)
	}
}

func TestSharedConnectionsKeepOneOfTheSameAccount(t *testing.T) {
	clk := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	ann := newPerson()
	first, second := machine(t, ann, clk), machine(t, ann, clk)
	ann.down = true
	old := connectSync(t, first, clk, "tok-1", "")
	newer := connectSync(t, second, clk, "tok-2", "")
	ann.down = false
	for _, s := range []*Service{first, second, first} {
		if err := s.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []*Service{first, second} {
		if got := ids(t, s); len(got) != 1 || !strings.HasPrefix(got[0], newer.ID+":") {
			t.Fatalf("the same account twice: %v", got)
		}
	}
	if _, ok := ann.secrets[secretName(old.ID)]; ok {
		t.Fatal("the replaced duplicate's credentials stay in the safe")
	}
}

func TestSharedConnectionsStayWithTheirPerson(t *testing.T) {
	clk := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	ann, bob := newPerson(), newPerson()
	connectSync(t, machine(t, ann, clk), clk, "tok-1", "Ann's")
	// A machine of another person reads that person's folder: Ann's connection and token are not there.
	if got := ids(t, machine(t, bob, clk)); len(got) != 0 {
		t.Fatalf("another person's machine has %v", got)
	}
	if len(bob.secrets) != 0 || bob.record != "" {
		t.Fatal("another person's folder got something")
	}
}

func TestSharedConnectionsRefusedByTheHubStayLocal(t *testing.T) {
	clk := &clock{t: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}
	ann := newPerson()
	ann.denied = true
	s := machine(t, ann, clk)
	c := connectSync(t, s, clk, "tok-1", "Local")
	if got := ids(t, s); len(got) != 1 || got[0] != c.ID+":Local" {
		t.Fatalf("with the switch off: %v", got)
	}
	reads := ann.reads
	// While refused, neither the list nor a change asks the hub again until deniedWait passes.
	clk.tick()
	_, _ = s.Rename(c.ID, "Again")
	clk.t = clk.t.Add(pullEvery)
	ids(t, s)
	if ann.reads != reads {
		t.Fatalf("the hub was asked %d more times while it refused", ann.reads-reads)
	}
	clk.t = clk.t.Add(deniedWait)
	ids(t, s)
	if ann.reads != reads+1 {
		t.Fatalf("after deniedWait the hub was asked %d times", ann.reads-reads)
	}
}

func TestMergeRemovalsAgainstLaterChanges(t *testing.T) {
	now := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	c := Conn{ID: "c1", Kind: syncKind, BaseURL: "https://x", UpdatedAt: now.Add(-time.Hour)}
	// Removed after its last change: gone.
	if list, _, _ := merge([]Conn{c}, nil, nil, map[string]time.Time{"c1": now.Add(-time.Minute)}, now); len(list) != 0 {
		t.Fatalf("removed later, kept: %v", list)
	}
	// Changed after the removal (connected again on a machine that had it): stays.
	c.UpdatedAt = now
	if list, _, _ := merge([]Conn{c}, nil, nil, map[string]time.Time{"c1": now.Add(-time.Minute)}, now); len(list) != 1 {
		t.Fatal("changed after the removal, dropped")
	}
	// A removal older than removedKeep is forgotten.
	if _, removed, _ := merge(nil, map[string]time.Time{"c2": now.Add(-removedKeep - time.Hour)}, nil, nil, now); removed != nil {
		t.Fatalf("an old removal is kept: %v", removed)
	}
}
