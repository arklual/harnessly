package proxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ly-solutions/harnessly/internal/blobserve"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// A chat's snapshot shared by a link. The machine captures, redacts and publishes it (internal/share); the
// link used to be opened by a gateway that asked the author's machine for the page, so it needed that gateway and the
// machine. Behind the hub the hub keeps the published page itself:
//
//	…/api/<platform>/shares[/<id>]   the machine's own routes, passed on with the hub's address of the links; a
//	                                 published link's page is put into the store first, a revoked one ends in the hub
//	GET …/api/<platform>/chat-shares/<id>   the page, for a signed-in person the access model lets read the snapshot
//	                                 (resaccess kind "chat": the people of its space): for colleagues, never for anybody
//
// The page is the machine's redacted, script-free HTML; the hub serves it sandboxed under the page's own policy.

// shareBaseHeader is where a runtime reads the address of the links (cmd/runtime shareDefaults): the hub's word only.
const shareBaseHeader = "X-Harnessly-Share-Base"

// maxChatShare is the biggest snapshot page the hub keeps.
const maxChatShare = 32 << 20

// chatShareCSP is the snapshot page's own policy (share.PageCSP) in a sandbox: nothing of it runs or reads the hub.
const chatShareCSP = "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src 'none'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'"

var (
	chatSharesPath = regexp.MustCompile(`^/api/([A-Za-z0-9_-]+)/shares(?:/([A-Za-z0-9_-]{43}))?$`)
	chatSharePage  = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/chat-shares/([A-Za-z0-9_-]{43})$`)
	chatShareID    = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
)

// capRecorder keeps an answer of the machine up to max bytes.
type capRecorder struct {
	header http.Header
	status int
	wrote  bool
	over   bool
	max    int
	body   bytes.Buffer
}

func newCapRecorder(max int) *capRecorder {
	return &capRecorder{header: http.Header{}, status: http.StatusOK, max: max}
}

func (c *capRecorder) Header() http.Header { return c.header }

func (c *capRecorder) WriteHeader(code int) {
	if !c.wrote {
		c.status, c.wrote = code, true
	}
}

func (c *capRecorder) Write(b []byte) (int, error) {
	c.WriteHeader(http.StatusOK)
	if c.body.Len()+len(b) > c.max {
		c.over = true
		return 0, errors.New("the answer is too big")
	}
	return c.body.Write(b)
}

func (c *capRecorder) Flush() {}

// replay sends a kept answer on as the machine gave it.
func (c *capRecorder) replay(w http.ResponseWriter) {
	for k, v := range c.header {
		w.Header()[k] = v
	}
	w.WriteHeader(c.status)
	_, _ = w.Write(c.body.Bytes())
}

// chatShareBase is the address the links of this hub start with, as the page was asked.
func (s *Service) chatShareBase(r *http.Request, platform string) string {
	scheme := "http"
	if s.cfg.Secure {
		scheme = "https"
	}
	return scheme + "://" + strings.ToLower(r.Host) + s.cfg.BasePath + workspaceBase + "/api/" + platform + "/chat-shares/"
}

// serveChatShares passes the machine's share routes on with the hub's address of the links, and keeps what a link needs
// to open without the machine; false when the path is not theirs. r is the request as the runtime reads it.
func (s *Service) serveChatShares(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, rest string) bool {
	// The address of the links is the hub's word, never the browser's.
	r.Header.Del(shareBaseHeader)
	if match := chatGuestManagement.FindStringSubmatch(rest); match != nil {
		s.manageGuestChatSnapshot(w, r, u, m, match[1])
		return true
	}
	match := chatSharesPath.FindStringSubmatch(rest)
	st := s.uploadsStore()
	if match == nil || st == nil {
		return false
	}
	platform, id := match[1], match[2]
	r.Header.Set(shareBaseHeader, s.chatShareBase(r, platform))
	switch {
	case id == "" && r.Method == http.MethodPost:
		s.publishChatShare(w, r, u, m, st, platform)
	case id != "" && r.Method == http.MethodPatch:
		rec := newCapRecorder(maxPage)
		s.forward(rec, r, u, m)
		if exp, ok := shareExpiry(rec); ok && rec.status == http.StatusOK {
			if _, err := s.db.Exec(r.Context(), `UPDATE chat_shares SET expires_at = ? WHERE id = ? AND machine_id = ? AND revoked_at IS NULL`, exp, id, m.ID); err != nil {
				s.log.Warn("chat share: the new date was not kept", "machine", m.ID, "error", err)
			}
		}
		rec.replay(w)
	case id != "" && r.Method == http.MethodDelete:
		s.revokeChatShare(w, r, u, m, id)
	default:
		s.forward(w, r, u, m)
	}
	return true
}

// shareExpiry is {share: {expiresAt}} of the machine's answer, in milliseconds.
func shareExpiry(rec *capRecorder) (int64, bool) {
	var out struct {
		Share struct {
			ExpiresAt float64 `json:"expiresAt"`
		} `json:"share"`
	}
	if json.Unmarshal(rec.body.Bytes(), &out) != nil || out.Share.ExpiresAt <= 0 {
		return 0, false
	}
	return int64(out.Share.ExpiresAt), true
}

// publishChatShare keeps the draft's page in the store, lets the machine publish the link and records it. The page
// is read before the machine publishes: publishing removes the draft.
func (s *Service) publishChatShare(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, st Uploads, platform string) {
	ctx := r.Context()
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		httpx.WriteError(w, r, httpx.Invalid("body", "the body was not read"))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	var in struct {
		DraftID string `json:"draftId"`
	}
	if json.Unmarshal(body, &in) != nil || !chatShareID.MatchString(in.DraftID) {
		// Not a publish the hub understands: the machine says what is wrong with it.
		s.forward(w, r, u, m)
		return
	}
	page, err := http.NewRequestWithContext(ctx, http.MethodGet, workspaceBase+"/api/"+platform+"/snapshots/"+url.PathEscape(in.DraftID)+"/download?format=html", nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	page.Host = r.Host
	html := newCapRecorder(maxChatShare)
	s.forward(html, page, u, m)
	if html.over {
		httpx.WriteError(w, r, httpx.Invalid("draftId", "the snapshot is too big to share"))
		return
	}
	if html.status != http.StatusOK || html.body.Len() == 0 {
		// The machine's own refusal (a draft out of date, no session, away) is the answer.
		html.header.Del("Content-Disposition")
		html.replay(w)
		return
	}
	sum := sha256.Sum256(html.body.Bytes())
	sha := hex.EncodeToString(sum[:])
	if err := st.PutBlob(ctx, m.SpaceID, filestore.KindFiles, sha, bytes.NewReader(html.body.Bytes())); err != nil {
		s.fail(w, r, err)
		return
	}
	rec := newCapRecorder(maxPage)
	s.forward(rec, r, u, m)
	if rec.status == http.StatusCreated {
		var out struct {
			Share struct {
				ID        string  `json:"id"`
				Title     string  `json:"title"`
				ExpiresAt float64 `json:"expiresAt"`
			} `json:"share"`
		}
		if json.Unmarshal(rec.body.Bytes(), &out) != nil || !chatShareID.MatchString(out.Share.ID) || out.Share.ExpiresAt <= 0 {
			s.log.Warn("chat share: the machine published a link the hub cannot read", "machine", m.ID)
		} else if _, err := s.db.Exec(ctx, `INSERT INTO chat_shares (id, space_id, machine_id, owner_id, owner_email, sha256, size, title, expires_at, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (id) DO NOTHING`,
			out.Share.ID, m.SpaceID, m.ID, u.ID, strings.ToLower(strings.TrimSpace(u.Email)), sha, html.body.Len(), clip(out.Share.Title, 255), int64(out.Share.ExpiresAt), store.Ms(s.now())); err != nil {
			s.log.Warn("chat share: the link was not recorded", "machine", m.ID, "error", err)
		} else if s.cfg.ChatGuestLinks {
			var published map[string]json.RawMessage
			if json.Unmarshal(rec.body.Bytes(), &published) == nil {
				published["guestLinksAvailable"] = json.RawMessage("true")
				httpx.WriteJSON(w, http.StatusCreated, published)
				return
			}
		}
	}
	rec.replay(w)
}

// revokeChatShare ends a link: on the machine, and in the hub at once. Its author ends it with the machine away too.
func (s *Service) revokeChatShare(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine, id string) {
	ctx := r.Context()
	end := func(owner string) bool {
		q, args := `UPDATE chat_shares SET revoked_at = ? WHERE id = ? AND machine_id = ? AND revoked_at IS NULL`, []any{store.Ms(s.now()), id, m.ID}
		if owner != "" {
			q, args = q+` AND owner_id = ?`, append(args, owner)
		}
		res, err := s.db.Exec(ctx, q, args...)
		if err != nil {
			s.log.Warn("chat share: the link was not ended in the hub", "machine", m.ID, "error", err)
			return false
		}
		n, _ := res.RowsAffected()
		return n > 0
	}
	if s.reg.Get(m.ID) == nil {
		if end(u.ID) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		s.offline(w, r)
		return
	}
	rec := newCapRecorder(maxPage)
	s.forward(rec, r, u, m)
	if rec.status == http.StatusNoContent || rec.status == http.StatusOK {
		end("")
	}
	rec.replay(w)
}

// serveChatSharePage opens a published snapshot from the store to a signed-in person the access model lets read it
// (resaccess kind "chat"): the people of the snapshot's space. To anybody else it is not there.
func (s *Service) serveChatSharePage(w http.ResponseWriter, r *http.Request, p *auth.Principal, id string) {
	ctx := r.Context()
	st := s.uploadsStore()
	if st == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	var space, machine, sha string
	var expires int64
	var revoked sql.NullInt64
	err := s.db.QueryRow(ctx, `SELECT space_id, machine_id, sha256, expires_at, revoked_at FROM chat_shares WHERE id = ?`, id).Scan(&space, &machine, &sha, &expires, &revoked)
	if err != nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	// Whether the person is of the snapshot's space is the hub's own knowledge (may they open its machine); what that
	// gives them is the access model's word.
	_, away := s.admit(ctx, p.User.ID, machine)
	role, err := resaccess.New(s.db, s.now, "").Role(ctx, resaccess.Ref{Kind: resaccess.KindChat, ID: id, Space: space},
		resaccess.Actor{UserID: p.User.ID, Email: p.User.Email, Outside: away != nil})
	if err != nil || resaccess.Rank(role) == 0 {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	s.chatSnapshotBlob(w, r, st, space, sha, expires, revoked)
}

func (s *Service) chatSnapshotBlob(w http.ResponseWriter, r *http.Request, st Uploads, space, sha string, expires int64, revoked sql.NullInt64) {
	if revoked.Valid || expires <= store.Ms(s.now()) {
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "private, no-store")
		h.Set("Content-Security-Policy", chatShareCSP)
		w.WriteHeader(http.StatusGone)
		_, _ = io.WriteString(w, `<!doctype html><html lang="ru"><meta charset="utf-8"><title>Ссылка больше не действует</title><body style="font:16px/1.5 system-ui,sans-serif;display:grid;place-items:center;min-height:90vh;margin:0"><p>Ссылка отозвана или её срок вышел</p></body></html>`)
		return
	}
	h := w.Header()
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	err := blobserve.Serve(w, r, blobserve.Blob{Sha: sha, Open: func(ctx context.Context) (io.ReadCloser, error) {
		return st.OpenBlob(ctx, space, filestore.KindFiles, sha)
	}}, blobserve.Policy{Type: "text/html; charset=utf-8", Cache: blobserve.NoStore, CSP: chatShareCSP, NoIndex: true})
	switch {
	case errors.Is(err, blobserve.ErrNotFound):
		h.Del("X-Frame-Options")
		h.Del("Referrer-Policy")
		httpx.WriteError(w, r, httpx.ErrNotFound)
	case err != nil:
		s.fail(w, r, err)
	}
}
