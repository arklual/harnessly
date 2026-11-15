package designaccess

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// A board shown from the store runs in a sandbox of an opaque origin (isolated), so the browser asks for its own files
// (the engine's support.js, styles, pictures) as «cross-site» and without a Referer: the hub's cross-site refusal turned
// every one away and the board drew as raw markup. The board is therefore opened at an address that carries
// a key of its own, <project>/k/<key>/f/<file>: its relative files inherit the key with the path, and the proxy lets a
// cross-site read through only on such an address (proxy sandboxStoreFile). The key is the hub's MAC of the person and
// the project with its end, and it names the person: such a request carries no session either (the hub's cookie is
// SameSite=Lax, and a sandboxed frame's own requests are cross-site), so on hub.example.com every file of the board was
// 401. The proxy takes the person from a live key (KeyedFileUser) when there is no session, and the project's access is
// still checked as that person's, as for any other read.

const (
	fileKeyInfo = "harnessly-store-file/v1"
	fileKeyTTL  = 12 * time.Hour
	// fileKeyStep: the end is rounded up to it, so one person's key of a project stays the same address for hours and
	// the browser keeps the board's files (?tkv=… immutable) across openings instead of fetching them under a new key.
	fileKeyStep = 6 * time.Hour
)

type fileKeyer struct {
	key []byte
}

// newFileKeyer derives the files' key from the hub's master key; nil without one (the board is then served unkeyed).
func newFileKeyer(master []byte) *fileKeyer {
	if len(master) == 0 {
		return nil
	}
	key, err := hkdf.Key(sha256.New, master, []byte("harnessly"), fileKeyInfo, 32)
	if err != nil {
		return nil
	}
	return &fileKeyer{key: key}
}

// mint is the key of one person for one project: "<end, base 36>.<person>.<mac>"; "" without a key.
func (k *fileKeyer) mint(userID, project string, now time.Time) string {
	if k == nil || !keyIDShape(userID) {
		return ""
	}
	step := int64(fileKeyStep / time.Second)
	at := now.Add(fileKeyTTL).Unix()
	end := strconv.FormatInt((at+step-1)/step*step, 36)
	return end + "." + userID + "." + k.mac(userID, project, end)
}

// check says whether key is a live key of this person for this project.
func (k *fileKeyer) check(key, userID, project string, now time.Time) bool {
	who, ok := k.person(key, project, now)
	return ok && who == userID
}

// person is whose live key of the project key is.
func (k *fileKeyer) person(key, project string, now time.Time) (string, bool) {
	if k == nil || !storeKeyShape(key) {
		return "", false
	}
	end, rest, _ := strings.Cut(key, ".")
	who, sum, _ := strings.Cut(rest, ".")
	at, err := strconv.ParseInt(end, 36, 64)
	if err != nil || now.Unix() > at {
		return "", false
	}
	if !hmac.Equal([]byte(sum), []byte(k.mac(who, project, end))) {
		return "", false
	}
	return who, true
}

// keyedFile is a file at a board's keyed address: …/design/store/<project>/k/<key>/f/<file>.
var keyedFile = regexp.MustCompile(`/design/store/([^/]+)/k/([^/]+)/f/.`)

// KeyedFileUser is the person whose live key a GET of a board's file at its keyed address carries, "" for any other
// request: the proxy signs such a request in as them when it came without a session (the sandbox sends none).
func (s *Service) KeyedFileUser(r *http.Request) string {
	if s == nil || s.files == nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return ""
	}
	m := keyedFile.FindStringSubmatch(r.URL.Path)
	if m == nil || !projectIDRe.MatchString(m[1]) {
		return ""
	}
	who, ok := s.files.person(m[2], m[1], s.now())
	if !ok {
		return ""
	}
	return who
}

func (k *fileKeyer) mac(userID, project, end string) string {
	h := hmac.New(sha256.New, k.key)
	h.Write([]byte(userID + "\x00" + project + "\x00" + end))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)[:18])
}

// keyedLocation is where a navigation to <project>/f/<segs> goes to be opened with key: relative to the asked address,
// so the hub's prefix in front of it stays whatever the browser used.
func keyedLocation(segs []string, key, rawQuery string) string {
	esc := make([]string, len(segs))
	for i, s := range segs {
		esc[i] = url.PathEscape(s)
	}
	loc := strings.Repeat("../", len(segs)) + "k/" + key + "/f/" + strings.Join(esc, "/")
	if rawQuery != "" {
		loc += "?" + rawQuery
	}
	return loc
}

// storeNavigation: the browser opens the address as a page or a frame, not as a file of one.
func storeNavigation(r *http.Request) bool {
	return r.Header.Get("Sec-Fetch-Mode") == "navigate"
}

// storeKeyShape is the shape the proxy lets through (proxy storeKeyedPath): the end in base 36, a dot, the person's id,
// a dot, the MAC.
func storeKeyShape(key string) bool {
	end, rest, ok := strings.Cut(key, ".")
	if !ok || !keyIDShape(end) {
		return false
	}
	who, sum, ok := strings.Cut(rest, ".")
	if !ok || !keyIDShape(who) || sum == "" {
		return false
	}
	for _, c := range sum {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// keyIDShape: lower-case letters and digits, as the end in base 36 and the hub's ids (store.NewID) are.
func keyIDShape(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}
