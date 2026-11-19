package hubproto

import "encoding/json"

// The slot of the smart notice (docs/hub/notices.md): what apps and the agents of the chats ask the person on
// a machine. The runtime hands the open questions to the agent over its replica socket ({"k":"slot","notices":[…]}):
// all of them when the socket opens, then all of them again each time the slot moves. The agent forwards the newest
// list to the hub as a `noticeslot` event and sends it again when a link subscribes to the event; the hub keeps the
// questions in its database and tells the person's pages over its stream that the slot moved. The answers stay with
// the runtime. A question carries no secret: the slot never asks for one.
const (
	// EventNoticeSlot carries NoticeSlot.
	EventNoticeSlot = "noticeslot"
	// NoticeSlotMax is the most questions one event carries (the slot holds far fewer at once).
	NoticeSlotMax = 50
)

// NoticeSlot is the payload of EventNoticeSlot: every question open on the machine now.
type NoticeSlot struct {
	Notices []NoticeSlotItem `json:"notices"`
}

// NoticeSlotItem is one open question: its id and chat name it, Doc is the question as the slot lists it (without a
// view of its own).
type NoticeSlotItem struct {
	ID           string          `json:"id"`
	Conversation string          `json:"conversation,omitempty"`
	CreatedAt    int64           `json:"createdAt"`
	Doc          json.RawMessage `json:"doc"`
}
