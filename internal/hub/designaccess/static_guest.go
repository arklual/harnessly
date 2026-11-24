package designaccess

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"image/jpeg"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// StaticPrefix addresses immutable snapshots on the Hub. It never reaches a runtime.
const StaticPrefix = "/guest/design/"

// The standard ingress has a one-hour read timeout. Return a definite publication result before it.
const staticPublicationTimeout = 50 * time.Minute

const maxStaticBoards = 64
const maxStaticPage = 2 << 20
const maxStaticBundle = 16 << 20

func isStaticToken(token string) bool {
	return strings.HasPrefix(token, "snap-") && shareTokenRe.MatchString(strings.TrimPrefix(token, "snap-"))
}

type staticPage struct{ Path, Title, Image string }

func cleanStaticPage(raw []byte) (string, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2400 || cfg.Height > 2400 {
		return "", errRuntime
	}
	pic, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", errRuntime
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, pic, &jpeg.Options{Quality: 90}); err != nil || b.Len() > maxStaticPage {
		return "", errRuntime
	}
	return base64.StdEncoding.EncodeToString(b.Bytes()), nil
}

func (s *Service) captureStatic(ctx context.Context, machine, project string, in shareInput) ([]staticPage, error) {
	req := hubproto.DesignPublicTokenRequest{Project: project, Rev: in.Rev, Capture: true, Pages: in.Pages}
	unavailable := fail("unavailable", "The owner machine could not capture the complete revision")
	get := func(segments []string, limit int64) ([]byte, error) {
		res, err := s.machineFetch(ctx, machine, req, segments, nil)
		if err != nil {
			s.log.Debug("design snapshot: capture request failed", "raster", len(segments) == 2, "error", err)
			return nil, unavailable
		}
		if res.StatusCode != http.StatusOK || len(segments) == 2 && (res.Header.Get("X-Design-Static-Page") != segments[1] || res.Header.Get("Content-Type") != "image/jpeg") {
			res.Body.Close()
			s.log.Debug("design snapshot: unexpected capture response", "raster", len(segments) == 2, "status", res.StatusCode)
			return nil, unavailable
		}
		b, err := readAll(res, limit)
		if err != nil {
			return nil, unavailable
		}
		return b, nil
	}
	raw, err := get(nil, maxIndex)
	if err != nil {
		return nil, err
	}
	var index struct {
		Kind   *string
		Boards []struct{ Path, Title string }
	}
	if json.Unmarshal(raw, &index) != nil || index.Kind != nil || len(index.Boards) == 0 || len(index.Boards) > maxStaticBoards {
		return nil, unavailable
	}
	if in.Pages != nil && len(in.Pages) != len(index.Boards) {
		return nil, unavailable
	}
	scope := shareTarget{Pages: in.Pages}
	pages := make([]staticPage, 0, len(index.Boards))
	seen := map[string]bool{}
	size := 0
	for _, b := range index.Boards {
		if b.Path == "" || len(b.Path) > maxBoard || seen[b.Path] || !scope.allows(b.Path) {
			return nil, unavailable
		}
		seen[b.Path] = true
		raw, err := get([]string{"__raster", fmt.Sprintf("%x", sha256.Sum256([]byte(b.Path)))}, maxStaticPage)
		if err != nil {
			return nil, err
		}
		pic, err := cleanStaticPage(raw)
		if err != nil {
			return nil, unavailable
		}
		size += len(pic)
		if size > maxStaticBundle {
			return nil, unavailable
		}
		pages = append(pages, staticPage{Path: b.Path, Title: clip(b.Title, maxTitle), Image: pic})
	}
	return pages, nil
}

func (t *txn) saveStatic(row shareRow, pages []staticPage) error {
	for i, p := range pages {
		if _, err := t.q.Exec(t.ctx, `INSERT INTO design_static_pages(link_id,rev,position,board,title,jpeg) VALUES(?,?,?,?,?,?)`, row.ID, row.Rev, i, p.Path, p.Title, p.Image); err != nil {
			return err
		}
	}
	return nil
}

var staticView = template.Must(template.New("snapshot").Parse(`<!doctype html><html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}}</title><style>body{margin:0;font:1rem/1.5 system-ui,sans-serif}nav{display:flex;flex-wrap:wrap;gap:1em;padding:.6em}main{text-align:center;background:#eee}img{max-width:100%;height:auto;vertical-align:middle}</style><nav>{{range .Pages}}<a href="?page={{.N}}">{{.Title}}</a>{{end}}</nav><main><img src="{{.Image}}" alt="{{.Title}}"></main></html>`))

func (s *Service) StaticGuest(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "private, no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:; style-src 'unsafe-inline'; frame-ancestors 'none'; form-action 'none'; base-uri 'none'")
	deny := func() { http.NotFound(w, r) }
	if !s.staticGuests || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		deny()
		return
	}
	token := strings.TrimPrefix(r.URL.Path, StaticPrefix)
	if !isStaticToken(token) {
		deny()
		return
	}
	state, l, err := s.resolveShare(r.Context(), s.now(), token)
	if err != nil || state != "ok" || l == nil || l.Rights != roleView || l.Version != "pinned" || l.Rev == "" || l.Expires == nil {
		deny()
		return
	}
	m, err := s.db.S().MachineByID(r.Context(), l.Machine)
	if err != nil {
		deny()
		return
	}
	t, err := s.read(r.Context(), m)
	if err != nil {
		deny()
		return
	}
	ok, err := t.makerMayShare(l.Project, l.CreatedBy)
	if err != nil || !ok {
		deny()
		return
	}
	page := 0
	if q := r.URL.Query(); len(q) > 0 {
		v := q.Get("page")
		page, err = strconv.Atoi(v)
		if len(q) != 1 || len(q["page"]) != 1 || err != nil || page < 0 || strconv.Itoa(page) != v {
			deny()
			return
		}
	}
	rows, err := s.db.Query(r.Context(), `SELECT position,board,title FROM design_static_pages WHERE link_id=? AND rev=? ORDER BY position`, l.ID, l.Rev)
	if err != nil {
		deny()
		return
	}
	type navPage struct {
		N     int
		Title string
	}
	var nav []navPage
	var title string
	found := false
	for rows.Next() {
		var item navPage
		var path string
		if err = rows.Scan(&item.N, &path, &item.Title); err != nil {
			break
		}
		if !l.allows(path) {
			continue
		}
		if item.Title == "" {
			item.Title = strconv.Itoa(item.N + 1)
		}
		nav = append(nav, item)
		if item.N == page {
			title = item.Title
			found = true
		}
	}
	rowsErr := rows.Err()
	rows.Close()
	if err != nil || rowsErr != nil || !found {
		deny()
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	var encoded string
	if s.db.QueryRow(r.Context(), `SELECT jpeg FROM design_static_pages WHERE link_id=? AND rev=? AND position=?`, l.ID, l.Rev, page).Scan(&encoded) != nil {
		deny()
		return
	}
	// Only cleanStaticPage's canonical base64 JPEG is stored in this column.
	_ = staticView.Execute(w, struct {
		Title string
		Pages []navPage
		Image template.URL
	}{title, nav, template.URL("data:image/jpeg;base64," + encoded)}) // #nosec G203 -- image is re-encoded before storage.
}
