package docs

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
)

var legacySlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// LegacyRedirect answers the old document address, /a/<slug> (HUB_DOCS_LEGACY_HOSTS), with a permanent redirect to the
// document's page in the hub's workspace. It serves no content: whether the person may read the document is decided
// there, by their sign-in and the document's own access, as for any link to it. Anything but /a/<slug> is 404.
func LegacyRedirect(publicURL string) http.Handler {
	base := strings.TrimRight(publicURL, "/")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, "/a/")
		slug, _, _ := strings.Cut(rest, "/")
		if !ok || !legacySlug.MatchString(slug) {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		to := base + "/agents/docs/" + url.PathEscape(slug)
		if v := r.URL.Query().Get("v"); v != "" && len(v) <= 6 && strings.Trim(v, "0123456789") == "" {
			to += "?v=" + v
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, to, http.StatusMovedPermanently)
	})
}
