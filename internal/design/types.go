package design

import (
	"net/url"
	"regexp"
	"strings"
)

var libraryIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// IsLibraryID: an artifact is addressed by its folder name in the library.
func IsLibraryID(v string) bool { return libraryIDRe.MatchString(v) }

const (
	LimitTitle         = 120
	LimitBoardPath     = 512
	LimitBoards        = 400
	LimitUploadBytes   = 300 * 1024 * 1024
	LimitUnpackedBytes = 1024 * 1024 * 1024
	LimitEntries       = 20000
)

// Board is a board of an artifact; Path is under project/, with the query the page needs. ID is the permanent id of
// its page in a project with meta/; "" for a board no page names.
type Board struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title"`
	Path  string `json:"path"`
	W     *int   `json:"w"`
	H     *int   `json:"h"`
}

// Agent is an agent conversation whose transcript touched the artifact's files.
type Agent struct {
	TabID       string   `json:"tabId"`
	Title       string   `json:"title"`
	Workspace   *string  `json:"workspace"`
	State       string   `json:"state"`
	LastEventAt *float64 `json:"lastEventAt"`
	Self        bool     `json:"self"`
}

// ClaimView is a claim on a board as the panel shows it.
type ClaimView struct {
	Board     string `json:"board"`
	Page      string `json:"page,omitempty"`
	Person    string `json:"person"`
	TabID     string `json:"tabId"`
	ChatTitle string `json:"chatTitle"`
	Since     int64  `json:"since"`
	Local     bool   `json:"local"`
	Inferred  bool   `json:"inferred"`
}

// CheckSummary is the last check of a design as a card shows it.
type CheckSummary struct {
	FinishedAt *int64  `json:"finishedAt"`
	Done       int     `json:"done"`
	Total      int     `json:"total"`
	Broken     int     `json:"broken"`
	Warnings   int     `json:"warnings"`
	Failure    *string `json:"failure"`
}

// Artifact is a project of the library.
type Artifact struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Source      *string       `json:"source"`
	UpdatedAt   int64         `json:"updatedAt"`
	Boards      []Board       `json:"boards"`
	HasSupport  bool          `json:"hasSupport"`
	ProjectPath string        `json:"projectPath"`
	Cover       *string       `json:"cover"`
	Check       *CheckSummary `json:"check"`
	Agents      []Agent       `json:"agents"`
	Claims      []ClaimView   `json:"claims"`
	// Release is the board format's release the project is on; absent when releases are not used.
	Release *ProjectRelease `json:"release,omitempty"`
}

// clone is a copy whose slices can be changed without touching a cached artifact.
func (a *Artifact) clone() *Artifact {
	c := *a
	c.Boards = append([]Board{}, a.Boards...)
	c.Agents = append([]Agent{}, a.Agents...)
	c.Claims = append([]ClaimView{}, a.Claims...)
	return &c
}

// EncodeURIComponent is JavaScript's encodeURIComponent.
func EncodeURIComponent(s string) string {
	const keep = "-_.!~*'()"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(keep, c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteString("%" + strings.ToUpper(hex2(c)))
	}
	return b.String()
}

func hex2(c byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[c>>4], digits[c&15]})
}

// decodeURIComponent is JavaScript's decodeURIComponent; ok false where it would throw.
func decodeURIComponent(s string) (string, bool) {
	if !strings.Contains(s, "%") {
		return s, true
	}
	out, err := url.PathUnescape(s)
	if err != nil || !validUTF8(out) {
		return "", false
	}
	return out, true
}

func encodeSegments(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = EncodeURIComponent(s)
	}
	return strings.Join(parts, "/")
}

// BoardHref is where a board lives on the library server: the path encoded per segment, the query as written.
func BoardHref(id, boardPath string) string {
	pathname, query := boardPath, ""
	if at := strings.IndexByte(boardPath, '?'); at >= 0 {
		pathname, query = boardPath[:at], boardPath[at:]
	}
	return "/a/" + id + "/p/" + encodeSegments(pathname) + query
}

// JavaScript's . stops at every line terminator, not only \n.
var pagePathRe = regexp.MustCompile(`^/a/([^/]+)/p/([^\n\r\x{2028}\x{2029}]+)$`)

// ParsePagePath is the artifact and the file a page of the library server shows: /a/<ref>/p/<path>.
func ParsePagePath(pagePath string) (ref, file string, ok bool) {
	pathname := pagePath
	if i := strings.IndexAny(pathname, "?#"); i >= 0 {
		pathname = pathname[:i]
	}
	m := pagePathRe.FindStringSubmatch(pathname)
	if m == nil {
		return "", "", false
	}
	ref, ok = decodeURIComponent(m[1])
	if !ok {
		return "", "", false
	}
	parts := strings.Split(m[2], "/")
	for i, s := range parts {
		if parts[i], ok = decodeURIComponent(s); !ok {
			return "", "", false
		}
	}
	if !IsLibraryID(ref) {
		return "", "", false
	}
	return ref, strings.Join(parts, "/"), true
}
