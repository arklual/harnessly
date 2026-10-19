package usagereport

import (
	"bytes"
	"fmt"
	"math"
	"strconv"

	"harnessly/internal/jscompat"
	"harnessly/internal/nodejson"
)

// jw writes JSON as JSON.stringify does; the report and the facts file are large, so they are written directly.
type jw struct{ bytes.Buffer }

const hexDigits = "0123456789abcdef"

func (w *jw) str(s string) {
	w.WriteByte('"')
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			continue
		}
		w.WriteString(s[start:i])
		switch c {
		case '"':
			w.WriteString(`\"`)
		case '\\':
			w.WriteString(`\\`)
		case '\b':
			w.WriteString(`\b`)
		case '\f':
			w.WriteString(`\f`)
		case '\n':
			w.WriteString(`\n`)
		case '\r':
			w.WriteString(`\r`)
		case '\t':
			w.WriteString(`\t`)
		default:
			w.WriteString(`\u00`)
			w.WriteByte(hexDigits[c>>4])
			w.WriteByte(hexDigits[c&0xf])
		}
		start = i + 1
	}
	w.WriteString(s[start:])
	w.WriteByte('"')
}

func (w *jw) num(f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		w.WriteString("null")
		return
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		w.WriteString(strconv.FormatInt(int64(f), 10))
		return
	}
	w.WriteString(jscompat.Number(f))
}

func (w *jw) strPtr(s *string) {
	if s == nil {
		w.WriteString("null")
		return
	}
	w.str(*s)
}

func (w *jw) boolean(b bool) {
	if b {
		w.WriteString("true")
	} else {
		w.WriteString("false")
	}
}

// value is a JSON value decoded from a transcript (a key, a model).
func (w *jw) value(v any) {
	switch x := v.(type) {
	case nil:
		w.WriteString("null")
	case string:
		w.str(x)
	case float64:
		w.num(x)
	case bool:
		w.boolean(x)
	case []any:
		w.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				w.WriteByte(',')
			}
			w.value(e)
		}
		w.WriteByte(']')
	default:
		w.Write(nodejson.Marshal(x))
	}
}

func (w *jw) key(k string) {
	w.str(k)
	w.WriteByte(':')
}

func (w *jw) strings(list []string) {
	w.WriteByte('[')
	for i, s := range list {
		if i > 0 {
			w.WriteByte(',')
		}
		w.str(s)
	}
	w.WriteByte(']')
}

// A cached row is a JSON array: its key, then numbers.

func rowOf(raw []byte) (any, []float64) {
	el := elements(raw)
	if len(el) == 0 {
		return nil, nil
	}
	nums := make([]float64, 8)
	for i, e := range el[1:] {
		if i < len(nums) {
			nums[i], _ = decodeValue(e).(float64)
		}
	}
	return decodeValue(el[0]), nums
}

func stringsOf(raw []byte) []string {
	out := []string{}
	for _, e := range elements(raw) {
		s, _ := decodeValue(e).(string)
		out = append(out, s)
	}
	return out
}

func strOrNull(o object, k string) *string {
	if s, ok := o.str(k); ok {
		return &s
	}
	return nil
}

func eventsOf(raw []byte) []EventRow {
	out := []EventRow{}
	for _, r := range elements(raw) {
		k, n := rowOf(r)
		out = append(out, EventRow{Key: k, Ts: n[0]})
	}
	return out
}

// factsOf reads facts as the previous runtime stored them.
func factsOf(o object) *Facts {
	provider, _ := o.str("provider")
	session, _ := o.str("sessionId")
	f := newFacts(provider, session, o.isTrue("sidechain"))
	f.Title, f.FirstPrompt = strOrNull(o, "title"), strOrNull(o, "firstPrompt")
	for _, m := range elements(o.raw("models")) {
		f.Models = append(f.Models, decodeValue(m))
	}
	f.Projects, f.Tasks, f.PRs = stringsOf(o.raw("projects")), stringsOf(o.raw("tasks")), stringsOf(o.raw("prs"))
	f.FirstTs, f.LastTs = o.num("firstTs"), o.num("lastTs")
	for _, r := range elements(o.raw("messages")) {
		k, n := rowOf(r)
		f.Messages = append(f.Messages, MsgRow{Key: k, Ts: n[0], Model: int(n[1]), Project: int(n[2]), Input: n[3], Output: n[4],
			CacheRead: n[5], W5m: n[6], W1h: n[7]})
	}
	for _, r := range elements(o.raw("prompts")) {
		k, n := rowOf(r)
		f.Prompts = append(f.Prompts, PromptRow{Key: k, Ts: n[0], Project: int(n[1])})
	}
	for _, r := range elements(o.raw("turns")) {
		k, n := rowOf(r)
		f.Turns = append(f.Turns, TurnRow{Key: k, Ts: n[0], Ms: n[1]})
	}
	for _, r := range elements(o.raw("tools")) {
		k, n := rowOf(r)
		f.Tools = append(f.Tools, ToolRow{Key: k, Ts: n[0], Error: n[1]})
	}
	f.Interrupts, f.Denials, f.Compactions = eventsOf(o.raw("interrupts")), eventsOf(o.raw("denials")), eventsOf(o.raw("compactions"))
	return f
}

// write is the facts as JSON.stringify wrote them.
func (f *Facts) write(w *jw) {
	w.WriteString(`{"provider":`)
	w.str(f.Provider)
	w.WriteString(`,"sessionId":`)
	w.str(f.SessionID)
	w.WriteString(`,"sidechain":`)
	w.boolean(f.Sidechain)
	w.WriteString(`,"title":`)
	w.strPtr(f.Title)
	w.WriteString(`,"firstPrompt":`)
	w.strPtr(f.FirstPrompt)
	w.WriteString(`,"models":`)
	w.value(append([]any{}, f.Models...))
	w.WriteString(`,"projects":`)
	w.strings(f.Projects)
	w.WriteString(`,"tasks":`)
	w.strings(f.Tasks)
	w.WriteString(`,"prs":`)
	w.strings(f.PRs)
	w.WriteString(`,"firstTs":`)
	w.num(f.FirstTs)
	w.WriteString(`,"lastTs":`)
	w.num(f.LastTs)
	w.WriteString(`,"messages":[`)
	for i, m := range f.Messages {
		if i > 0 {
			w.WriteByte(',')
		}
		w.WriteByte('[')
		w.value(m.Key)
		for _, x := range []float64{m.Ts, float64(m.Model), float64(m.Project), m.Input, m.Output, m.CacheRead, m.W5m, m.W1h} {
			w.WriteByte(',')
			w.num(x)
		}
		w.WriteByte(']')
	}
	w.WriteString(`],"prompts":[`)
	for i, p := range f.Prompts {
		if i > 0 {
			w.WriteByte(',')
		}
		w.WriteByte('[')
		w.value(p.Key)
		w.WriteByte(',')
		w.num(p.Ts)
		w.WriteByte(',')
		w.num(float64(p.Project))
		w.WriteByte(']')
	}
	w.WriteString(`],"turns":[`)
	for i, t := range f.Turns {
		if i > 0 {
			w.WriteByte(',')
		}
		w.WriteByte('[')
		w.value(t.Key)
		w.WriteByte(',')
		w.num(t.Ts)
		w.WriteByte(',')
		w.num(t.Ms)
		w.WriteByte(']')
	}
	w.WriteString(`],"tools":[`)
	for i, t := range f.Tools {
		if i > 0 {
			w.WriteByte(',')
		}
		w.WriteByte('[')
		w.value(t.Key)
		w.WriteByte(',')
		w.num(t.Ts)
		w.WriteByte(',')
		w.num(t.Error)
		w.WriteByte(']')
	}
	for _, ev := range []struct {
		name string
		rows []EventRow
	}{{"interrupts", f.Interrupts}, {"denials", f.Denials}, {"compactions", f.Compactions}} {
		fmt.Fprintf(w, `],"%s":[`, ev.name)
		for i, e := range ev.rows {
			if i > 0 {
				w.WriteByte(',')
			}
			w.WriteByte('[')
			w.value(e.Key)
			w.WriteByte(',')
			w.num(e.Ts)
			w.WriteByte(']')
		}
	}
	w.WriteString(`]}`)
}
