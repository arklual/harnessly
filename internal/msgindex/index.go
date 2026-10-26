// Package msgindex is the full-text index over the messages of conversations: what the person wrote and what the agent
// answered, not the tool noise. It is an inverted index in memory over an append-only log in the runtime's data dir; the
// log is a cache of the transcripts, which stay the truth, so a log that is lost or damaged is built again.
//
// Words are lowercased with ё folded into е, a query word matches the words it is a prefix of, and a message must hold
// every word of the query. The text of a message stays in the log and is read for the few hits shown.
package msgindex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Roles of a message.
const (
	RoleUser  = "user"
	RoleAgent = "agent"
)

// Doc is one message.
type Doc struct {
	Conv  string `json:"c"`
	Entry string `json:"e"`
	At    int64  `json:"t"`
	Role  string `json:"r"`
	// Turn is the turn the message belongs to, "" for one outside any.
	Turn string `json:"u,omitempty"`
	Text string `json:"x"`
}

// Hit is a message found, with the words around the match.
type Hit struct {
	Conv    string
	Entry   string
	At      int64
	Role    string
	Turn    string
	Snippet string
}

type line struct {
	K string `json:"k"` // p: a message, d: a conversation forgotten, m: a conversation read up to a size
	Doc
	Size int64 `json:"s,omitempty"`
}

type doc struct {
	conv  string
	entry string
	at    int64
	role  string
	turn  string
	off   int64
	n     int32
	sum   uint64
	dead  bool
}

type key struct{ conv, entry string }

// Index is safe for concurrent use. A nil *Index finds nothing and keeps nothing.
type Index struct {
	mu     sync.RWMutex
	path   string
	f      *os.File
	end    int64
	docs   []doc
	byKey  map[key]uint32
	terms  map[string][]uint32
	sorted []string
	dirty  bool
	convs  map[string]*convState
	dead   int
}

type convState struct {
	ids  []uint32
	size int64
	read bool
}

// Open reads the log in dir and goes on appending to it. A log that cannot be read whole is built again from what
// is readable; an error means the index cannot be kept at all, and the caller goes without.
func Open(dir string) (*Index, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	ix := &Index{path: filepath.Join(dir, "messages.jsonl")}
	ix.reset()
	if err := ix.load(); err != nil {
		return nil, err
	}
	if ix.needsCompaction() {
		if err := ix.compactLocked(); err != nil {
			return nil, err
		}
	}
	f, err := os.OpenFile(ix.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	ix.f = f
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	ix.end = st.Size()
	return ix, nil
}

func (ix *Index) reset() {
	ix.docs, ix.byKey, ix.terms = nil, map[key]uint32{}, map[string][]uint32{}
	ix.sorted, ix.dirty, ix.convs, ix.dead = nil, false, map[string]*convState{}, 0
}

// Close releases the log.
func (ix *Index) Close() error {
	if ix == nil {
		return nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.f == nil {
		return nil
	}
	err := ix.f.Close()
	ix.f = nil
	return err
}

func (ix *Index) load() error {
	f, err := os.Open(ix.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var off int64
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 && raw[len(raw)-1] == '\n' {
			var l line
			if json.Unmarshal(raw, &l) == nil {
				ix.apply(l, off, int32(len(raw)))
			}
			off += int64(len(raw))
		}
		if err != nil {
			if err == io.EOF {
				// A last line without its newline was cut by a stop: it is dropped, the file is cut back to the last whole line.
				if len(raw) > 0 && raw[len(raw)-1] != '\n' {
					return os.Truncate(ix.path, off)
				}
				return nil
			}
			return err
		}
	}
}

func (ix *Index) state(conv string) *convState {
	s := ix.convs[conv]
	if s == nil {
		s = &convState{}
		ix.convs[conv] = s
	}
	return s
}

func sumOf(text string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(text))
	return h.Sum64()
}

// apply puts a line into memory; off and n say where it lies in the log.
func (ix *Index) apply(l line, off int64, n int32) {
	switch l.K {
	case "p":
		ix.addDoc(l.Doc, off, n)
	case "d":
		ix.forget(l.Conv)
	case "m":
		s := ix.state(l.Conv)
		s.size, s.read = l.Size, true
	}
}

func (ix *Index) addDoc(d Doc, off int64, n int32) {
	k := key{d.Conv, d.Entry}
	if old, ok := ix.byKey[k]; ok {
		ix.docs[old].dead = true
		ix.dead++
	}
	id := uint32(len(ix.docs))
	ix.docs = append(ix.docs, doc{conv: d.Conv, entry: d.Entry, at: d.At, role: d.Role, turn: d.Turn, off: off, n: n, sum: sumOf(d.Text)})
	ix.byKey[k] = id
	s := ix.state(d.Conv)
	s.ids = append(s.ids, id)
	for _, w := range Tokens(clip(d.Text)) {
		if _, ok := ix.terms[w]; !ok {
			ix.dirty = true
		}
		ix.terms[w] = append(ix.terms[w], id)
	}
}

func (ix *Index) forget(conv string) {
	s := ix.convs[conv]
	if s == nil {
		return
	}
	for _, id := range s.ids {
		if !ix.docs[id].dead {
			ix.docs[id].dead = true
			ix.dead++
			delete(ix.byKey, key{conv, ix.docs[id].entry})
		}
	}
	delete(ix.convs, conv)
}

func clip(text string) string {
	if len(text) <= TextMax {
		return text
	}
	rs := []rune(text)
	if len(rs) > TextMax {
		rs = rs[:TextMax]
	}
	return string(rs)
}

func (ix *Index) needsCompaction() bool { return ix.dead > 2000 && ix.dead > len(ix.docs)/2 }

// append writes lines to the log and puts them into memory.
func (ix *Index) append(ls []line) {
	if ix.f == nil {
		return
	}
	var buf bytes.Buffer
	type at struct {
		l   line
		off int64
		n   int32
	}
	var applied []at
	for _, l := range ls {
		start := buf.Len()
		b, err := json.Marshal(l)
		if err != nil {
			continue
		}
		buf.Write(b)
		buf.WriteByte('\n')
		applied = append(applied, at{l, ix.end + int64(start), int32(buf.Len() - start)})
	}
	if _, err := ix.f.Write(buf.Bytes()); err != nil {
		// The log is behind memory now; positions are no longer right, so the index stops growing until the next start.
		ix.f.Close()
		ix.f = nil
		return
	}
	ix.end += int64(buf.Len())
	for _, a := range applied {
		ix.apply(a.l, a.off, a.n)
	}
}

// Put keeps messages of one conversation, replacing those it has under the same entry; one that did not change is
// skipped.
func (ix *Index) Put(docs ...Doc) {
	if ix == nil || len(docs) == 0 {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.putLocked(docs, nil)
}

func (ix *Index) putLocked(docs []Doc, mark *line) {
	var ls []line
	for _, d := range docs {
		if strings.TrimSpace(d.Text) == "" {
			continue
		}
		d.Text = clip(d.Text)
		if id, ok := ix.byKey[key{d.Conv, d.Entry}]; ok && ix.docs[id].sum == sumOf(d.Text) && ix.docs[id].role == d.Role {
			continue
		}
		ls = append(ls, line{K: "p", Doc: d})
	}
	if mark != nil {
		ls = append(ls, *mark)
	}
	ix.append(ls)
	if ix.needsCompaction() {
		_ = ix.compactLocked()
	}
}

// Replace is what a reading of a whole conversation gives: its messages, and the size of the transcript they were read
// at. Messages the index holds that the reading no longer has are forgotten.
func (ix *Index) Replace(conv string, size int64, docs []Doc) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	have := map[string]struct{}{}
	for _, d := range docs {
		have[d.Entry] = struct{}{}
	}
	stale := false
	if s := ix.convs[conv]; s != nil {
		for _, id := range s.ids {
			if _, ok := have[ix.docs[id].entry]; !ix.docs[id].dead && !ok {
				stale = true
				break
			}
		}
	}
	if stale {
		ix.append([]line{{K: "d", Doc: Doc{Conv: conv}}})
	}
	ix.putLocked(docs, &line{K: "m", Doc: Doc{Conv: conv}, Size: size})
}

// Mark says that the messages of the conversation were taken up to size bytes of its transcript, without a message
// changing (live entries keep it current).
func (ix *Index) Mark(conv string, size int64) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if s := ix.convs[conv]; s != nil && s.read && s.size == size {
		return
	}
	ix.append([]line{{K: "m", Doc: Doc{Conv: conv}, Size: size}})
}

// Size is the transcript size the conversation was read at; false when it never was read whole.
func (ix *Index) Size(conv string) (int64, bool) {
	if ix == nil {
		return 0, false
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if s := ix.convs[conv]; s != nil && s.read {
		return s.size, true
	}
	return 0, false
}

// Read says whether the conversation was read whole at least once.
func (ix *Index) Read(conv string) bool { _, ok := ix.Size(conv); return ok }

// Remove forgets a conversation.
func (ix *Index) Remove(conv string) {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.convs[conv] == nil {
		return
	}
	ix.append([]line{{K: "d", Doc: Doc{Conv: conv}}})
}

// Count is how many messages the index holds.
func (ix *Index) Count() int {
	if ix == nil {
		return 0
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.docs) - ix.dead
}

// Options of a search.
type Options struct {
	// Limit is how many hits at most (default 40).
	Limit int
	// PerConv is how many hits a conversation gives at most (default 3).
	PerConv int
}

const maxExpansion = 400

func (ix *Index) sortedTerms() []string {
	if ix.dirty || ix.sorted == nil {
		// A new slice: a search that took the previous one is still reading it.
		fresh := make([]string, 0, len(ix.terms))
		for t := range ix.terms {
			fresh = append(fresh, t)
		}
		sort.Strings(fresh)
		ix.sorted = fresh
		ix.dirty = false
	}
	return ix.sorted
}

// prefixed is the postings of the words that begin with prefix, ascending.
func (ix *Index) prefixed(sorted []string, prefix string) []uint32 {
	i := sort.SearchStrings(sorted, prefix)
	var lists [][]uint32
	for n := 0; i < len(sorted) && strings.HasPrefix(sorted[i], prefix) && n < maxExpansion; i, n = i+1, n+1 {
		lists = append(lists, ix.terms[sorted[i]])
	}
	switch len(lists) {
	case 0:
		return nil
	case 1:
		return lists[0]
	}
	var all []uint32
	for _, l := range lists {
		all = append(all, l...)
	}
	slices.Sort(all)
	return slices.Compact(all)
}

func intersect(a, b []uint32) []uint32 {
	var out []uint32
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i] == b[j]:
			out = append(out, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return out
}

// Search finds the messages that hold every word of q (each a prefix of a word of the message), the newest first.
func (ix *Index) Search(q string, o Options) []Hit {
	if ix == nil {
		return nil
	}
	words := queryTokens(q)
	if len(words) == 0 {
		return nil
	}
	if o.Limit <= 0 {
		o.Limit = 40
	}
	if o.PerConv <= 0 {
		o.PerConv = 3
	}
	ix.mu.Lock()
	sorted := ix.sortedTerms()
	ix.mu.Unlock()
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	// sorted may have been rebuilt by another caller between the locks; it is only ever replaced under the write lock,
	// and a slice read here stays a valid list of words.
	lists := make([][]uint32, 0, len(words))
	for _, w := range words {
		l := ix.prefixed(sorted, w)
		if len(l) == 0 {
			return nil
		}
		lists = append(lists, l)
	}
	slices.SortFunc(lists, func(a, b []uint32) int { return len(a) - len(b) })
	found := lists[0]
	for _, l := range lists[1:] {
		found = intersect(found, l)
	}
	cands := make([]uint32, 0, len(found))
	for _, id := range found {
		if !ix.docs[id].dead {
			cands = append(cands, id)
		}
	}
	slices.SortFunc(cands, func(a, b uint32) int {
		switch {
		case ix.docs[a].at > ix.docs[b].at:
			return -1
		case ix.docs[a].at < ix.docs[b].at:
			return 1
		}
		return int(b) - int(a)
	})
	var out []Hit
	per := map[string]int{}
	for _, id := range cands {
		d := ix.docs[id]
		if per[d.conv] >= o.PerConv {
			continue
		}
		text, ok := ix.textOf(d)
		if !ok {
			continue
		}
		per[d.conv]++
		out = append(out, Hit{Conv: d.conv, Entry: d.entry, At: d.at, Role: d.role, Turn: d.turn, Snippet: Snippet(text, words)})
		if len(out) >= o.Limit {
			break
		}
	}
	return out
}

func (ix *Index) textOf(d doc) (string, bool) {
	if ix.f == nil {
		f, err := os.Open(ix.path)
		if err != nil {
			return "", false
		}
		defer f.Close()
		return readText(f, d)
	}
	return readText(ix.f, d)
}

func readText(f *os.File, d doc) (string, bool) {
	buf := make([]byte, d.n)
	if _, err := f.ReadAt(buf, d.off); err != nil {
		return "", false
	}
	var l line
	if json.Unmarshal(buf, &l) != nil {
		return "", false
	}
	return l.Text, true
}

// Messages are the messages of a conversation in the order they were written.
func (ix *Index) Messages(conv string) []Doc {
	if ix == nil {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	s := ix.convs[conv]
	if s == nil {
		return nil
	}
	ids := make([]uint32, 0, len(s.ids))
	for _, id := range s.ids {
		if !ix.docs[id].dead {
			ids = append(ids, id)
		}
	}
	slices.SortStableFunc(ids, func(a, b uint32) int {
		switch {
		case ix.docs[a].at < ix.docs[b].at:
			return -1
		case ix.docs[a].at > ix.docs[b].at:
			return 1
		}
		return 0
	})
	out := make([]Doc, 0, len(ids))
	for _, id := range ids {
		d := ix.docs[id]
		if text, ok := ix.textOf(d); ok {
			out = append(out, Doc{Conv: d.conv, Entry: d.entry, At: d.at, Role: d.role, Turn: d.turn, Text: text})
		}
	}
	return out
}

// Compact writes the log again with the live messages only.
func (ix *Index) Compact() error {
	if ix == nil {
		return nil
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.compactLocked()
}

func (ix *Index) compactLocked() error {
	var buf bytes.Buffer
	read := func(d doc) (string, bool) {
		if ix.f != nil {
			return readText(ix.f, d)
		}
		f, err := os.Open(ix.path)
		if err != nil {
			return "", false
		}
		defer f.Close()
		return readText(f, d)
	}
	for _, d := range ix.docs {
		if d.dead {
			continue
		}
		text, ok := read(d)
		if !ok {
			continue
		}
		b, _ := json.Marshal(line{K: "p", Doc: Doc{Conv: d.conv, Entry: d.entry, At: d.at, Role: d.role, Turn: d.turn, Text: text}})
		buf.Write(b)
		buf.WriteByte('\n')
	}
	for conv, s := range ix.convs {
		if s.read {
			b, _ := json.Marshal(line{K: "m", Doc: Doc{Conv: conv}, Size: s.size})
			buf.Write(b)
			buf.WriteByte('\n')
		}
	}
	tmp := ix.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, ix.path); err != nil {
		return err
	}
	if ix.f != nil {
		ix.f.Close()
		ix.f = nil
	}
	ix.reset()
	if err := ix.load(); err != nil {
		return err
	}
	f, err := os.OpenFile(ix.path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	ix.f, ix.end = f, st.Size()
	return nil
}

// Snippet is the words of text around the first word that one of the query words begins; whitespace is one space.
func Snippet(text string, words []string) string {
	const before, after = 50, 130
	rs := []rune(text)
	folded := make([]rune, len(rs))
	for i, r := range rs {
		folded[i] = foldRune(r)
	}
	at := -1
	for _, w := range wordsOf(folded) {
		word := string(folded[w.from:w.to])
		for _, q := range words {
			if strings.HasPrefix(word, q) {
				at = w.from
				break
			}
		}
		if at >= 0 {
			break
		}
	}
	if at < 0 {
		at = 0
	}
	from, to := max(0, at-before), min(len(rs), at+after)
	if from > 0 {
		for from < at && rs[from-1] != ' ' && rs[from-1] != '\n' {
			from++
		}
	}
	if to < len(rs) {
		for to > at && rs[to] != ' ' && rs[to] != '\n' {
			to--
		}
		if to == at {
			to = min(len(rs), at+after)
		}
	}
	s := strings.Join(strings.Fields(string(rs[from:to])), " ")
	if from > 0 {
		s = "…" + s
	}
	if to < len(rs) {
		s += "…"
	}
	return s
}
