package wire

import (
	"encoding/json"
	"testing"
)

// A record saved, or an ask sent, when ultracode was a level of the effort is read as the mode it was: xhigh with
// ultracode on, what `--effort ultracode` ran.
func TestAnEffortUltracodeOfBeforeIsReadAsTheMode(t *testing.T) {
	var conv struct {
		Model   *Model        `json:"model"`
		Pending *PendingModel `json:"pendingModel"`
		Log     []ModelSwitch `json:"modelLog"`
	}
	old := `{"model": {"id": "opus", "effort": "ultracode"},
		"pendingModel": {"id": "sonnet", "effort": "ultracode", "since": 1, "applying": false},
		"modelLog": [{"at": 1, "from": {"id": "opus", "effort": "high"}, "to": {"id": "opus", "effort": "ultracode"}}]}`
	if err := json.Unmarshal([]byte(old), &conv); err != nil {
		t.Fatal(err)
	}
	if m := conv.Model; *m.Effort != EffortXHigh || !m.Ultracode {
		t.Fatalf("model %+v", m)
	}
	if p := conv.Pending; *p.Effort != EffortXHigh || !p.Ultracode {
		t.Fatalf("pending %+v", p)
	}
	if to, from := conv.Log[0].To, conv.Log[0].From; *to.Effort != EffortXHigh || !to.Ultracode || from.Ultracode {
		t.Fatalf("log %+v %+v", from, to)
	}
	// What is written back is the new shape, and it passes the contract.
	raw, _ := json.Marshal(conv.Model)
	if string(raw) != `{"id":"opus","effort":"xhigh","ultracode":true}` {
		t.Fatalf("%s", raw)
	}
	if err := conv.Model.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAModelAskOfBeforeTurnsUltracodeOnAndOneOfNowMayLeaveItAsItIs(t *testing.T) {
	cmd, err := DecodeCommand(OpModel, []byte(`{"opId": "o", "id": "tab-a", "model": "opus", "effort": "ultracode"}`))
	if err != nil {
		t.Fatal(err)
	}
	m := cmd.(ModelCmd)
	if *m.Effort != EffortXHigh || m.Ultracode == nil || !*m.Ultracode {
		t.Fatalf("%+v", m)
	}
	cmd, err = DecodeCommand(OpModel, []byte(`{"opId": "o", "id": "tab-a", "model": "opus", "effort": "low"}`))
	if err != nil {
		t.Fatal(err)
	}
	m = cmd.(ModelCmd)
	if m.Ultracode != nil {
		t.Fatalf("%+v", m)
	}
	if !m.UltracodeAfter(&Model{ID: "opus", Ultracode: true}) || m.UltracodeAfter(&Model{ID: "opus"}) || m.UltracodeAfter(nil) {
		t.Fatal("an ask that does not say keeps the mode")
	}
	off := false
	if (ModelCmd{Ultracode: &off}).UltracodeAfter(&Model{Ultracode: true}) {
		t.Fatal("an ask that says off turns it off")
	}
	// Unknown fields are still refused: the decoding of the old shape is as strict as before.
	if _, err := DecodeCommand(OpModel, []byte(`{"opId": "o", "id": "tab-a", "model": "opus", "effort": null, "ultra": 1}`)); err == nil {
		t.Fatal("an unknown field passed")
	}
}
