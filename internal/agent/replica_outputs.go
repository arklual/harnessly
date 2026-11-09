package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// HRN-879, spec v10 («машина нужна только для живого»): an event and a page carry the first 4 KB of a tool's output
// (wire.ToolOutputMax), the rest stays on the machine and was read from there on «show all». The cluster's copy gets
// the whole output too: for every tool entry it sees cut, the agent reads the whole output from the runtime, apart from
// the stream and one at a time, and sends the entry again with it as a `feed_reset` of that one entry. The copy keeps
// the longer output of one revision and pages show 4 KB of it, as the machine does.

const (
	outputShipMax = 1 << 20 // the most of one output the copy gets; past it, it says how much it has of how much
	outputQueue   = 512     // outputs waiting to be read; one more is let go and read when it is seen again
	outputSeenMax = 20000   // entries remembered as sent; past it the memory starts again
)

type outputJob struct {
	conv  string
	id    string
	rev   int64
	entry json.RawMessage
}

// noteOutputs finds the tool entries of a chat's event or page whose output is cut, and queues each once per revision.
func (r *replica) noteOutputs(ch string, raw json.RawMessage) {
	conv, ok := strings.CutPrefix(ch, "conv:")
	if !ok || r.outQ == nil || (!bytes.Contains(raw, []byte(`"truncated":{`)) && !bytes.Contains(raw, []byte(`"truncated": {`))) {
		return
	}
	var ev struct {
		Entry   json.RawMessage   `json:"entry"`
		Entries []json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &ev) != nil {
		return
	}
	if ev.Entry != nil {
		ev.Entries = append(ev.Entries, ev.Entry)
	}
	for _, e := range ev.Entries {
		r.noteOutput(conv, e)
	}
}

func (r *replica) noteOutput(conv string, raw json.RawMessage) {
	var e struct {
		T      string `json:"t"`
		ID     string `json:"id"`
		Rev    int64  `json:"rev"`
		Output *struct {
			Truncated *json.RawMessage `json:"truncated"`
		} `json:"output"`
	}
	if json.Unmarshal(raw, &e) != nil || e.T != "tool" || e.ID == "" || e.Output == nil || e.Output.Truncated == nil {
		return
	}
	key := conv + "\x00" + e.ID
	r.outMu.Lock()
	if have, ok := r.outSeen[key]; ok && have >= e.Rev {
		r.outMu.Unlock()
		return
	}
	if len(r.outSeen) >= outputSeenMax {
		r.outSeen = map[string]int64{}
	}
	r.outSeen[key] = e.Rev
	r.outMu.Unlock()
	select {
	case r.outQ <- outputJob{conv: conv, id: e.ID, rev: e.Rev, entry: raw}:
	default:
		r.forgetOutput(conv, e.ID) // read when it is seen again
	}
}

func (r *replica) forgetOutput(conv, id string) {
	r.outMu.Lock()
	delete(r.outSeen, conv+"\x00"+id)
	r.outMu.Unlock()
}

// shipOutputs reads the queued outputs from the runtime one at a time, with a pause between, for as long as the agent runs.
func (r *replica) shipOutputs(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-r.outQ:
			r.shipOutput(j)
			select {
			case <-ctx.Done():
				return
			case <-time.After(r.pause):
			}
		}
	}
}

func (r *replica) shipOutput(j outputJob) {
	r.mu.Lock()
	on := r.enabled && r.connected()
	r.mu.Unlock()
	if !on || r.output == nil {
		r.forgetOutput(j.conv, j.id)
		return
	}
	raw, err := r.output(j.conv, j.id)
	if err != nil {
		r.forgetOutput(j.conv, j.id)
		Log("warn", "tool output not read for the cluster", F{"conv", j.conv}, F{"entry", j.id}, F{"error", err.Error()})
		return
	}
	ev, ok := wholeOutput(j.entry, raw)
	if !ok {
		return
	}
	r.emit(evV2Backfill{Event: "v2.backfill", Ch: "conv:" + j.conv, Events: []json.RawMessage{ev}})
}

// wholeOutput is the `feed_reset` of one entry with the output the runtime gave (GET …/entries/<id>/output), up to
// outputShipMax bytes cut on a character boundary.
func wholeOutput(entry, page json.RawMessage) (json.RawMessage, bool) {
	var p struct {
		Output struct {
			Text string `json:"text"`
		} `json:"output"`
	}
	var e map[string]json.RawMessage
	if json.Unmarshal(page, &p) != nil || json.Unmarshal(entry, &e) != nil {
		return nil, false
	}
	text := p.Output.Text
	out := map[string]any{"text": text, "truncated": nil}
	if len(text) > outputShipMax {
		cut := outputShipMax
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		out = map[string]any{"text": text[:cut], "truncated": map[string]int{"shown": cut, "total": len(text)}}
	}
	o, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	e["output"] = o
	ev, err := json.Marshal(map[string]any{"type": "feed_reset", "turns": []json.RawMessage{}, "entries": []any{e}, "beforeTurnId": nil})
	return ev, err == nil
}
