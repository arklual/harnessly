package designaccess

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// variantFile is the link record at the root of a conflict variant (filesync.VariantFile): the variant holds only its
// board, and the rest of the board's files (styles, scripts, pictures) stay in the project it was split from.
const variantFile = "variant.json"

// serveOrSource answers a file of the project; a file a conflict variant does not hold is answered from the project it
// was split from, when the person may open that one too. True when the answer came from that project: it
// is not decided by this project's revision alone, nor the same for everyone.
func (s *Service) serveOrSource(w http.ResponseWriter, r *http.Request, t *txn, u store.User, spaceID, project string,
	head int64, segs []string) (bool, error) {
	miss := &missWriter{w: w, header: http.Header{}}
	if err := s.view.Serve(miss, r, spaceID, project, head, segs, nil, false); err != nil || !miss.missed {
		return false, err
	}
	of, ofHead := s.variantSource(r.Context(), t, u, spaceID, project)
	if of == "" {
		miss.flush()
		return false, nil
	}
	return true, s.view.Serve(w, r, spaceID, of, ofHead, segs, nil, false)
}

// variantSource is the project a variant was split from and its head, when the person may open it; "" otherwise.
func (s *Service) variantSource(ctx context.Context, t *txn, u store.User, spaceID, project string) (string, int64) {
	lib, ok := s.view.(Librarian)
	if !ok {
		return "", 0
	}
	raw, err := lib.ReadFile(ctx, spaceID, project+"/"+variantFile, 64<<10)
	if err != nil || raw == nil {
		return "", 0
	}
	var link struct {
		Of string `json:"of"`
	}
	if json.Unmarshal(raw, &link) != nil || link.Of == project || !projectIDRe.MatchString(link.Of) {
		return "", 0
	}
	p, stored, err := t.effective(link.Of)
	if err != nil || !stored {
		return "", 0
	}
	if f, err := t.fold(p, u.Email); err != nil || f.Role == "" {
		return "", 0
	}
	head, err := s.view.Head(ctx, spaceID, link.Of)
	if err != nil || head == 0 {
		return "", 0
	}
	return link.Of, head
}

// missWriter passes an answer through unless it is a 404, which it keeps aside so another source can answer instead.
type missWriter struct {
	w      http.ResponseWriter
	header http.Header
	missed bool
	passed bool
	status int
	body   []byte
}

func (m *missWriter) Header() http.Header { return m.header }

func (m *missWriter) WriteHeader(code int) {
	if m.passed || m.missed {
		return
	}
	if code == http.StatusNotFound {
		m.missed, m.status = true, code
		return
	}
	m.passed = true
	for k, v := range m.header {
		m.w.Header()[k] = v
	}
	m.w.WriteHeader(code)
}

func (m *missWriter) Write(b []byte) (int, error) {
	if !m.passed && !m.missed {
		m.WriteHeader(http.StatusOK)
	}
	if m.missed {
		if len(m.body) < 4<<10 {
			m.body = append(m.body, b...)
		}
		return len(b), nil
	}
	return m.w.Write(b)
}

// flush gives the kept 404 as it was.
func (m *missWriter) flush() {
	for k, v := range m.header {
		m.w.Header()[k] = v
	}
	m.w.WriteHeader(m.status)
	_, _ = m.w.Write(m.body)
}
