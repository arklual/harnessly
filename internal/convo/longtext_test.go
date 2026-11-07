package convo

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/attach"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// keepingUploads is fakeUploads that keeps texts the runtime writes, as the machine's uploads do.
type keepingUploads struct {
	fakeUploads
	kept map[string]string
}

func (k keepingUploads) Keep(name string, body []byte) (wire.AttachmentRef, string, bool) {
	k.kept[name] = string(body)
	return wire.AttachmentRef{ID: "up_TXT", Name: name, Kind: wire.AttachmentFile, Mime: "text/markdown"}, "/data/uploads/up_TXT.md", true
}

// A long message is not pasted into the agent's box (Claude Code refuses some long pastes without a word): the
// agent gets its start and the file that holds all of it; the bubble has that file.
func TestALongMessageGoesToTheAgentAsAFile(t *testing.T) {
	r := newRig(t)
	up := keepingUploads{fakeUploads: fakeUploads{}, kept: map[string]string{}}
	r.rt = NewInline(Deps{MachineID: "m1", Publisher: r.pub, Machine: r.machine, Launcher: r.launcher, Clock: r.clock,
		Store: NewStore(filepath.Join(r.dir, "v2")), Limits: wire.DefaultLimits, Uploads: up})
	r.open()
	long := strings.Repeat("Строка длинного сообщения про очередь и поле ввода.\n", 600)
	res := r.do(wire.SendCmd{OpID: "m1", ClientID: "m1", ConversationID: r.id, Text: long, Mode: wire.ModeSteer,
		Kind: wire.KindPrompt})
	if res.R != wire.ResultQueued && res.R != wire.ResultAccepted {
		t.Fatalf("send: %+v", res)
	}
	if up.kept[longTextName] != long {
		t.Fatalf("the file does not hold the whole message: %d of %d", len(up.kept[longTextName]), len(long))
	}
	m := r.message("m1")
	if attach.IsLongText(m.Text) {
		t.Fatalf("the box would get a long paste still: %d", len(m.Text))
	}
	for _, want := range []string{"Строка длинного сообщения", "/data/uploads/up_TXT.md", `<ref kind="upload" id="up_TXT"`} {
		if !strings.Contains(m.Text, want) {
			t.Errorf("no %q in\n%s", want, m.Text)
		}
	}
	if len(m.Attachments) != 1 || m.Attachments[0].ID != "up_TXT" {
		t.Errorf("the bubble's files: %+v", m.Attachments)
	}
	// A short one goes as it is.
	r.do(wire.SendCmd{OpID: "m2", ClientID: "m2", ConversationID: r.id, Text: "коротко", Mode: wire.ModeQueue,
		Kind: wire.KindPrompt})
	if text := r.message("m2").Text; text != "коротко" {
		t.Errorf("a short message: %q", text)
	}
}
