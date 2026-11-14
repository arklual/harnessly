package designaccess

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A board shown from the store runs in a sandbox of an opaque origin (isolated), so the browser asks for its own files
// (the engine's support.js, styles, pictures) as «cross-site» and without a Referer: the hub's cross-site refusal turned
// every one away and the board drew as raw markup. The board is therefore opened at an address that carries
// a key of its own, <project>/k/<key>/f/<file>: its relative files inherit the key with the path, and the proxy lets a
// cross-site read through only on such an address (proxy sandboxStoreFile). The key is the hub's MAC of the person and
// the project with its end; it is checked here, after the person signed in, and the project's access is checked as for
// any other read.

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

// mint is the key of one person for one project: "<end, base 36>.<mac>"; "" without a key.
func (k *fileKeyer) mint(userID, project string, now time.Time) string {
	if k == nil {
		return ""
	}
	step := int64(fileKeyStep / time.Second)
	at := now.Add(fileKeyTTL).Unix()
	end := strconv.FormatInt((at+step-1)/step*step, 36)
	return end + "." + k.mac(userID, project, end)
}

// check says whether key is a live key of this person for this project.
func (k *fileKeyer) check(key, userID, project string, now time.Time) bool {
	if k == nil || !storeKeyShape(key) {
		return false
	}
	end, sum, ok := strings.Cut(key, ".")
	if !ok {
		return false
	}
	at, err := strconv.ParseInt(end, 36, 64)
	if err != nil || now.Unix() > at {
		return false
	}
	return hmac.Equal([]byte(sum), []byte(k.mac(userID, project, end)))
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

// storeKeyShape is the shape the proxy lets through (proxy storeKeyedPath): the end in base 36, a dot, the MAC.
func storeKeyShape(key string) bool {
	end, sum, ok := strings.Cut(key, ".")
	if !ok || end == "" || sum == "" {
		return false
	}
	for _, c := range end {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	for _, c := range sum {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
