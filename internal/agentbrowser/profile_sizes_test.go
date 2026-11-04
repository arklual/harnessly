package agentbrowser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestProfileDirectorySizeBoundaries(t *testing.T) {
	data := t.TempDir()
	chrome := filepath.Join(data, "agent-browser", "profiles", "managed", "chrome")
	must(t, os.MkdirAll(filepath.Join(chrome, "Default"), 0700))
	must(t, os.WriteFile(filepath.Join(chrome, "Local State"), []byte("123"), 0600))
	must(t, os.WriteFile(filepath.Join(chrome, "Default", "Cookies"), []byte("12345"), 0600))
	must(t, os.WriteFile(filepath.Join(filepath.Dir(chrome), "tabs.json"), []byte("not Chrome data"), 0600))
	outside := t.TempDir()
	must(t, os.WriteFile(filepath.Join(outside, "large"), make([]byte, 1024), 0600))
	must(t, os.Symlink(outside, filepath.Join(chrome, "linked-dir")))
	must(t, os.Symlink(filepath.Join(outside, "large"), filepath.Join(chrome, "linked-file")))
	size, err := profileDirectorySize(t.Context(), data, "managed")
	must(t, err)
	if size != 8 {
		t.Fatalf("size = %d, want 8", size)
	}
	for _, id := range []string{"missing", "../managed"} {
		if _, err := profileDirectorySize(t.Context(), data, id); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	must(t, os.Rename(chrome, chrome+"-old"))
	must(t, os.Symlink(chrome+"-old", chrome))
	if _, err := profileDirectorySize(t.Context(), data, "managed"); err == nil {
		t.Fatal("followed Chrome directory symlink")
	}
	must(t, os.Remove(chrome))
	must(t, os.Rename(chrome+"-old", chrome))
	parent := filepath.Dir(chrome)
	must(t, os.Rename(parent, parent+"-old"))
	must(t, os.Symlink(parent+"-old", parent))
	if _, err := profileDirectorySize(t.Context(), data, "managed"); err == nil {
		t.Fatal("followed profile directory symlink")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := profileDirectorySize(ctx, data, "managed-old"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func waitProfileSize(t *testing.T, c *profileSizes, id string) profileSizeEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		entry, ok := c.entries[id]
		c.mu.Unlock()
		if ok && !entry.pending {
			return entry
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("background size calculation did not finish")
	return profileSizeEntry{}
}

func TestProfileSizeCacheAsyncExpiryAndRetry(t *testing.T) {
	started := make(chan string, 8)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	c := profileSizes{calculate: func(ctx context.Context, id string) (int64, error) {
		started <- id
		select {
		case <-release:
		case <-ctx.Done():
			return 0, ctx.Err()
		}
		if id == "broken" {
			return 0, errors.New("unreadable")
		}
		return 7, nil
	}}
	if c.get("managed", nil) != nil {
		t.Fatal("initial size known")
	}
	<-started
	// The worker is blocked: page requests still return, and one profile gets only one worker.
	for range 20 {
		if c.get("managed", nil) != nil {
			t.Fatal("pending size known")
		}
	}
	select {
	case <-started:
		t.Fatal("duplicate traversal")
	default:
	}
	once.Do(func() { close(release) })
	entry := waitProfileSize(t, &c, "managed")
	if entry.size == nil || *entry.size != 7 {
		t.Fatal(entry)
	}
	size := c.get("managed", nil)
	if size == nil || *size != 7 {
		t.Fatal(size)
	}
	*size = 100
	if *c.get("managed", nil) != 7 {
		t.Fatal("caller changed cache")
	}
	c.mu.Lock()
	entry.until = time.Now().Add(-time.Second)
	c.entries["managed"] = entry
	c.mu.Unlock()
	if c.get("managed", nil) != nil {
		t.Fatal("expired size returned")
	}
	<-started
	waitProfileSize(t, &c, "managed")
	if c.get("broken", nil) != nil {
		t.Fatal("error size known")
	}
	<-started
	entry = waitProfileSize(t, &c, "broken")
	if entry.size != nil || !entry.until.After(time.Now()) {
		t.Fatal("failure not cached")
	}
	for range 20 {
		if c.get("broken", nil) != nil {
			t.Fatal("failed size known")
		}
	}
	select {
	case <-started:
		t.Fatal("failure retried before TTL")
	default:
	}
	c.mu.Lock()
	entry.until = time.Now().Add(-time.Second)
	c.entries["broken"] = entry
	c.mu.Unlock()
	c.get("broken", nil)
	<-started
	waitProfileSize(t, &c, "broken")
}

func TestProfileSizeCacheBoundsAndAttached(t *testing.T) {
	c := profileSizes{entries: make(map[string]profileSizeEntry), calculate: func(context.Context, string) (int64, error) { return 0, nil }}
	for n := range profileSizeLimit {
		c.entries[string(rune(n))] = profileSizeEntry{until: time.Now().Add(time.Hour)}
	}
	c.get("new", nil)
	waitProfileSize(t, &c, "new")
	if len(c.entries) != profileSizeLimit {
		t.Fatalf("cache grew to %d", len(c.entries))
	}
	c.running = profileSizeWorkers
	if c.get("busy", nil) != nil {
		t.Fatal("unexpected size")
	}
	if _, ok := c.entries["busy"]; ok {
		t.Fatal("worker limit ignored")
	}
	store := OpenProfiles(t.TempDir(), "")
	store.data.Profiles["attached"] = Profile{ID: "attached", Kind: "attached"}
	if store.SizeBytes("attached") != nil || store.SizeBytes("missing") != nil || len(store.sizes.entries) != 0 {
		t.Fatal("scheduled external or unknown profile")
	}
}
