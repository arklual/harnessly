package wire

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// MessageStatusMax is how many client ids one GET .../messages asks about.
const MessageStatusMax = 20

// MessageStatusKind is what the runtime knows of a client id.
type MessageStatusKind string

const (
	// StatusKnown: the runtime has the message; Message is its record as the pages get it.
	StatusKnown MessageStatusKind = "known"
	// StatusUnknown: the runtime never had a message with this id (or lost it): it is the page's to send again.
	StatusUnknown MessageStatusKind = "unknown"
	// StatusExpired: the runtime had it and let its finished record go long ago (At is when it ended).
	StatusExpired MessageStatusKind = "expired"
)

// MessageStatusKinds is every MessageStatusKind, in the schema's order.
var MessageStatusKinds = []MessageStatusKind{StatusKnown, StatusUnknown, StatusExpired}

// HandedWhere is where a message handed to the agent is now.
type HandedWhere string

const (
	// HandedInputBox: typed into the agent's input box, not taken yet.
	HandedInputBox HandedWhere = "input-box"
	// HandedAgentQueue: typed into a working agent, which holds it in its own queue until its next step.
	HandedAgentQueue HandedWhere = "agent-queue"
	// HandedAgent: the agent took it (its prompt-submit hook), its transcript entry not read yet.
	HandedAgent HandedWhere = "agent"
	// HandedTranscript: the transcript has its entry (EntryID).
	HandedTranscript HandedWhere = "transcript"
)

// HandedWheres is every HandedWhere, in the schema's order.
var HandedWheres = []HandedWhere{HandedInputBox, HandedAgentQueue, HandedAgent, HandedTranscript}

// MessageHanded is when the message was typed into the agent and where it is now.
type MessageHanded struct {
	At      Ms          `json:"at"`
	Where   HandedWhere `json:"where"`
	EntryID *string     `json:"entryId"`
}

// MessageStatus is the runtime's word on one client id: known (with the record and, once typed, where it went),
// unknown, or expired (when it ended).
type MessageStatus struct {
	ClientID string            `json:"clientId"`
	K        MessageStatusKind `json:"k"`
	Message  *Message          `json:"message,omitempty"`
	Handed   *MessageHanded    `json:"handed,omitempty"`
	At       Ms                `json:"at,omitempty"`
}

// MessageStatuses is the answer of GET /api/v2/conversations/{id}/messages?ids=a,b: one status per id asked, in order.
type MessageStatuses struct {
	Messages []MessageStatus `json:"messages"`
}

func (h MessageHanded) Validate() error {
	errs := []error{positive("at", h.At), in("where", h.Where, HandedWheres), entryIDPtr("entryId", h.EntryID)}
	if (h.Where == HandedTranscript) != (h.EntryID != nil) {
		errs = append(errs, errors.New("entryId: only and always for transcript"))
	}
	return errors.Join(errs...)
}

func (s MessageStatus) Validate() error {
	errs := []error{need("clientId", s.ClientID), in("k", s.K, MessageStatusKinds)}
	switch s.K {
	case StatusKnown:
		if s.Message == nil || s.At != 0 {
			errs = append(errs, errors.New("known: a message and no at"))
		} else {
			errs = append(errs, under("message", s.Message.Validate()))
			if s.Message.ClientID != s.ClientID {
				errs = append(errs, errors.New("message.clientId: not the id asked"))
			}
		}
		if s.Handed != nil {
			errs = append(errs, under("handed", s.Handed.Validate()))
		}
	case StatusUnknown:
		if s.Message != nil || s.Handed != nil || s.At != 0 {
			errs = append(errs, errors.New("unknown: nothing but the id"))
		}
	case StatusExpired:
		if s.Message != nil || s.Handed != nil {
			errs = append(errs, errors.New("expired: no message"))
		}
		errs = append(errs, positive("at", s.At))
	}
	return errors.Join(errs...)
}

func (s MessageStatuses) Validate() error {
	errs := make([]error, 0, len(s.Messages))
	for i, m := range s.Messages {
		errs = append(errs, under(fmt.Sprintf("messages[%d]", i), m.Validate()))
	}
	return errors.Join(errs...)
}

// MessageIDs reads ?ids=a,b of a status request: 1–MessageStatusMax ids, none empty, none twice.
func MessageIDs(r *http.Request) ([]string, bool) {
	raw := r.URL.Query().Get("ids")
	if raw == "" {
		return nil, false
	}
	ids := strings.Split(raw, ",")
	if len(ids) > MessageStatusMax {
		return nil, false
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 128 || seen[id] {
			return nil, false
		}
		seen[id] = true
	}
	return ids, true
}

// messageStatus serves GET /api/v2/conversations/{id}/messages?ids=a,b. The answer is cut to the page's caps like
// its frames: a stalled message is failed{unconfirmed} for a page without CapMsgStatus.
func (s *Server) messageStatus(ctx context.Context, w http.ResponseWriter, r *http.Request, conv string) {
	ids, ok := MessageIDs(r)
	if !ok {
		refuse(w, RefuseBadRequest)
		return
	}
	if s.MessageStatus == nil {
		refuse(w, RefuseNotFound)
		return
	}
	out, err := s.MessageStatus(ctx, conv, ids)
	switch {
	case err != nil && s.NotFound != nil && s.NotFound(err):
		refuse(w, RefuseNotFound)
	case err != nil && ctx.Err() != nil:
		refuse(w, RefuseTimeout)
	case err != nil:
		jsonlog.Log("warn", "v2: message status failed", jsonlog.F{K: "path", V: r.URL.Path}, jsonlog.F{K: "error", V: err.Error()})
		refuse(w, RefuseInternal)
	default:
		if !CapsOf(r).MsgStatus {
			for i, m := range out.Messages {
				if m.Message != nil {
					old := messageWithoutStall(*m.Message)
					out.Messages[i].Message = &old
				}
			}
		}
		s.writeChecked(w, r, out)
	}
}
