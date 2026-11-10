package profilesync

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"filippo.io/age"
	"filippo.io/age/armor"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

type fakeSafe map[string]map[string]string

func (f fakeSafe) Read(_ context.Context, p string) (map[string]string, error) {
	d, ok := f[p]
	if !ok {
		return nil, &secrets.Refusal{Reason: secrets.ReasonNotFound, Err: errors.New("no such secret")}
	}
	return d, nil
}

func (f fakeSafe) Write(_ context.Context, p string, d map[string]string) error {
	f[p] = d
	return nil
}

type fakeRoom struct {
	keys    []filestore.WrappedKey
	entries []filestore.Entry
}

func (r fakeRoom) Keys(context.Context) ([]filestore.WrappedKey, error) { return r.keys, nil }
func (r fakeRoom) Changes(context.Context, int64, time.Duration) (filestore.Changes, error) {
	return filestore.Changes{Entries: r.entries}, nil
}

func wrapFor(t *testing.T, key []byte, id *age.X25519Identity) string {
	t.Helper()
	var buf bytes.Buffer
	aw := armor.NewWriter(&buf)
	w, err := age.Encrypt(aw, id.Recipient())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(key)
	_ = w.Close()
	_ = aw.Close()
	return buf.String()
}

// the room key is the safe's; a machine with design-sync's identity puts the one wrapped for it there; a room
// nobody keyed gets a new one; anything else waits.
func TestTheRoomKeyComesFromTheSafeOrFromDesignSync(t *testing.T) {
	ctx := context.Background()
	key := bytes.Repeat([]byte{5}, 32)
	home := t.TempDir()
	id, _ := age.GenerateX25519Identity()
	other, _ := age.GenerateX25519Identity()
	state := filepath.Join(home, ".local", "state", "profile-sync")
	file := filepath.Join(state, "agent-sergey", "identity.age")
	_ = os.MkdirAll(filepath.Dir(file), 0o700)
	if err := os.WriteFile(file, []byte(id.String()+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ids := Identities(state)
	if len(ids) != 1 {
		t.Fatalf("identities = %v", ids)
	}
	room := fakeRoom{keys: []filestore.WrappedKey{{Member: "vm-2/sergey", Wrapped: wrapFor(t, key, other)},
		{Member: "sergey-vm/sergey", Wrapped: wrapFor(t, key, id)}}, entries: []filestore.Entry{{Path: ".claude/CLAUDE.md"}}}

	safe := fakeSafe{}
	got, err := Key(ctx, safe, "me", room, ids)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("key = %x %v", got, err)
	}
	if v := safe["me/"+KeySecret][secrets.ValueKey]; v != base64.StdEncoding.EncodeToString(key) {
		t.Fatalf("the safe keeps %q", v)
	}
	// Another machine of the person, without design-sync: the safe's.
	if got, err := Key(ctx, safe, "me", room, nil); err != nil || !bytes.Equal(got, key) {
		t.Fatalf("from the safe: %x %v", got, err)
	}
	// A keyed room, nothing here opens it: wait.
	if _, err := Key(ctx, fakeSafe{}, "me", room, nil); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
	// A room with files and no key anywhere: wait as well, a new key would not open them.
	if _, err := Key(ctx, fakeSafe{}, "me", fakeRoom{entries: room.entries}, nil); !errors.Is(err, ErrNoKey) {
		t.Fatalf("err = %v", err)
	}
	// An empty room nobody keyed: a new key, kept in the safe.
	fresh := fakeSafe{}
	if k, err := Key(ctx, fresh, "me", fakeRoom{}, nil); err != nil || len(k) != 32 || fresh["me/"+KeySecret] == nil {
		t.Fatalf("new key = %x %v", k, err)
	}
}
