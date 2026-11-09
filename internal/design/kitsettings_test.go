package design

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

func kitStatus(err error) int {
	var ke *KitError
	if errors.As(err, &ke) {
		return ke.Status
	}
	return 0
}

func TestTheKitScreenReadsTheDraftWhereAVersionGoes(t *testing.T) {
	l, _ := kitLibrary(t)
	if err := l.SetKitPin("admin", &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}}); err != nil {
		t.Fatal(err)
	}
	if designkit.Resolve(l.KitsDir(), "acme-ds", designkit.DraftVersion, []string{"kit.json"}) != "" {
		t.Fatal("a kit without a draft has nothing at the draft's place")
	}
	d, err := l.EditKitFrom("admin")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(l.ProjectDir(d.Project), "themes", "plain", "button.css"), ".button{color:teal}")
	got := designkit.Resolve(l.KitsDir(), "acme-ds", designkit.DraftVersion, []string{"themes", "plain", "button.css"})
	if got == "" || readFileT(t, got) != ".button{color:teal}" {
		t.Fatalf("the draft is not read at its place: %q", got)
	}
	if designkit.Resolve(l.KitsDir(), "acme-ds", designkit.DraftVersion, []string{"..", "admin", "meta", "project.json"}) != "" {
		t.Fatal("the draft's place lets a path out of it")
	}
	if l.kitCache(got) == kitImmutable {
		t.Fatal("a file of the draft is kept for good")
	}
	if v := designkit.Resolve(l.KitsDir(), "acme-ds", "1.0.0", []string{"themes", "plain", "button.css"}); l.kitCache(v) != kitImmutable {
		t.Fatal("a version's file is not kept for good")
	}
}

func TestAKitForViewingOnlyIsEditedByNobodyUntilItsMakerTurnsItOff(t *testing.T) {
	l, _ := kitLibrary(t)
	for _, p := range []string{"admin", "post"} {
		if err := l.SetKitPin(p, &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}}); err != nil {
			t.Fatal(p, err)
		}
	}
	d, err := l.EditKitFrom("admin")
	if err != nil {
		t.Fatal(err)
	}
	if !l.RoleFor(d.Project, "").AtLeast(RoleEdit) {
		t.Fatal("the draft is not edited before viewing only")
	}

	// Only the maker turns it on.
	if err := l.SetKitReadOnly("acme-ds", "someone@example.local", true); kitStatus(err) != 403 {
		t.Fatalf("not the maker: %v", err)
	}
	if l.KitReadOnly("acme-ds") {
		t.Fatal("a refused change changed the kit")
	}
	if err := l.SetKitReadOnly("acme-ds", "", true); err != nil {
		t.Fatal(err)
	}
	if st := l.KitSettingsFor("acme-ds", "someone@example.local"); !st.ReadOnly || st.Owner {
		t.Fatalf("another person's settings: %+v", st)
	}
	if st := l.KitSettingsFor("acme-ds", ""); !st.ReadOnly || !st.Owner {
		t.Fatalf("the maker's settings: %+v", st)
	}

	// Nobody edits it now, the maker included: the draft is looked at, nothing starts, publishes or moves onto it.
	if r := l.RoleFor(d.Project, ""); r.AtLeast(RoleEdit) || !r.AtLeast(RoleView) {
		t.Fatalf("the draft's role while viewing only: %v", r)
	}
	if _, err := l.StartKitDraft("acme-ds"); kitStatus(err) != 403 {
		t.Fatalf("start: %v", err)
	}
	if _, err := l.PublishKitDraft("acme-ds", "minor", "x"); kitStatus(err) != 403 {
		t.Fatalf("publish: %v", err)
	}
	if err := l.DiscardKitDraft("acme-ds"); kitStatus(err) != 403 {
		t.Fatalf("discard: %v", err)
	}
	if _, err := l.EditKitFrom("post"); kitStatus(err) != 403 {
		t.Fatalf("edit in the kit: %v", err)
	}
	if pin, _ := l.KitPinOf("post"); pin.Draft {
		t.Fatal("a project moved onto the draft of a kit for viewing only")
	}
	if !l.RoleFor("post", "").AtLeast(RoleEdit) {
		t.Fatal("a project on the kit is its own: viewing only is the kit's, not the project's")
	}

	// The maker turns it off: the kit is edited again.
	if err := l.SetKitReadOnly("acme-ds", "", false); err != nil {
		t.Fatal(err)
	}
	if !l.RoleFor(d.Project, "").AtLeast(RoleEdit) {
		t.Fatal("the draft stays for viewing only")
	}
	if _, err := l.PublishKitDraft("acme-ds", "minor", "x"); kitStatus(err) != 409 {
		t.Fatalf("publish of a draft without edits after viewing only: %v", err)
	}
}

// The draft's files at the kit's draft place are its project's: closed to whoever its project's board is closed to.
func TestTheKitsDraftOnTheLibraryServerIsClosedLikeItsProject(t *testing.T) {
	l, _ := kitLibrary(t)
	if err := l.SetKitPin("admin", &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}}); err != nil {
		t.Fatal(err)
	}
	path := "/kit/acme-ds/draft/themes/plain/button.css"
	if w := boardGet(l, path); w.Code != http.StatusNotFound {
		t.Fatalf("a kit without a draft answers %d", w.Code)
	}
	d, err := l.EditKitFrom("admin")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(l.ProjectDir(d.Project), "themes", "plain", "button.css"), ".button{color:teal}")
	if w := boardGet(l, path); w.Code != http.StatusOK {
		t.Fatalf("the draft is not served to whoever may see it: %d", w.Code)
	}
	writeFile(t, filepath.Join(l.ProjectDir(d.Project), OwnerFile), otherV2)
	l.now = func() time.Time { return accessNow }
	if err := l.SetAccess(snapshot(map[string]Grant{}, map[string]string{d.Project: "none"})); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, "/kitv/acme-ds/draft/plain/button.css"} {
		if w := boardGet(l, p); w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "teal") {
			t.Fatalf("%s is served without a role in the draft: %d", p, w.Code)
		}
	}
	if w := boardGet(l, "/kit/acme-ds/1.0.0/themes/plain/button.css"); w.Code != http.StatusOK {
		t.Fatalf("a published version is closed with the draft: %d", w.Code)
	}
}

// A theme folder of a kit that is a link out of the kit is not served, exported or published.
func TestAKitThemeThatLinksOutOfTheKitIsNotUsed(t *testing.T) {
	l, _ := kitLibrary(t)
	if err := l.SetKitPin("admin", &KitPin{ID: "acme-ds", Version: "1.0.0", Mounts: []string{"plain"}}); err != nil {
		t.Fatal(err)
	}
	if l.kitFile("admin", []string{"ds", "plain", "button.css"}) == "" || l.KitMountDirs("admin")["plain"] == "" {
		t.Fatal("the kit's own theme is not used")
	}
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "button.css"), "secret")
	theme := filepath.Join(l.KitsDir(), "acme-ds", "1.0.0", "themes", "plain")
	if err := os.RemoveAll(theme); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, theme); err != nil {
		t.Fatal(err)
	}
	if got := l.kitFile("admin", []string{"ds", "plain", "button.css"}); got != "" {
		t.Fatalf("a file out of the kit is served: %s", got)
	}
	if dirs := l.KitMountDirs("admin"); dirs["plain"] != "" {
		t.Fatalf("a folder out of the kit goes with an export: %v", dirs)
	}
}
