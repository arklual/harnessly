package peers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPRelay is the way to the other machines through the fleet's gateway. Its contract (the gateway's side belongs to
// the fleet's deployment):
//
//	POST <URL>/send       Authorization: Bearer SignRelay(own secret, self); body Outbound
//	  the gateway checks the token with the secret of the machine it claims, finds the machine of Outbound.To in the
//	  fleet, and POSTs <runtime>/agents/api/peers/inbox with Authorization: Bearer SignInbox(that runtime's secret,
//	  sender) and the body Inbound{id, conversationId, fromConversationId, fromTitle, text, via}; it answers the
//	  runtime's Result as is, or {state: "refused", reason: "unreachable"} when the machine does not answer.
//	GET  <URL>/directory  the same token; the gateway asks GET /agents/api/peers/directory of every other runtime of
//	  the fleet with an inbox token and answers {chats: [Chat…]} — each runtime lists only what its allow-list lets the
//	  sender see.
type HTTPRelay struct {
	URL    string
	Secret []byte
	Self   Sender
	Client *http.Client
}

func (h *HTTPRelay) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (h *HTTPRelay) do(ctx context.Context, method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(h.URL, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+SignRelay(h.Secret, h.Self))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := h.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("relay answered %d", res.StatusCode)
	}
	return nil
}

// Send hands o to the gateway; a machine that cannot be reached is a refusal the sender sees, never an error.
func (h *HTTPRelay) Send(ctx context.Context, o Outbound) Result {
	var r Result
	if err := h.do(ctx, http.MethodPost, "/send", o, &r); err != nil || (r.State != StateDelivered && r.State != StateRefused) {
		detail := "no answer"
		if err != nil {
			detail = err.Error()
		}
		return refused(o.ID, RefuseUnreachable, detail)
	}
	return r
}

// Directory is the other machines' chats this machine may write to.
func (h *HTTPRelay) Directory(ctx context.Context) ([]Chat, error) {
	var out struct {
		Chats []Chat `json:"chats"`
	}
	if err := h.do(ctx, http.MethodGet, "/directory", nil, &out); err != nil {
		return nil, err
	}
	if out.Chats == nil {
		return nil, errors.New("relay answered no chats")
	}
	return out.Chats, nil
}
