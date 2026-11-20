package apps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// fakeRoom is the person's room of the hub's store in memory.
type fakeRoom struct {
	mu    sync.Mutex
	rev   int64
	files map[string]filestore.Entry
	blobs map[string][]byte
	puts  []string
	clock int64
}

func newFakeRoom() *fakeRoom {
	return &fakeRoom{files: map[string]filestore.Entry{}, blobs: map[string][]byte{}}
}

func (f *fakeRoom) Changes(_ context.Context, since int64, _ time.Duration) (filestore.Changes, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := filestore.Changes{Rev: f.rev, Entries: []filestore.Entry{}}
	for _, e := range f.files {
		if e.Rev > since {
			out.Entries = append(out.Entries, e)
		}
	}
	return out, nil
}

func (f *fakeRoom) Current(_ context.Context, path string) (*filestore.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.files[path]; ok {
		return &e, nil
	}
	return nil, nil
}

func (f *fakeRoom) GetBlob(_ context.Context, sha string, w io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blobs[sha]
	if !ok {
		return os.ErrNotExist
	}
	_, err := w.Write(b)
	return err
}

func (f *fakeRoom) Put(_ context.Context, path string, data []byte, author string) (filestore.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	f.blobs[sha] = slices.Clone(data)
	f.rev++
	e := filestore.Entry{Path: path, Rev: f.rev, Sha: sha, Size: int64(len(data)), Author: author, Machine: author}
	f.files[path] = e
	f.puts = append(f.puts, path)
	return e, nil
}

// vm is one machine of the person: a registry, a storage and a pass of the sync.
type vm struct {
	t   *testing.T
	set *AppSet
	reg *Registry
	st  *Storage
	now *int64
}

func newVM(t *testing.T, name string, room SetRoom) *vm {
	t.Helper()
	data := t.TempDir()
	reg := &Registry{Root: filepath.Join(data, "apps")}
	room.(*fakeRoom).mu.Lock()
	clock := &room.(*fakeRoom).clock
	room.(*fakeRoom).mu.Unlock()
	if *clock == 0 {
		*clock = 1_000_000
	}
	v := &vm{t: t, reg: reg, st: OpenStorage(data), now: clock}
	v.set = &AppSet{Reg: reg, Storage: v.st, Room: room, Machine: name, Host: "1.0.0", CarryConsent: true,
		Now: func() time.Time { *clock += 1000; return time.UnixMilli(*clock) }}
	return v
}

func (v *vm) pass() SetReport {
	v.t.Helper()
	rep, err := v.set.Reconcile(context.Background())
	if err != nil {
		v.t.Fatal(err)
	}
	if len(rep.Problems) > 0 {
		v.t.Fatalf("problems: %v", rep.Problems)
	}
	return rep
}

func (v *vm) has(id string) (Installed, bool) {
	list, err := v.reg.userInstalls()
	if err != nil {
		v.t.Fatal(err)
	}
	for _, a := range list {
		if a.ID == id {
			return a, true
		}
	}
	return Installed{}, false
}

// appFolder is a package with one setting, at a version.
func appFolder(t *testing.T, id, version string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "pkg")
	if err := Scaffold(dir, id, "", "chat"); err != nil {
		t.Fatal(err)
	}
	editManifest(t, dir, version)
	return dir
}

func editManifest(t *testing.T, dir, version string) {
	t.Helper()
	path := filepath.Join(dir, ManifestPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["version"] = version
	m["permissions"] = []string{"storage"}
	m["settings"] = json.RawMessage(`{"type":"object","properties":{"mode":{"type":"string","enum":["fast","slow"],"default":"fast"}}}`)
	out, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (v *vm) install(dir string, opt InstallOptions) Installed {
	v.t.Helper()
	opt.HostVersion = "1.0.0"
	got, _, err := v.reg.Install(dir, opt)
	if err != nil {
		v.t.Fatal(err)
	}
	return got
}

func TestAppSetTwoVMs(t *testing.T) {
	SetPermissions(true)
	room := newFakeRoom()
	a, b := newVM(t, "vm-a", room), newVM(t, "vm-b", room)
	dir := appFolder(t, "acme.inbox", "0.8.11")

	// An install from a local folder on A is on B after both have passed, with the folder gone from B's view.
	a.install(dir, InstallOptions{Consent: ConsentGiven})
	if rep := a.pass(); !slices.Equal(rep.Published, []string{"acme.inbox"}) {
		t.Fatalf("A published %+v", rep)
	}
	if !slices.Contains(room.puts, SetArchivePath(mustSum(t, a, "acme.inbox"))) {
		t.Fatalf("the package is not in the room: %v", room.puts)
	}
	rep := b.pass()
	if !slices.Equal(rep.Installed, []string{"acme.inbox"}) {
		t.Fatalf("B installed %+v", rep)
	}
	got, ok := b.has("acme.inbox")
	if !ok || got.Version != "0.8.11" || got.Dev || got.Off {
		t.Fatalf("B has %+v", got)
	}
	if _, err := os.Stat(filepath.Join(got.Dir, ManifestPath)); err != nil {
		t.Fatal(err)
	}
	// The person's click on A carries over: the app is not waiting on B.
	if got.WaitsConsent() {
		t.Errorf("B's app waits for consent that A has: %+v", got.Grant)
	}
	// Nothing more to do on either side, and nothing is written again.
	puts := len(room.puts)
	if r := a.pass(); !r.Empty() {
		t.Errorf("A again: %+v", r)
	}
	if r := b.pass(); !r.Empty() {
		t.Errorf("B again: %+v", r)
	}
	if len(room.puts) != puts {
		t.Errorf("a quiet pass wrote %v", room.puts[puts:])
	}

	// Switching off and a setting made on B reach A.
	if err := b.reg.SetDisabled("acme.inbox", true); err != nil {
		t.Fatal(err)
	}
	if _, err := b.st.SaveSettings(got.Manifest, "acme.inbox", map[string]any{"mode": "slow"}); err != nil {
		t.Fatal(err)
	}
	if r := b.pass(); !slices.Equal(r.Published, []string{"acme.inbox"}) {
		t.Fatalf("B published %+v", r)
	}
	if r := a.pass(); !slices.Equal(r.Changed, []string{"acme.inbox"}) {
		t.Fatalf("A changed %+v", r)
	}
	if x, _ := a.has("acme.inbox"); !x.Off {
		t.Error("A did not switch it off")
	}
	vals, err := a.st.Settings(got.Manifest, "acme.inbox")
	if err != nil || vals["mode"] != "slow" {
		t.Errorf("A's settings %v %v", vals, err)
	}

	// An update on A (a new version of the folder) reaches B.
	editManifest(t, dir, "0.9.0")
	a.install(dir, InstallOptions{Consent: ConsentGiven})
	a.pass()
	if r := b.pass(); !slices.Equal(r.Updated, []string{"acme.inbox"}) {
		t.Fatalf("B updated %+v", r)
	}
	if x, _ := b.has("acme.inbox"); x.Version != "0.9.0" || !x.Off {
		t.Errorf("B after the update: %+v", x)
	}
	if r := a.pass(); !r.Empty() {
		t.Errorf("A after B's pass: %+v", r)
	}

	// A removal on A takes the app off B; the room keeps the tombstone so a VM that comes later does not bring it back.
	if err := a.reg.Remove("acme.inbox"); err != nil {
		t.Fatal(err)
	}
	if r := a.pass(); !slices.Equal(r.Published, []string{"acme.inbox"}) {
		t.Fatalf("A's removal %+v", r)
	}
	if r := b.pass(); !slices.Equal(r.Removed, []string{"acme.inbox"}) {
		t.Fatalf("B removed %+v", r)
	}
	if _, ok := b.has("acme.inbox"); ok {
		t.Error("B still has the app")
	}
	c := newVM(t, "vm-c", room)
	if r := c.pass(); !r.Empty() {
		t.Errorf("a new VM: %+v", r)
	}
	if _, ok := c.has("acme.inbox"); ok {
		t.Error("a new VM got a removed app")
	}
}

func mustSum(t *testing.T, v *vm, id string) string {
	t.Helper()
	a, ok := v.has(id)
	if !ok {
		t.Fatal("not installed")
	}
	sum, err := v.set.sumOf(a)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func TestAppSetDevStaysOnItsVM(t *testing.T) {
	room := newFakeRoom()
	a, b := newVM(t, "vm-a", room), newVM(t, "vm-b", room)
	a.install(appFolder(t, "acme.live", "1.0.0"), InstallOptions{Dev: true, Consent: ConsentGiven})
	rep := a.pass()
	if len(rep.Published) != 0 || !slices.Equal(rep.LocalOnly, []string{"acme.live"}) {
		t.Fatalf("A: %+v", rep)
	}
	if len(room.puts) != 0 {
		t.Fatalf("a dev install was put into the room: %v", room.puts)
	}
	if rep := b.pass(); !rep.Empty() {
		t.Fatalf("B: %+v", rep)
	}
}

func TestAppSetNewVMCatchesUpAndKeepsItsOwn(t *testing.T) {
	SetPermissions(true)
	room := newFakeRoom()
	a, b := newVM(t, "vm-a", room), newVM(t, "vm-b", room)
	a.install(appFolder(t, "acme.one", "1.0.0"), InstallOptions{Consent: ConsentGiven})
	b.install(appFolder(t, "acme.two", "1.0.0"), InstallOptions{Consent: ConsentGiven})
	a.pass()
	b.pass() // B brings acme.two to the room and gets acme.one
	a.pass()
	for _, v := range []*vm{a, b} {
		for _, id := range []string{"acme.one", "acme.two"} {
			if _, ok := v.has(id); !ok {
				t.Errorf("%s lacks %s", v.set.Machine, id)
			}
		}
	}
}

func TestAppSetLaterChangeWins(t *testing.T) {
	room := newFakeRoom()
	a, b := newVM(t, "vm-a", room), newVM(t, "vm-b", room)
	a.install(appFolder(t, "acme.inbox", "1.0.0"), InstallOptions{Consent: ConsentGiven})
	a.pass()
	b.pass()
	// Both switch it differently while apart; B was later.
	if err := a.reg.SetDisabled("acme.inbox", true); err != nil {
		t.Fatal(err)
	}
	a.pass()
	got, _ := b.has("acme.inbox")
	if _, err := b.st.SaveSettings(got.Manifest, "acme.inbox", map[string]any{"mode": "slow"}); err != nil {
		t.Fatal(err)
	}
	b.pass() // sees A's record changed and its own settings changed: B's change is later, so it publishes its own
	a.pass()
	b.pass()
	for _, v := range []*vm{a, b} {
		if r := v.pass(); !r.Empty() {
			t.Errorf("%s still moves: %+v", v.set.Machine, r)
		}
	}
	ra, _ := a.set.localRecord(mustGet(t, a))
	rb, _ := b.set.localRecord(mustGet(t, b))
	if ra.hash() != rb.hash() {
		t.Errorf("the VMs differ: %+v / %+v", ra, rb)
	}
}

func mustGet(t *testing.T, v *vm) Installed {
	t.Helper()
	a, ok := v.has("acme.inbox")
	if !ok {
		t.Fatal("not installed")
	}
	return a
}

func TestAppSetRefusesWhatIsNotTheRecord(t *testing.T) {
	room := newFakeRoom()
	b := newVM(t, "vm-b", room)
	// A package that is not the one the record names is not installed.
	dir := appFolder(t, "acme.inbox", "1.0.0")
	raw, sum, err := Pack(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := room.Put(context.Background(), SetArchivePath(strings.Repeat("0", 64)), raw, "x"); err != nil {
		t.Fatal(err)
	}
	rec, _ := json.Marshal(SetRecord{V: 1, ID: "acme.inbox", Version: "1.0.0", Sum: strings.Repeat("0", 64), At: 5})
	if _, err := room.Put(context.Background(), SetPath("acme.inbox"), rec, "x"); err != nil {
		t.Fatal(err)
	}
	rep, err := b.set.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.has("acme.inbox"); ok || len(rep.Problems) != 1 {
		t.Fatalf("a package of another sum was installed: %+v", rep)
	}
	_ = sum
	_ = bytes.MinRead
}

func TestAppSetConsentCarriesOnlyWhenAsked(t *testing.T) {
	SetPermissions(true)
	for _, carry := range []bool{true, false} {
		room := newFakeRoom()
		a, b := newVM(t, "vm-a", room), newVM(t, "vm-b", room)
		b.set.CarryConsent = carry
		got := a.install(appFolder(t, "acme.inbox", "1.0.0"), InstallOptions{Consent: ConsentGiven})
		if len(got.Wants) == 0 || got.WaitsConsent() {
			t.Fatalf("the fixture must reach something and be agreed to on A: %+v", got)
		}
		a.pass()
		b.pass()
		on, _ := b.has("acme.inbox")
		if carry == on.WaitsConsent() {
			t.Errorf("carry=%v: B waits=%v, grant %+v wants %v", carry, on.WaitsConsent(), on.Grant, on.Wants)
		}
	}
}
