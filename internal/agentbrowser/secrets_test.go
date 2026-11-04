package agentbrowser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretRegistryDoesNotPersistPageEchoes(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	value := "generated-" + strings.Repeat("v3", 18)
	i.pages["tab"] = newPage(Tab{ID: "tab", Conversation: "chat", URL: "https://example.com", Control: "agent"})
	i.RememberSecret(value)
	if err := i.Seal("chat", "tab", "document"); err != nil {
		t.Fatal(err)
	}
	i.mu.Lock()
	i.pages["tab"].URL = "https://example.com/" + value
	i.pages["tab"].Title = value
	i.changedLocked()
	i.mu.Unlock()
	i.save()
	raw, err := os.ReadFile(filepath.Join(i.dir, "tabs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(value)) {
		t.Fatal("registry contains secret")
	}
	var persisted registry
	if err = json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted.Tabs) != 1 || !persisted.Tabs[0].Sealed || persisted.Tabs[0].URL != "about:blank" {
		t.Fatalf("missing durable seal or unsafe restore URL: %s", raw)
	}
	restored := newInstance(m, DefaultProfile)
	if tab := restored.Tabs("chat")[0]; !tab.Sealed || tab.SecretDocument != "document" {
		t.Fatal("restart lost seal")
	}
	restored.pages["tab"].Title = value
	restored.RememberSecret("other-" + strings.Repeat("z7", 10))
	if restored.Tabs("chat")[0].Title != "" {
		t.Fatal("an unrelated new mask exposed metadata after restart")
	}

}
func TestSecretMasksAcrossTabs(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	short := strings.Repeat("s4", 7)
	long := short + strings.Repeat("l8", 9)
	i.RememberSecret(short)
	i.RememberSecret(long)
	i.pages["sibling"] = newPage(Tab{ID: "sibling", Conversation: "other", Title: long})
	if got := i.Redact(long); got != "[redacted]" {
		t.Fatal("profile masks exposed an overlapping value")
	}
	if got := i.Tabs("other")[0].Title; got != "[redacted]" {
		t.Fatal("sibling tab metadata exposed an overlapping value")
	}
}

func TestSecretInputReservationRollbackAndDurability(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	i.pages["tab"] = newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
	value := strings.Repeat("p7", 13)
	finish, err := i.BeginSecretInput("chat", "tab", "document")
	must(t, err)
	mask := i.ReserveSecrets(value)
	if i.Redact(value) != "[redacted]" {
		t.Fatal("in-flight value was not masked")
	}
	restored := newInstance(m, DefaultProfile)
	if tab := restored.Tabs("chat")[0]; !tab.Sealed || !tab.SecretUntracked {
		t.Fatal("in-flight input was not durably sealed")
	}
	mask(false)
	finish(false)
	if i.Tabs("chat")[0].Sealed || i.Tabs("chat")[0].SecretTouched || i.Redact(value) != value {
		t.Fatal("rejected input retained state")
	}
	first, second := i.ReserveSecrets(value), i.ReserveSecrets(value)
	first(false)
	if i.Redact(value) != "[redacted]" {
		t.Fatal("rollback removed another reservation")
	}
	second(true)
	third := i.ReserveSecrets(value)
	third(false)
	if i.Redact(value) != "[redacted]" {
		t.Fatal("rollback removed a committed mask")
	}
}

func TestUnsealRejectsReplacedGenerationAndControl(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
	i.pages[p.ID] = p
	must(t, i.Seal("chat", p.ID, "document"))
	checked := i.Tabs("chat")[0]
	must(t, i.SetControl(p.ID, "person"))
	must(t, i.setControl(p.ID, "agent", func(context.Context, string) (string, bool, error) {
		return "document", true, nil
	}))
	if err := i.Unseal("chat", p.ID, checked.SealGeneration); err == nil {
		t.Fatal("stale field proof removed a newer handoff seal in the same document")
	}
	current := i.Tabs("chat")[0]
	if !current.Sealed || !current.SecretUntracked {
		t.Fatal("failed comparison changed the current seal")
	}
	must(t, i.SetControl(p.ID, "person"))
	if err := i.Unseal("chat", p.ID, current.SealGeneration); !errors.Is(err, ErrPersonControl) {
		t.Fatal("seal release ignored person control")
	}
	if err := i.Unseal("another-chat", p.ID, current.SealGeneration); !errors.Is(err, ErrNotOwner) {
		t.Fatal("seal release ignored ownership")
	}
	must(t, i.setControl(p.ID, "agent", func(context.Context, string) (string, bool, error) {
		return "document", false, nil
	}))
	must(t, i.Unseal("chat", p.ID, current.SealGeneration))
	if i.Tabs("chat")[0].Sealed {
		t.Fatal("matching seal was not released")
	}
}
