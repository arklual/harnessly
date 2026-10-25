package wire

import (
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"
)

// The composers' drafts, kept on the machine so every device of the person sees what was typed or dictated. Each
// draft is a register with a revision: a write names the revision it makes (the current one + 1) and is refused with
// conflict otherwise, so a device that has not seen the last write (a clear made on another device, say) cannot put an
// older text back. A clear is a write of an empty text; the revision stays, so the empty draft is kept too.

// DraftNew is the key of the draft of the new-chat field, which has no conversation yet.
const DraftNew = "new"

// DraftTextMax is the longest text a draft keeps on the machine, in characters; a longer one stays on its device.
const DraftTextMax = 20000

// DraftsMax is how many drafts a machine keeps; past that the least recently written go first.
const DraftsMax = 500

// DraftAttachmentsMax is how many uploaded files a draft holds, as many as a message takes.
const DraftAttachmentsMax = 20

// Draft is the field of one composer: of a conversation (Key is its id) or of the new-chat field (DraftNew), its text
// and the files already uploaded for it (absent when none: a clear after a send empties both). Rev starts at 1 and
// grows by one with every write; DeviceID is the device that wrote it, At when (ms).
type Draft struct {
	Key         string          `json:"key"`
	Text        string          `json:"text"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	// Refs are the places the field points at (MessageRef); absent when none.
	Refs     []MessageRef `json:"refs,omitempty"`
	Rev      int64        `json:"rev"`
	DeviceID string       `json:"deviceId"`
	At       int64        `json:"at"`
}

// DraftUpsertEv is a draft after a write of it.
type DraftUpsertEv struct {
	Draft Draft `json:"draft"`
}

// DraftSetCmd writes a draft: Rev must be the draft's current revision + 1 (1 for a draft the machine does not have),
// else the answer is refused{conflict} and the page waits for the current draft on the convs channel.
type DraftSetCmd struct {
	OpID        string          `json:"opId"`
	Key         string          `json:"key"`
	Text        string          `json:"text"`
	Attachments []AttachmentRef `json:"attachments,omitempty"`
	Refs        []MessageRef    `json:"refs,omitempty"`
	Rev         int64           `json:"rev"`
}

// SameField reports whether the command writes what d already holds (text and files).
func (c DraftSetCmd) SameField(d Draft) bool {
	return c.Text == d.Text && slices.Equal(c.Attachments, d.Attachments) && slices.Equal(c.Refs, d.Refs)
}

// ValidDraftKey reports whether key names a draft: a conversation id or DraftNew.
func ValidDraftKey(key string) bool { return key == DraftNew || ValidConvID(key) }

func draftKey(name, key string) error {
	if !ValidDraftKey(key) {
		return fmt.Errorf("%s: %q is not a conversation id or %q", name, key, DraftNew)
	}
	return nil
}

func draftText(text string) error {
	if utf8.RuneCountInString(text) > DraftTextMax {
		return fmt.Errorf("text: longer than %d characters", DraftTextMax)
	}
	return nil
}

func draftFiles(files []AttachmentRef) error {
	if len(files) > DraftAttachmentsMax {
		return fmt.Errorf("attachments: more than %d", DraftAttachmentsMax)
	}
	return each("attachments", files)
}

func (d Draft) Validate() error {
	return errors.Join(draftKey("key", d.Key), draftText(d.Text), draftFiles(d.Attachments), refsValid(d.Refs), positive("rev", d.Rev),
		need("deviceId", d.DeviceID), nonNegative("at", d.At))
}

func (e DraftUpsertEv) Validate() error { return under("draft", e.Draft.Validate()) }

func (c DraftSetCmd) Validate() error {
	return errors.Join(need("opId", c.OpID), draftKey("key", c.Key), draftText(c.Text), draftFiles(c.Attachments),
		refsValid(c.Refs), positive("rev", c.Rev))
}
