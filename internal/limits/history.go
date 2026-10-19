package limits

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"harnessly/internal/jscompat"
	"harnessly/internal/nodejson"
)

// limits-history.jsonl (0600) holds every measurement of every window for the burn-rate forecast, one line per
// provider state worth keeping: {"t":<written at>,"p":"claude","w":{"five_hour":[used %, resets at, measured at],…}}.

// HistoryKeepSec is how much history the rotation keeps.
const HistoryKeepSec = 8 * 86400

// A flat stretch is data too, but one line a minute per provider is enough to see it.
const flatEverySec = 60

var extraID = regexp.MustCompile(`^[\w.-]{1,80}$`)

// Window is a history tuple: used %, resets at, measured at.
type Window [3]float64

// Windows keeps the order the tuples are written in.
type Windows struct {
	IDs    []string
	Tuples map[string]Window
}

func (w *Windows) add(id string, t Window) {
	if w.Tuples == nil {
		w.Tuples = map[string]Window{}
	}
	if _, has := w.Tuples[id]; !has {
		w.IDs = append(w.IDs, id)
	}
	w.Tuples[id] = t
}

func finite(v any) bool {
	f, ok := v.(float64)
	return ok && !math.IsNaN(f) && !math.IsInf(f, 0)
}

func tuple(v any, providerTs float64) (Window, bool) {
	w, ok := object(v)
	if !ok || !finite(w["used_percentage"]) || !finite(w["resets_at"]) {
		return Window{}, false
	}
	return Window{w["used_percentage"].(float64), w["resets_at"].(float64), windowAt(w, providerTs)}, true
}

// HistoryWindows is one provider as history tuples: 5 h, 7 d and codexbar's extra windows under their own ids.
func HistoryWindows(d Data) Windows {
	var out Windows
	ts := num(d["ts"])
	for _, k := range windowKeys {
		if t, ok := tuple(d[k], ts); ok {
			out.add(k, t)
		}
	}
	extra, _ := d["extra"].([]any)
	for _, item := range extra {
		m, _ := object(item)
		id, _ := m["id"].(string)
		if t, ok := tuple(m["window"], ts); ok && extraID.MatchString(id) {
			out.add(id, t)
		}
	}
	return out
}

type historyRecord struct {
	t float64
	w Windows
}

// ShouldRecord is whether a provider's new state is worth a line: some window was measured after the previous line,
// and either a value changed or that line is a minute old.
func ShouldRecord(prevT float64, prev *Windows, w Windows, now float64) bool {
	if len(w.IDs) == 0 {
		return false
	}
	if prev == nil {
		return true
	}
	measuredLater, changed := false, false
	for _, id := range w.IDs {
		p, has := prev.Tuples[id]
		var prevAt float64
		if has && !math.IsNaN(p[2]) { // an unknown measurement time reads as 0 (`?? 0`)
			prevAt = p[2]
		}
		t := w.Tuples[id]
		if t[2] > prevAt {
			measuredLater = true
		}
		if !has || t[0] != p[0] || t[1] != p[1] {
			changed = true
		}
	}
	return measuredLater && (changed || now-prevT >= flatEverySec)
}

// History appends to limits-history.jsonl. What it wrote last lives in memory only, so the first state after a start
// always gets a line.
type History struct {
	File string
	mu   sync.Mutex
	last map[string]historyRecord
}

func formatLine(t float64, provider string, w Windows) string {
	var b strings.Builder
	b.WriteString(`{"t":`)
	b.WriteString(jscompat.Number(t))
	b.WriteString(`,"p":`)
	b.Write(nodejson.Marshal(provider))
	b.WriteString(`,"w":{`)
	for i, id := range w.IDs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(nodejson.Marshal(id))
		b.WriteString(":[")
		for j, x := range w.Tuples[id] {
			if j > 0 {
				b.WriteByte(',')
			}
			if math.IsNaN(x) {
				b.WriteString("null")
			} else {
				b.WriteString(jscompat.Number(x))
			}
		}
		b.WriteByte(']')
	}
	b.WriteString("}}")
	return b.String()
}

// Record appends a line for every provider of the content whose state is worth one; now is epoch seconds.
func (h *History) Record(cache map[string]any, now float64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.last == nil {
		h.last = map[string]historyRecord{}
	}
	var lines []string
	for _, p := range Providers {
		d, ok := object(cache[p])
		if !ok {
			continue
		}
		w := HistoryWindows(d)
		var prev *Windows
		var prevT float64
		if r, has := h.last[p]; has {
			prev, prevT = &r.w, r.t
		}
		if !ShouldRecord(prevT, prev, w, now) {
			continue
		}
		t := math.Floor(now + 0.5)
		h.last[p] = historyRecord{t: t, w: w}
		lines = append(lines, formatLine(t, p, w))
	}
	if len(lines) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.File), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(h.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	return err
}

func recordTime(line string) (float64, bool) {
	var v map[string]any
	if json.Unmarshal([]byte(line), &v) != nil {
		return 0, false
	}
	p := v["p"]
	if !finite(v["t"]) || (p != "claude" && p != "codex") {
		return 0, false
	}
	switch v["w"].(type) {
	case map[string]any, []any:
	default:
		return 0, false
	}
	return v["t"].(float64), true
}

// RotateHistory keeps the last eight days; the file is replaced atomically and stays 0600. A missing file is left so.
func RotateHistory(file string, now float64) error {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if t, ok := recordTime(line); line != "" && ok && t >= now-HistoryKeepSec {
			kept = append(kept, line)
		}
	}
	body := ""
	if len(kept) > 0 {
		body = strings.Join(kept, "\n") + "\n"
	}
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
