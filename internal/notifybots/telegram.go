package notifybots

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAPI is Telegram's Bot API.
const DefaultAPI = "https://api.telegram.org"

// Reasons a call to Telegram fails, as the page and the agents are told them.
const (
	ReasonInvalidToken = "invalid-token" // Telegram does not know the token
	ReasonWebhook      = "webhook"       // the bot has a webhook: getUpdates is not allowed
	ReasonChatGone     = "chat-gone"     // the bot was removed from the chat or blocked by the person
	ReasonRateLimited  = "rate-limited"  // Telegram or this runtime asked to wait
	ReasonUnreachable  = "unreachable"   // no answer from Telegram
	ReasonRejected     = "rejected"      // Telegram refused the call for another reason
)

// APIError is a failed call to Telegram. Its text never holds the token: the request's address is never part of it.
type APIError struct {
	Reason string
	// Detail is Telegram's own description (or the network error without the address).
	Detail string
	// RetryAfter is Telegram's wait in seconds, MigrateTo a group's new id after it became a supergroup.
	RetryAfter int
	MigrateTo  int64
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return "telegram: " + e.Reason
	}
	return "telegram: " + e.Reason + ": " + e.Detail
}

// ReasonOf is the reason of a failed call; "" for an error that is not one.
func ReasonOf(err error) string {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Reason
	}
	return ""
}

// Client calls the Bot API.
type Client struct {
	// Base is the API's address (DefaultAPI when empty); tests put a fake server here.
	Base string
	HTTP *http.Client
}

func (c *Client) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return DefaultAPI
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

type apiAnswer struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter      int   `json:"retry_after"`
		MigrateToChatID int64 `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

// call posts one method with a JSON body and decodes its result into out.
func (c *Client) call(ctx context.Context, token, method string, body, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/bot"+token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		// The address holds the token: the error says only that it could not be made.
		return &APIError{Reason: ReasonInvalidToken, Detail: "malformed token"}
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client().Do(req)
	if err != nil {
		detail := "no answer"
		var ue *url.Error
		if errors.As(err, &ue) {
			detail = ue.Err.Error()
		}
		if ctx.Err() != nil {
			detail = ctx.Err().Error()
		}
		return &APIError{Reason: ReasonUnreachable, Detail: detail}
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return &APIError{Reason: ReasonUnreachable, Detail: "the answer broke off"}
	}
	var a apiAnswer
	if err := json.Unmarshal(data, &a); err != nil {
		return &APIError{Reason: ReasonRejected, Detail: fmt.Sprintf("HTTP %d", res.StatusCode)}
	}
	if !a.OK {
		return answerError(a, res.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(a.Result, out)
}

func answerError(a apiAnswer, status int) error {
	e := &APIError{Reason: ReasonRejected, Detail: a.Description, RetryAfter: a.Parameters.RetryAfter, MigrateTo: a.Parameters.MigrateToChatID}
	code := a.ErrorCode
	if code == 0 {
		code = status
	}
	desc := strings.ToLower(a.Description)
	switch {
	case code == http.StatusUnauthorized || code == http.StatusNotFound:
		e.Reason = ReasonInvalidToken
	case code == http.StatusConflict && strings.Contains(desc, "webhook"):
		e.Reason = ReasonWebhook
	case code == http.StatusTooManyRequests:
		e.Reason = ReasonRateLimited
	case code == http.StatusForbidden || strings.Contains(desc, "chat not found"):
		e.Reason = ReasonChatGone
	}
	return e
}

// Me is getMe's answer.
type Me struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

// GetMe checks a token: the bot it belongs to.
func (c *Client) GetMe(ctx context.Context, token string) (Me, error) {
	var me Me
	err := c.call(ctx, token, "getMe", map[string]any{}, &me)
	return me, err
}

// Update is the part of an update the binding reads.
type Update struct {
	UpdateID    int64    `json:"update_id"`
	Message     *Message `json:"message"`
	ChannelPost *Message `json:"channel_post"`
}

// Message is a message of an update.
type Message struct {
	Text string `json:"text"`
	Chat struct {
		ID        int64  `json:"id"`
		Type      string `json:"type"`
		Title     string `json:"title"`
		Username  string `json:"username"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
	} `json:"chat"`
}

// GetUpdates waits up to wait for new messages after offset.
func (c *Client) GetUpdates(ctx context.Context, token string, offset int64, wait time.Duration) ([]Update, error) {
	var out []Update
	body := map[string]any{"timeout": int(wait / time.Second), "allowed_updates": []string{"message", "channel_post"}}
	if offset != 0 {
		body["offset"] = offset
	}
	err := c.call(ctx, token, "getUpdates", body, &out)
	return out, err
}

// SendMessage sends one message; html says the text is Telegram HTML.
func (c *Client) SendMessage(ctx context.Context, token string, chat int64, text string, html, silent bool) error {
	body := map[string]any{"chat_id": chat, "text": text, "disable_notification": silent,
		"link_preview_options": map[string]any{"is_disabled": true}}
	if html {
		body["parse_mode"] = "HTML"
	}
	return c.call(ctx, token, "sendMessage", body, nil)
}
