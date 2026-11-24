package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/docprovider"
)

type personalDocumentReader struct {
	docprovider.Provider
	calls int
}

func (p *personalDocumentReader) Read(_ context.Context, slug, version string) (docprovider.Content, error) {
	p.calls++
	if slug != docprovider.Alias("sample_Doc123") || version != "" {
		return docprovider.Content{}, errors.New("not accessible")
	}
	return docprovider.Content{Format: "md", Content: "sample"}, nil
}

func TestManagedDocumentRead(t *testing.T) {
	p := &personalDocumentReader{}
	defer docprovider.Pin(p)()
	t.Setenv("RUNTIME_OWNER_EMAIL", "owner@example.org")
	t.Setenv("RUNTIME_DOCUMENT_READER_PACKAGES", "org.reader=digest")
	app := apps.Installed{ID: "org.reader", Env: true, EnvSum: "digest", Manifest: &apps.Manifest{Permissions: []string{"documents.read"}}}
	session := apps.Session{ManagedSum: "digest"}
	args := map[string]any{"id": "sample_Doc123"}
	owner := context.WithValue(context.Background(), documentSubjectKey{}, "owner@example.org")
	got, err := readManagedDocument(owner, session, app, args)
	if err != nil || got.Content != "sample" || p.calls != 1 {
		t.Fatalf("read %v %v, calls %d", got, err, p.calls)
	}
	if _, err = readManagedDocument(owner, session, app, map[string]any{"id": "forbidden"}); err == nil {
		t.Fatal("provider refusal lost")
	}
	before := p.calls
	for _, subject := range []string{"", "user", "machine-agent", "guest@example.org"} {
		if _, err := readManagedDocument(context.WithValue(context.Background(), documentSubjectKey{}, subject), session, app, args); err == nil {
			t.Errorf("allowed %q", subject)
		}
	}
	for _, bad := range []apps.Session{{}, {ManagedSum: "previous"}} {
		if _, err := readManagedDocument(owner, bad, app, args); err == nil {
			t.Error("stale or unbound session allowed")
		}
	}
	for _, mutate := range []func(*apps.Installed){func(a *apps.Installed) { a.Env = false }, func(a *apps.Installed) { a.ID = "other" }, func(a *apps.Installed) { a.Manifest = &apps.Manifest{} }, func(a *apps.Installed) { a.EnvSum = "changed" }} {
		a := app
		mutate(&a)
		if _, err := readManagedDocument(owner, session, a, args); err == nil {
			t.Error("unbound package allowed")
		}
	}
	if _, err := readManagedDocument(owner, session, app, map[string]any{"id": "sample_Doc123", "method": "publish"}); err == nil {
		t.Fatal("accepted extra arguments")
	}
	t.Setenv("RUNTIME_DOCUMENT_READER_PACKAGES", "")
	if _, err := readManagedDocument(owner, session, app, args); err == nil {
		t.Fatal("revoked binding allowed")
	}
	if before != p.calls {
		t.Fatalf("refusal reached provider %d times", p.calls-before)
	}
}

func TestManagedDocumentViewSessionAndToolRoute(t *testing.T) {
	call, _ := appRig(t)
	dir := filepath.Join(t.TempDir(), "reader")
	if err := apps.Scaffold(dir, "acme.reader", "Reader", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, apps.ManifestPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest apps.Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Permissions = append(manifest.Permissions, "documents.read")
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	registry := apps.Open(decRigDir)
	pkg, _, err := registry.InstallForEnv(dir, apps.EnvInstallOptions{Link: "managed", ItemID: "reader", HostVersion: apps.TemplateVersion})
	if err != nil {
		t.Fatal(err)
	}
	p := &personalDocumentReader{}
	defer docprovider.Pin(p)()
	t.Setenv("RUNTIME_OWNER_EMAIL", "owner@example.org")
	t.Setenv("RUNTIME_DOCUMENT_READER_PACKAGES", pkg.ID+"="+pkg.Sum)
	page := map[string]string{"X-Test-Session": "yes", "X-Test-Subject": "owner@example.org", "X-Test-Verified-Owner": "true"}
	code, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.reader","view":"main","target":"pane"}`, page)
	var view struct{ Session string }
	if code != 200 || json.Unmarshal([]byte(body), &view) != nil || view.Session == "" {
		t.Fatalf("open: %d %s", code, body)
	}
	read := `{"session":"` + view.Session + `","name":"@host/documents/read","args":{"id":"sample_Doc123"}}`
	code, body = call("POST", platformAPI+"/apps/tool", read, page)
	if code != 200 || !strings.Contains(body, `"content":"sample"`) {
		t.Fatalf("read: %d %s", code, body)
	}
	delete(page, "X-Test-Verified-Owner")
	if code, _ := call("POST", platformAPI+"/apps/tool", read, page); code != 403 {
		t.Fatalf("unverified owner email: %d", code)
	}
	page["X-Test-Verified-Owner"] = "true"
	page["X-Test-Subject"] = "viewer@example.org"
	if code, _ := call("POST", platformAPI+"/apps/tool", read, page); code != 403 {
		t.Fatalf("other viewer: %d", code)
	}
	page["X-Test-Subject"] = "owner@example.org"
	if _, _, err := registry.Install(dir, apps.InstallOptions{HostVersion: apps.TemplateVersion}); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("POST", platformAPI+"/apps/tool", read, page); code == 200 {
		t.Fatal("shadow package kept managed permission")
	}
	if p.calls != 1 {
		t.Fatalf("unexpected provider calls: %d", p.calls)
	}
}
