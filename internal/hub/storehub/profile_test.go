package storehub

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type machinesAndUsers struct {
	fakeMachines
	emails map[string]string
}

func (m machinesAndUsers) UserByID(_ context.Context, id string) (store.User, error) {
	e, ok := m.emails[id]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return store.User{ID: id, Email: e}, nil
}

// a person's Claude profile is one room for all their machines, whatever space each is in; design-sync's room
// the configuration names for them is kept; nobody else's machine opens it; secrets never go in.
func TestAPersonsProfileIsOneRoomOfTheirMachinesOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	meta, err := filestore.OpenMetaStore(ctx, "sqlite://"+dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { meta.Close() })
	blobs, err := filestore.NewFileBlobs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	db := machinesAndUsers{fakeMachines: fakeMachines{
		"vm-a":  {ID: "vm-a", Name: "vm-a", SpaceID: "sp1", CreatedBy: "usergey"},
		"vm-b":  {ID: "vm-b", Name: "vm-b", SpaceID: "sp2", CreatedBy: "usergey"},
		"vm-r":  {ID: "vm-r", Name: "vm-r", SpaceID: "sp1", CreatedBy: "uroma"},
		"vm-no": {ID: "vm-no", Name: "vm-no", SpaceID: "sp1"},
	}, emails: map[string]string{"usergey": "Sergey@Example.org", "uroma": "roma@example.org"}}
	cfg := &config.Config{}
	cfg.Store.ProfileRooms = map[string]string{"usergey": "profile-sergey"}
	next := ""
	st, err := Open(ctx, Options{Config: cfg, DB: db, Key: []byte("0123456789abcdef0123456789abcdef"), Meta: meta, Blobs: blobs,
		Admit: func(*http.Request) (store.Machine, error) { return db.fakeMachines[next], nil },
		Now:   func() time.Time { return time.Now() }})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	st.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := func(machine string) *filesync.Client {
		next = machine
		resp, err := http.Post(srv.URL+hubproto.StorePath, "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var s hubproto.StoreSession
		if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
			t.Fatal(err)
		}
		tok := s.Token
		return filesync.NewClient(srv.URL+hubproto.StoreAPIPath, filestore.KindProfile,
			func(context.Context, bool) (string, error) { return tok, nil }, nil)
	}
	a, b, roma, nobody := client("vm-a"), client("vm-b"), client("vm-r"), client("vm-no")

	key := bytes.Repeat([]byte{9}, filesync.RoomKeySize)
	// Two machines of the person sync their profiles through the room: sealed in the store, the token left behind.
	sync := func(c *filesync.Client, home string) {
		t.Helper()
		codec := &filesync.ProfileCodec{Home: home, LocalKeys: filesync.DefaultLocalKeys}
		codec.SetKey(key)
		ag, err := filesync.New(filesync.Config{Root: home, StateDir: filepath.Join(home, ".state"), Store: c, Machine: "m",
			Policy: filestore.Profile{}, Codec: codec, TmpDir: ".local/state/profile-sync/tmp",
			ConflictDir: ".local/state/profile-sync/conflicts"})
		if err != nil {
			t.Fatal(err)
		}
		defer ag.Close()
		if err := ag.SyncOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	homeA, homeB := t.TempDir(), t.TempDir()
	write := func(home, rel, body string) {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(home, rel)), 0o755)
		if err := os.WriteFile(filepath.Join(home, rel), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(homeA, ".claude/CLAUDE.md", "# mine\n")
	sync(a, homeA)
	if e, err := b.Current(ctx, ".claude/CLAUDE.md"); err != nil || e == nil {
		t.Fatalf("the person's other machine does not see it: %+v %v", e, err)
	}
	if e, err := roma.Current(ctx, ".claude/CLAUDE.md"); err != nil || e != nil {
		t.Fatalf("another person's machine sees it: %+v %v", e, err)
	}
	if _, err := roma.Put(ctx, ".claude/CLAUDE.md", []byte("roma's"), "t"); err != nil {
		t.Fatal(err)
	}
	if _, err := nobody.Current(ctx, ".claude/CLAUDE.md"); err == nil {
		t.Fatal("a machine nobody made opened a profile")
	}
	if _, err := a.Put(ctx, ".gitconfig", []byte("[credential]"), "t"); err == nil {
		t.Fatal("a secret went into the profile")
	}
	rooms, err := st.Server().Meta().Rooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, r := range rooms {
		has[r] = true
	}
	if !has["profile-sergey"] || !has["profile-uroma"] {
		t.Fatalf("rooms = %v", rooms)
	}

	// design-sync's machines left the room key wrapped for each of them: the person's machines read them.
	rs, err := st.Server().Room(ctx, "profile-sergey")
	if err != nil {
		t.Fatal(err)
	}
	if err := rs.RegisterMember(ctx, "sergey-vm/sergey", "age1recipient"); err != nil {
		t.Fatal(err)
	}
	if err := rs.PutKey(ctx, "sergey-vm/sergey", "-----BEGIN AGE ENCRYPTED FILE-----", "sergey-vm/sergey"); err != nil {
		t.Fatal(err)
	}
	if keys, err := b.Keys(ctx); err != nil || len(keys) != 1 || keys[0].Member != "sergey-vm/sergey" {
		t.Fatalf("keys = %+v %v", keys, err)
	}
	if keys, err := roma.Keys(ctx); err != nil || len(keys) != 0 {
		t.Fatalf("roma's room keys = %+v %v", keys, err)
	}

	write(homeA, ".claude/settings.json", `{"model":"opus","env":{"GITLAB_TOKEN":"glpat-secret","EDITOR":"vim"}}`)
	write(homeA, ".claude/rules/style.md", "plain rules of the person")
	write(homeA, ".gitconfig", "[credential]\n\thelper = store")
	sync(client("vm-a"), homeA)
	sync(client("vm-b"), homeB)
	got, err := os.ReadFile(filepath.Join(homeB, ".claude", "settings.json"))
	if err != nil || !strings.Contains(string(got), `"opus"`) || strings.Contains(string(got), "glpat") {
		t.Fatalf("the other machine's settings: %s %v", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(homeB, ".claude", "rules", "style.md")); string(b) != "plain rules of the person" {
		t.Fatalf("rules = %q", b)
	}
	if _, err := os.Stat(filepath.Join(homeB, ".gitconfig")); err == nil {
		t.Fatal(".gitconfig came down")
	}
	e, err := b.Current(ctx, ".claude/rules/style.md")
	if err != nil || e == nil {
		t.Fatalf("current = %+v %v", e, err)
	}
	var blob bytes.Buffer
	if err := b.GetBlob(ctx, e.Sha, &blob); err != nil || blob.Len() == 0 || bytes.Contains(blob.Bytes(), []byte("plain rules")) {
		t.Fatalf("the store holds the profile in clear: %q %v", blob.String(), err)
	}
}
