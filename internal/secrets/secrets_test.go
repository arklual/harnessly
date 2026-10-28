package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeValue is built at run time so no credential-shaped string sits in the source.
var fakeValue = "gh" + "p_" + strings.Repeat("Z9", 18)

func newStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data", "secrets")
	key := filepath.Join(t.TempDir(), "state", "harnessly", "safe.key")
	return Open(dir, key), dir, key
}

func TestStoreSealsValuesAndKeepsTheKeyApart(t *testing.T) {
	s, dir, key := newStore(t)
	v, created, err := s.Put("github-token", "  "+fakeValue+"\n")
	if err != nil || !created || v.Name != "github-token" || v.CreatedAt == 0 {
		t.Fatalf("put: %+v %v %v", v, created, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "safe.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), fakeValue) || strings.Contains(string(raw), "Z9Z9") {
		t.Fatalf("the safe's file holds the value in the clear: %s", raw)
	}
	for _, p := range []string{filepath.Join(dir, "safe.json"), key} {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", p, st, err)
		}
	}
	if st, _ := os.Stat(filepath.Dir(key)); st.Mode().Perm() != 0o700 {
		t.Fatalf("key folder mode %v", st.Mode())
	}
	got, err := s.Reveal([]string{"github-token"}, Use{Conv: "c1", Title: "Release"})
	if err != nil || got["github-token"] != fakeValue {
		t.Fatalf("reveal: %v %v", got, err)
	}
	list, _ := s.List()
	if len(list) != 1 || list[0].LastUsedAt == 0 || len(list[0].UsedBy) != 1 || list[0].UsedBy[0].Title != "Release" {
		t.Fatalf("list: %+v", list)
	}
	// A new value keeps the secret's uses and creation.
	v2, created, err := s.Put("github-token", fakeValue+"2")
	if err != nil || created || v2.CreatedAt != v.CreatedAt || len(v2.UsedBy) != 1 {
		t.Fatalf("replace: %+v %v %v", v2, created, err)
	}
	if got, _ := s.Reveal([]string{"github-token"}, Use{Conv: "c1"}); got["github-token"] != fakeValue+"2" {
		t.Fatal("the new value is not the one kept")
	}
	if list, _ := s.List(); len(list[0].UsedBy) != 1 {
		t.Fatalf("a conversation counts once: %+v", list[0].UsedBy)
	}
	if err := s.Remove("github-token"); err != nil {
		t.Fatal(err)
	}
	if ReasonOf(s.Remove("github-token")) != ReasonNotFound {
		t.Fatal("removing twice")
	}
	if _, err := s.Reveal([]string{"github-token"}, Use{}); ReasonOf(err) != ReasonNotFound {
		t.Fatalf("reveal of a removed one: %v", err)
	}
}

func TestStoreRefusals(t *testing.T) {
	s, _, key := newStore(t)
	for name, want := range map[string]string{"../x": ReasonBadName, "-x": ReasonBadName, "a b": ReasonBadName, strings.Repeat("a", 65): ReasonBadName} {
		if _, _, err := s.Put(name, "v"); ReasonOf(err) != want {
			t.Fatalf("%q: %v", name, err)
		}
	}
	if _, _, err := s.Put("x", " \n "); ReasonOf(err) != ReasonEmpty {
		t.Fatalf("empty: %v", err)
	}
	if _, _, err := s.Put("x", strings.Repeat("a", MaxValue+1)); ReasonOf(err) != ReasonTooLarge {
		t.Fatalf("large: %v", err)
	}
	// A key pasted several lines long stays as typed.
	pem := "-----BEGIN KEY-----\nabc\n-----END KEY-----\n"
	if _, _, err := s.Put("deploy-key", pem); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Reveal([]string{"deploy-key"}, Use{}); got["deploy-key"] != pem {
		t.Fatalf("pem: %q", got["deploy-key"])
	}
	// The key gone, the values are lost and say so; the list still shows them.
	if err := os.Remove(key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reveal([]string{"deploy-key"}, Use{}); ReasonOf(err) != ReasonKeyLost {
		t.Fatalf("key lost: %v", err)
	}
	if list, err := s.List(); err != nil || len(list) != 1 {
		t.Fatalf("list without key: %v %v", list, err)
	}
	// Put again with a new key, it reads.
	if _, _, err := s.Put("deploy-key", pem); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Reveal([]string{"deploy-key"}, Use{}); err != nil || got["deploy-key"] != pem {
		t.Fatalf("after a new put: %v", err)
	}
}

func TestStoreConcurrentPutsAndReveals(t *testing.T) {
	s, _, _ := newStore(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := "k" + string(rune('a'+i))
			if _, _, err := s.Put(name, fakeValue); err != nil {
				t.Error(err)
				return
			}
			if got, err := s.Reveal([]string{name}, Use{Conv: name}); err != nil || got[name] != fakeValue {
				t.Error(name, err)
			}
		}()
	}
	wg.Wait()
	if list, _ := s.List(); len(list) != 8 {
		t.Fatalf("lost a write: %d", len(list))
	}
}

func TestDefaultKeyFileIsOutsideTheDataFolder(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if got := DefaultKeyFile(get, "/home/me"); got != "/home/me/.local/state/harnessly/safe.key" {
		t.Fatal(got)
	}
	env["XDG_STATE_HOME"] = "/st"
	if got := DefaultKeyFile(get, "/home/me"); got != "/st/harnessly/safe.key" {
		t.Fatal(got)
	}
	env["RUNTIME_SECRETS_KEY_FILE"] = "/etc/harnessly/key"
	if got := DefaultKeyFile(get, "/home/me"); got != "/etc/harnessly/key" {
		t.Fatal(got)
	}
}

func TestAsksWaitFulfilDecline(t *testing.T) {
	a := NewAsks()
	first, state, err := a.Open("github-token", "push\nthe branch\x1b[31m", "c1", "Release", false)
	if err != nil || state != AskWaiting || !strings.HasPrefix(first.Why, "push") {
		t.Fatalf("open: %+v %s %v", first, state, err)
	}
	if strings.ContainsAny(first.Why, "\n\x1b") {
		t.Fatalf("why is not one safe line: %q", first.Why)
	}
	// Asking again waits on the same request.
	again, _, _ := a.Open("github-token", "x", "c2", "", false)
	if again.ID != first.ID || len(a.Pending()) != 1 {
		t.Fatal("a repeated ask made a second card")
	}
	if !a.PageOpen() {
		t.Fatal("Pending is a page looking")
	}
	got := make(chan string, 2)
	for range 2 {
		go func() { got <- a.Wait(context.Background(), first.ID, 5*time.Second) }()
	}
	time.Sleep(20 * time.Millisecond)
	a.Fulfil("github-token")
	for range 2 {
		if s := <-got; s != AskReady {
			t.Fatalf("waiter got %s", s)
		}
	}
	if len(a.Pending()) != 0 {
		t.Fatal("a fulfilled ask is still on screen")
	}

	x, _, _ := a.Open("openai-key", "y", "c1", "", false)
	if a.Wait(context.Background(), x.ID, 10*time.Millisecond) != AskWaiting {
		t.Fatal("no answer yet is waiting")
	}
	if !a.Decline(x.ID) || a.Decline(x.ID) {
		t.Fatal("decline once")
	}
	if _, state, _ := a.Open("openai-key", "y", "c1", "", false); state != AskDeclined {
		t.Fatalf("a declined ask answers a repeat for a while: %s", state)
	}
	if a.Wait(context.Background(), "sa_none", time.Second) != AskDeclined {
		t.Fatal("an unknown id")
	}
}

func TestAsksExpireAndLimit(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	a := NewAsks()
	a.Now = func() time.Time { return now }
	x, _, _ := a.Open("k0", "w", "c1", "", false)
	now = now.Add(askTTL)
	if len(a.Pending()) != 0 {
		t.Fatal("an ask past its time stays")
	}
	if a.Wait(context.Background(), x.ID, time.Second) != AskDeclined {
		t.Fatal("an expired ask ends declined")
	}
	for i := 1; i < 7; i++ {
		if _, _, err := a.Open("k"+string(rune('0'+i)), "w", "c1", "", false); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, _, err := a.Open("k9", "w", "c1", "", false); ReasonOf(err) != ReasonRateLimited {
		t.Fatalf("seventh ask in 10 minutes: %v", err)
	}
	if _, _, err := a.Open("k9", "w", "c2", "", false); err != nil {
		t.Fatalf("another conversation has its own limit: %v", err)
	}
	if _, _, err := a.Open("bad name", "w", "c3", "", false); ReasonOf(err) != ReasonBadName {
		t.Fatal(err)
	}
}

func TestFilesAreOwnerOnlyAndGoAway(t *testing.T) {
	f := NewFiles(t.TempDir())
	p, err := f.Write("deploy-key", fakeValue, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", st, err)
	}
	if st, _ := os.Stat(f.Dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir %v", st.Mode())
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the file outlived its time")
		}
		time.Sleep(10 * time.Millisecond)
	}
	p2, _ := f.Write("x", "v", time.Hour)
	f.Close()
	if _, err := os.Stat(p2); !os.IsNotExist(err) {
		t.Fatal("Close leaves a file")
	}
}

func TestPromptBlockTellsNotToUseTheChat(t *testing.T) {
	b := PromptBlock()
	for _, want := range []string{"harnessly secret ask", "harnessly secret run --env", "Never ask the person to paste"} {
		if !strings.Contains(b, want) {
			t.Fatalf("prompt block lacks %q", want)
		}
	}
}
