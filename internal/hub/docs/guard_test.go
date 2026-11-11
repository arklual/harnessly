package docs

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
)

// Every route of one document refuses a person of another space, for a document of a space (plan, private) and for
// one the hub knows only by its author (sketch, the board wall), before anything is read or changed; the author and
// the administrator of the installation keep their way in to the document without an organization.
func TestDocumentRoutesRefuseOtherSpaces(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	type route struct {
		name string
		fn   func(http.ResponseWriter, *http.Request, *auth.Principal) error
		body string
	}
	routes := []route{
		{"detail", s.detail, ""},
		{"newThread", s.newThread, `{"body":"x","anchor":{}}`},
		{"reply", s.reply, `{"body":"x"}`},
		{"threadStatus", s.threadStatus, `{"status":"resolved"}`},
		{"read", s.read, `{}`},
		{"content", s.content, ""},
		{"versions", s.versions, ""},
		{"diff", s.diffRoute, ""},
		{"getAccess", s.getAccess, ""},
		{"postAccess", s.postAccess, `{"public":true}`},
		{"source", s.source, ""},
		{"saveSource", s.saveSource, `{"markdown":"x"}`},
		{"acl", s.aclRoute, ""},
	}
	call := func(rt route, p *auth.Principal, slug string) error {
		r := httptest.NewRequest("POST", "/x", strings.NewReader(rt.body))
		r.Header.Set("Content-Type", "application/json")
		r.SetPathValue("slug", slug)
		r.SetPathValue("id", "t1")
		w := httptest.NewRecorder()
		if err := rt.fn(w, r, p); err != nil {
			return err
		}
		if w.Code >= 400 {
			return fail(w.Code, "", strings.TrimSpace(w.Body.String()))
		}
		return nil
	}
	for _, slug := range []string{"plan", "private", "sketch", "wall"} {
		for _, rt := range routes {
			err := call(rt, who["stranger"], slug)
			var f *failure
			if !errors.As(err, &f) || (f.status != http.StatusForbidden && f.status != http.StatusNotFound) {
				t.Errorf("a stranger on %s of %s: %v", rt.name, slug, err)
			}
		}
	}
	// The way in that stays: the author to their own document, and the administrator of the installation.
	for _, p := range []string{"author", "admin"} {
		for _, slug := range []string{"private", "sketch"} {
			if p == "admin" && slug == "sketch" {
				continue // a document the hub has no record of is its author's
			}
			if err := call(routes[0], who[p], slug); err != nil {
				t.Errorf("%s opens %s: %v", p, slug, err)
			}
		}
	}
}
