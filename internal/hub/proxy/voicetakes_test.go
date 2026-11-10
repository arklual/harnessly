package proxy_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

const takesBase = "/agents/api/platform/voice/takes"

type takeT struct {
	ID    string  `json:"id"`
	At    int64   `json:"at"`
	Ms    int64   `json:"ms"`
	Mime  string  `json:"mime"`
	State string  `json:"state"`
	Text  *string `json:"text"`
	Audio bool    `json:"audio"`
}

func putTake(p *hubtest.Person, id, q string, audio []byte) hubtest.Resp {
	return p.Do("PUT", takesBase+"/"+id+q, audio, "Content-Type", "audio/webm;codecs=opus")
}

func listTakes(t *testing.T, p *hubtest.Person) []takeT {
	t.Helper()
	r := p.Do("GET", takesBase, nil)
	if r.Code != 200 {
		t.Fatalf("list: %s", r)
	}
	var out struct{ Takes []takeT }
	r.JSON(&out)
	return out.Takes
}

func TestVoiceTakesKeptPerPerson(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn, hubtest.Env("HUB_VOICE_TAKES", "on"))
	owner := s.SignIn("owner@example.com")
	other := s.SignIn("other@example.com")

	now := time.Now().UnixMilli()
	q1 := fmt.Sprintf("?machine=m1&at=%d&ms=2500&state=failed", now-5000)
	q2 := fmt.Sprintf("?machine=m1&at=%d&ms=900&state=done", now)
	audio := bytes.Repeat([]byte{1, 2, 3, 0xff}, 100)
	if r := putTake(owner, "t-1", q1, audio); r.Code != 200 {
		t.Fatalf("put: %s", r)
	}
	if r := putTake(owner, "t-2", q2, audio[:10]); r.Code != 200 {
		t.Fatalf("put 2: %s", r)
	}
	// Sent again: still one.
	if r := putTake(owner, "t-1", q1, audio); r.Code != 200 {
		t.Fatalf("put again: %s", r)
	}
	takes := listTakes(t, owner)
	if len(takes) != 2 || takes[0].ID != "t-2" || takes[1].ID != "t-1" || takes[1].State != "failed" || !takes[1].Audio || takes[1].Mime != "audio/webm" || takes[1].Ms != 2500 {
		t.Fatalf("takes %+v", takes)
	}

	// Past 30 days only the text is left.
	if r := putTake(owner, "t-old", "?machine=m1&at=1000&ms=10&state=done", audio); r.Code != 200 {
		t.Fatalf("put old: %s", r)
	}
	if old := listTakes(t, owner); len(old) != 3 || old[2].ID != "t-old" || old[2].Audio {
		t.Fatalf("old take kept its audio: %+v", old)
	}
	owner.Do("DELETE", takesBase+"/t-old", nil)

	// Recognized again: text and state.
	if r := owner.Do("PATCH", takesBase+"/t-1", map[string]any{"text": "привет", "state": "done"}); r.Code != 200 {
		t.Fatalf("patch: %s", r)
	}
	if got := listTakes(t, owner)[1]; got.State != "done" || got.Text == nil || *got.Text != "привет" {
		t.Fatalf("after patch %+v", got)
	}
	if r := owner.Do("PATCH", takesBase+"/t-1", map[string]any{"state": "wat"}); r.Code != 400 {
		t.Fatalf("bad state: %s", r)
	}

	r := owner.Do("GET", takesBase+"/t-1/audio", nil)
	if r.Code != 200 || !bytes.Equal(r.Body, audio) || r.Header.Get("Content-Type") != "audio/webm" {
		t.Fatalf("audio: %s %q", r, r.Header.Get("Content-Type"))
	}

	// Another person sees and touches none of it.
	if n := len(listTakes(t, other)); n != 0 {
		t.Fatalf("other sees %d", n)
	}
	if r := other.Do("GET", takesBase+"/t-1/audio", nil); r.Code != 404 {
		t.Fatalf("other audio: %s", r)
	}
	if r := other.Do("PATCH", takesBase+"/t-1", map[string]any{"text": "x"}); r.Code != 404 {
		t.Fatalf("other patch: %s", r)
	}
	other.Do("DELETE", takesBase+"/t-1", nil)
	if n := len(listTakes(t, owner)); n != 2 {
		t.Fatalf("other's delete reached the owner's takes: %d", n)
	}

	if r := owner.Do("DELETE", takesBase+"/t-1", nil); r.Code != 200 {
		t.Fatalf("delete: %s", r)
	}
	if n := len(listTakes(t, owner)); n != 1 {
		t.Fatalf("after delete %d", n)
	}
	if r := putTake(owner, "t-3", "?machine=m1&at=0&ms=1&state=done", audio); r.Code != http.StatusBadRequest {
		t.Fatalf("bad at: %s", r)
	}
}

func TestVoiceTakesOffByDefault(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	if r := owner.Do("GET", takesBase, nil); r.Code == 200 {
		t.Fatalf("the list answers with the flag off: %s", r)
	}
}
