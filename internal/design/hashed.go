package design

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// A heavy board asset (the 6 MB design-system bundle) lies in a project at several paths, and a browser keeps a file
// by its address. So a heavy file is addressed by what it holds, /__h/<hash>/<name>: the same bytes are one address
// across paths, boards and projects, fetched and compiled once.

const (
	hashedPrefix     = "/__h/"
	hashedMinBytes   = 64 * 1024
	hashedMaxEntries = 4096
	hashLen          = 32
)

type hashEntry struct {
	real, version string
}

// hashIndex answers only for files that a served page named: hash -> the files holding it. It lives in memory and
// forgets the oldest hash past max entries.
type hashIndex struct {
	mu     sync.Mutex
	max    int
	byHash map[string][]hashEntry
	order  *list.List               // hashes, oldest first
	nodes  map[string]*list.Element // hash -> its place in order
	sums   map[string]string        // real path + version -> hash
}

func (x *hashIndex) limit() int {
	if x.max > 0 {
		return x.max
	}
	return hashedMaxEntries
}

func (x *hashIndex) init() {
	if x.byHash == nil {
		x.byHash, x.order, x.nodes, x.sums = map[string][]hashEntry{}, list.New(), map[string]*list.Element{}, map[string]string{}
	}
}

func (x *hashIndex) size() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return len(x.byHash)
}

var (
	relativeSpecifier = regexp.MustCompile("\\b(?:from|import)\\s*[\"'`]\\.{1,2}/")
	dynamicImport     = regexp.MustCompile("\\bimport\\s*\\(\\s*[\"'`]\\.{1,2}/")
	metaURL           = regexp.MustCompile(`\bimport\.meta\.url`)
)

// selfContained: a script that names no neighbour. Served under /__h/<hash>/ every relative address in a file would
// resolve to a 404, so only a script without import.meta, dynamic import and relative static or side-effect imports
// (minified or not) may move there.
func selfContained(body []byte) bool {
	return !metaURL.Match(body) && !dynamicImport.Match(body) && !relativeSpecifier.Match(body)
}

// hashable: images and fonts always; a script when it is self-contained; never styles or pages (they carry relative
// urls and pages need withAssetVersions/withClient).
func hashable(ext string, body []byte) bool {
	switch {
	case imageExt[ext] || fontExt[ext]:
		return true
	case ext == ".js" || ext == ".mjs":
		return selfContained(body)
	}
	return false
}

// sum reads the file once and returns its hash, "" when it is not to be hashed, or unreadable, or changed while it was
// read (the bytes must be those of the stat'ed version).
func (x *hashIndex) sum(real string, st os.FileInfo) (string, bool) {
	body, err := os.ReadFile(real)
	if err != nil {
		return "", false
	}
	after, err := os.Stat(real)
	if err != nil || FileVersion(after) != FileVersion(st) || int64(len(body)) != st.Size() {
		return "", false
	}
	if !hashable(strings.ToLower(extname(real)), body) {
		return "", true
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:hashLen], true
}

// urlFor is the content address of a heavy file, and files it in the index; false for a small or unfit one.
func (x *hashIndex) urlFor(real string, st os.FileInfo) (string, bool) {
	if x == nil || st.Size() < hashedMinBytes || ContentType(extname(real)) == "" {
		return "", false
	}
	version := FileVersion(st)
	key := real + "\n" + version
	x.mu.Lock()
	x.init()
	hash, ok := x.sums[key]
	x.mu.Unlock()
	if !ok {
		var read bool
		if hash, read = x.sum(real, st); !read {
			return "", false
		}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.init()
	x.sums[key] = hash
	if hash == "" {
		return "", false
	}
	entry := hashEntry{real, version}
	have := false
	for _, e := range x.byHash[hash] {
		have = have || e == entry
	}
	if !have {
		x.byHash[hash] = append(x.byHash[hash], entry)
	}
	if el, ok := x.nodes[hash]; ok {
		x.order.MoveToBack(el)
	} else {
		x.nodes[hash] = x.order.PushBack(hash)
	}
	for len(x.byHash) > x.limit() {
		x.drop(x.order.Front().Value.(string))
	}
	return hashedPrefix + hash + "/" + EncodeURIComponent(filepath.Base(real)), true
}

func (x *hashIndex) drop(hash string) {
	for _, e := range x.byHash[hash] {
		delete(x.sums, e.real+"\n"+e.version)
	}
	delete(x.byHash, hash)
	if el, ok := x.nodes[hash]; ok {
		x.order.Remove(el)
		delete(x.nodes, hash)
	}
}

// lookup is a file that still holds what the hash names: one edited since the page named it is no longer there.
func (x *hashIndex) lookup(hash string) (string, bool) {
	real, _, ok := x.find(hash)
	return real, ok
}

// find is lookup with the stat it was decided on, so what is served is the version that was checked.
func (x *hashIndex) find(hash string) (string, os.FileInfo, bool) {
	if x == nil {
		return "", nil, false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	kept := x.byHash[hash][:0:0]
	found := ""
	var foundSt os.FileInfo
	for _, e := range x.byHash[hash] {
		st, err := os.Stat(e.real)
		if err != nil || !st.Mode().IsRegular() || FileVersion(st) != e.version {
			delete(x.sums, e.real+"\n"+e.version)
			continue
		}
		kept = append(kept, e)
		if found == "" {
			found, foundSt = e.real, st
		}
	}
	if len(kept) == 0 {
		if _, ok := x.byHash[hash]; ok {
			x.drop(hash)
		}
		return "", nil, false
	}
	x.byHash[hash] = kept
	return found, foundSt, true
}

// validHash: exactly the 32 lowercase hex of an address.
func validHash(s string) bool {
	if len(s) != hashLen {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func (l *Library) serveHashed(w http.ResponseWriter, r *http.Request, pathname string, head bool) {
	parts := strings.Split(strings.TrimPrefix(pathname, hashedPrefix), "/")
	if len(parts) != 2 || !validHash(parts[0]) || parts[1] == "" {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	real, st, ok := l.hashed.find(parts[0])
	if !ok {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	// The bytes of a project the person lost access to are not served at their hash either.
	if _, id, inProject := projectTop(real); inProject && !l.Can(id, RoleView) {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	ext := strings.ToLower(extname(real))
	if ContentType(ext) == "" {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	l.serveStatic(w, r, real, st, ContentType(ext), ext, immutable, head)
}
