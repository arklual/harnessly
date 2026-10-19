package limits

import (
	"cmp"
	"encoding/json"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Record is one line of limits-history.jsonl as the forecast reads it: its windows that are whole tuples of finite
// numbers. The windows are kept in the record itself and their names shared, so a scan over a week of lines stays in
// cache.
type Record struct {
	T float64
	P string
	W []NamedWindow
}

// NamedWindow is one window of a record.
type NamedWindow struct {
	ID string
	W  Window
}

// Get is the record's window of an id.
func (r Record) Get(id string) (Window, bool) {
	for _, w := range r.W {
		if w.ID == id {
			return w.W, true
		}
	}
	return Window{}, false
}

// Sample is one measurement of a window: epoch seconds, percent used, reset time.
type Sample struct {
	T, U, R float64
}

// HistoryReader keeps every record of the last eight days and reads only what was appended since its previous call. A
// file replaced (another inode) or cut shorter is read again from the start.
type HistoryReader struct {
	File string

	mu      sync.Mutex
	ino     uint64
	offset  int64
	carry   string
	records []Record
	loaded  bool
}

var (
	namesMu sync.Mutex
	names   = map[string]string{}
)

// intern is one shared string per window id.
func intern(id string) string {
	namesMu.Lock()
	defer namesMu.Unlock()
	if s, ok := names[id]; ok {
		return s
	}
	if len(names) < 4096 {
		names[id] = id
	}
	return id
}

func parseRecord(line string) (Record, bool) {
	if r, ok, sure := fastRecord(line); sure {
		return r, ok
	}
	return parseRecordGeneral(line)
}

func parseRecordGeneral(line string) (Record, bool) {
	var v map[string]any
	if json.Unmarshal([]byte(line), &v) != nil {
		return Record{}, false
	}
	p := v["p"]
	if !finite(v["t"]) || (p != "claude" && p != "codex") {
		return Record{}, false
	}
	r := Record{T: v["t"].(float64), P: p.(string)}
	switch w := v["w"].(type) {
	case map[string]any:
		r.W = make([]NamedWindow, 0, len(w))
	windows:
		for k, x := range w {
			a, ok := x.([]any)
			// A tuple short of its three numbers is no measurement (the writer never wrote one).
			if !ok || len(a) < 3 {
				continue
			}
			for _, n := range a {
				if !finite(n) {
					continue windows
				}
			}
			r.W = append(r.W, NamedWindow{intern(k), Window{a[0].(float64), a[1].(float64), a[2].(float64)}})
		}
	case []any:
	default:
		return Record{}, false
	}
	return r, true
}

// fastRecord reads a line in the shape the writer writes ({"t":…,"p":"…","w":{"id":[n,n,n],…}}, plain keys and numbers
// only) without the reflection of encoding/json. sure is false for anything else, which the general parser then reads.
func fastRecord(line string) (r Record, ok, sure bool) {
	sc := scanner{s: line}
	if !sc.eat('{') {
		return r, false, false
	}
	var t float64
	var p string
	hasT, hasP, hasW := false, false, false
	for first := true; ; first = false {
		if sc.eat('}') {
			if !first && !sc.trailingComma {
				break
			}
			return r, false, false
		}
		if !first && !sc.eat(',') {
			return r, false, false
		}
		key, kok := sc.plainString()
		if !kok || !sc.eat(':') {
			return r, false, false
		}
		switch key {
		case "t":
			n, nok := sc.number()
			if !nok {
				return r, false, false
			}
			t, hasT = n, true
		case "p":
			v, vok := sc.plainString()
			if !vok {
				return r, false, false
			}
			p, hasP = v, true
		case "w":
			if !sc.eat('{') {
				return r, false, false
			}
			r.W = r.W[:0]
			for wfirst := true; ; wfirst = false {
				if sc.eat('}') {
					if wfirst || !sc.trailingComma {
						break
					}
					return r, false, false
				}
				if !wfirst && !sc.eat(',') {
					return r, false, false
				}
				id, iok := sc.plainString()
				if !iok || !sc.eat(':') || !sc.eat('[') {
					return r, false, false
				}
				var nums [3]float64
				count := 0
				for nfirst := true; ; nfirst = false {
					if sc.eat(']') {
						if nfirst || !sc.trailingComma {
							break
						}
						return r, false, false
					}
					if !nfirst && !sc.eat(',') {
						return r, false, false
					}
					n, nok := sc.number()
					if !nok {
						return r, false, false
					}
					if count < 3 {
						nums[count] = n
					}
					count++
				}
				id = intern(id)
				kept := r.W[:0]
				for _, w := range r.W { // a repeated id keeps its last value
					if w.ID != id {
						kept = append(kept, w)
					}
				}
				r.W = kept
				if count >= 3 {
					r.W = append(r.W, NamedWindow{id, Window(nums)})
				}
			}
			hasW = true
		default:
			return r, false, false
		}
	}
	sc.space()
	if sc.i != len(sc.s) {
		return r, false, false
	}
	if !hasT || !hasP || !hasW || (p != "claude" && p != "codex") {
		return Record{}, false, true
	}
	r.T, r.P = t, p
	return r, true, true
}

type scanner struct {
	s             string
	i             int
	trailingComma bool // the last thing eaten was a comma
}

func (sc *scanner) space() {
	for sc.i < len(sc.s) && (sc.s[sc.i] == ' ' || sc.s[sc.i] == '\t' || sc.s[sc.i] == '\n' || sc.s[sc.i] == '\r') {
		sc.i++
	}
}

func (sc *scanner) eat(c byte) bool {
	sc.space()
	if sc.i < len(sc.s) && sc.s[sc.i] == c {
		sc.i++
		sc.trailingComma = c == ','
		return true
	}
	return false
}

// plainString is a string without escapes or control characters.
func (sc *scanner) plainString() (string, bool) {
	if !sc.eat('"') {
		return "", false
	}
	start := sc.i
	for sc.i < len(sc.s) {
		c := sc.s[sc.i]
		switch {
		case c == '"':
			sc.i++
			sc.trailingComma = false
			return sc.s[start : sc.i-1], true
		case c == '\\' || c < 0x20 || c >= 0x80:
			return "", false
		}
		sc.i++
	}
	return "", false
}

// number is a JSON number.
func (sc *scanner) number() (float64, bool) {
	sc.space()
	start := sc.i
	if sc.i < len(sc.s) && sc.s[sc.i] == '-' {
		sc.i++
	}
	digits := func() int {
		n := 0
		for sc.i < len(sc.s) && sc.s[sc.i] >= '0' && sc.s[sc.i] <= '9' {
			sc.i++
			n++
		}
		return n
	}
	intStart := sc.i
	intLen := digits()
	if intLen == 0 || (sc.s[intStart] == '0' && intLen > 1) {
		return 0, false
	}
	if sc.i < len(sc.s) && sc.s[sc.i] == '.' {
		sc.i++
		if digits() == 0 {
			return 0, false
		}
	}
	if sc.i < len(sc.s) && (sc.s[sc.i] == 'e' || sc.s[sc.i] == 'E') {
		sc.i++
		if sc.i < len(sc.s) && (sc.s[sc.i] == '+' || sc.s[sc.i] == '-') {
			sc.i++
		}
		if digits() == 0 {
			return 0, false
		}
	}
	tok := sc.s[start:sc.i]
	if len(tok) <= 15 && intLen == len(tok) { // a plain non-negative integer, exact in a float64
		n := 0.0
		for i := 0; i < len(tok); i++ {
			n = n*10 + float64(tok[i]-'0')
		}
		sc.trailingComma = false
		return n, true
	}
	f, err := strconv.ParseFloat(tok, 64)
	if err != nil || math.IsInf(f, 0) {
		return 0, false
	}
	sc.trailingComma = false
	return f, true
}

// Read is every record of the last eight days before now (epoch seconds). The slice is the reader's own: callers do
// not change it.
func (h *HistoryReader) Read(now float64) []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	st, err := os.Stat(h.File)
	if err != nil {
		return nil
	}
	var ino uint64
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		ino = uint64(sys.Ino)
	}
	if !h.loaded || h.ino != ino || st.Size() < h.offset {
		h.loaded, h.ino, h.offset, h.carry, h.records = true, ino, 0, "", nil
	}
	if st.Size() > h.offset {
		if f, err := os.Open(h.File); err == nil {
			buf := make([]byte, st.Size()-h.offset)
			n, _ := f.ReadAt(buf, h.offset)
			f.Close()
			buf = buf[:n]
			h.offset += int64(n)
			lines := strings.Split(h.carry+string(buf), "\n")
			h.carry = lines[len(lines)-1]
			for _, line := range lines[:len(lines)-1] {
				if line == "" {
					continue
				}
				if r, ok := parseRecord(line); ok {
					h.records = append(h.records, r)
				}
			}
		}
	}
	cutoff := now - HistoryKeepSec
	if len(h.records) > 0 && h.records[0].T < cutoff {
		kept := h.records[:0:0]
		for _, r := range h.records {
			if r.T >= cutoff {
				kept = append(kept, r)
			}
		}
		h.records = kept
	}
	return h.records
}

// SamplesOf is one window of one provider, sorted by measurement time, one sample per measurement (the last line that
// carries it).
func SamplesOf(records []Record, provider, windowID string) []Sample {
	out := make([]Sample, 0, len(records)/2)
	for _, r := range records {
		if r.P != provider {
			continue
		}
		if t, ok := r.Get(windowID); ok {
			at := t[2]
			if at == 0 {
				at = 0 // -0 and 0 are one moment, as one Map key
			}
			out = append(out, Sample{T: at, U: t[0], R: t[1]})
		}
	}
	byTime := func(a, b Sample) int { return cmp.Compare(a.T, b.T) }
	if !slices.IsSortedFunc(out, byTime) { // lines are appended in time order, so this is rare
		slices.SortStableFunc(out, byTime)
	}
	kept := out[:0]
	for i, s := range out {
		if i+1 < len(out) && out[i+1].T == s.T {
			continue
		}
		kept = append(kept, s)
	}
	return kept
}
