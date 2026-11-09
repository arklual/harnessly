package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// HRN-879, spec v10: a tool's whole output is in the hub's copy too. The agent sends it apart from the stream, as the
// entry again with the whole output (internal/agent/replica_outputs.go); the copy keeps the longer output of one
// revision, a page carries its first toolOutputMax bytes as the machine's own pages do, and
// `…/entries/<id>/output` gives the rest.

// toolOutputMax is how much of a tool's output a page carries, in bytes (wire.ToolOutputMax).
const toolOutputMax = 4096

type toolOutput struct {
	Text      string          `json:"text"`
	Truncated json.RawMessage `json:"truncated"`
}

func outputOf(x obj) (toolOutput, bool) {
	var o toolOutput
	raw, ok := x["output"]
	if !ok || json.Unmarshal(raw, &o) != nil {
		return toolOutput{}, false
	}
	return o, true
}

// pageEntry is an entry as a page carries it: an output past toolOutputMax is cut on a character boundary and says how
// much of how much it shows.
func pageEntry(raw json.RawMessage) json.RawMessage {
	if len(raw) <= toolOutputMax {
		return raw
	}
	x, ok := object(raw)
	if !ok {
		return raw
	}
	o, ok := outputOf(x)
	if !ok || len(o.Text) <= toolOutputMax {
		return raw
	}
	cut := toolOutputMax
	for cut > 0 && !utf8.RuneStart(o.Text[cut]) {
		cut--
	}
	total := int64(len(o.Text))
	var had struct {
		Total int64 `json:"total"`
	}
	if json.Unmarshal(o.Truncated, &had) == nil && had.Total > total {
		total = had.Total
	}
	out, err := json.Marshal(map[string]any{"text": o.Text[:cut], "truncated": map[string]int64{"shown": int64(cut), "total": total}})
	if err != nil {
		return raw
	}
	x["output"] = out
	b, err := json.Marshal(x)
	if err != nil {
		return raw
	}
	return b
}

// keepLonger is the entry to store: when it repeats the stored revision with less of its output (an event or a page
// carries 4 KB), the stored output stays.
func keepLonger(ctx context.Context, q store.Q, o Owner, conv, id string, rev int64, x obj, raw json.RawMessage) json.RawMessage {
	in, ok := outputOf(x)
	if !ok || len(in.Text) < toolOutputMax-utf8.UTFMax {
		return raw
	}
	var haveRev int64
	var body string
	err := q.QueryRow(ctx, `SELECT rev, body FROM history_entries WHERE machine_id = ? AND conv_id = ? AND entry_id = ?`,
		o.MachineID, conv, id).Scan(&haveRev, &body)
	if err != nil || haveRev != rev {
		return raw
	}
	have, ok := object(json.RawMessage(body))
	if !ok {
		return raw
	}
	if kept, ok := outputOf(have); !ok || len(kept.Text) <= len(in.Text) {
		return raw
	}
	x["output"] = have["output"]
	b, err := json.Marshal(x)
	if err != nil {
		return raw
	}
	return b
}

// EntryOutput is the whole output of one tool entry of the person's chat, as `GET …/entries/<id>/output` gives it.
func EntryOutput(ctx context.Context, db *store.DB, userID, conv, entry string, machines []string) (json.RawMessage, error) {
	machine, _, _, err := head(ctx, db, userID, conv, machines)
	if err != nil {
		return nil, err
	}
	var body string
	err = db.QueryRow(ctx, `SELECT body FROM history_entries WHERE machine_id = ? AND conv_id = ? AND entry_id = ?`,
		machine, conv, entry).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	x, ok := object(json.RawMessage(body))
	if !ok {
		return nil, ErrNotFound
	}
	out, ok := x["output"]
	if !ok || string(out) == "null" {
		return nil, ErrNotFound
	}
	return json.Marshal(map[string]any{"entryId": entry, "output": out})
}
