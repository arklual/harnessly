package notifybots

import (
	"context"
	"time"
)

// The calls a question to a person needs beyond a notification: a message with buttons whose id is kept, the
// person's replies and button presses, and the edits that close the question in the chat (internal/asks).

// Button is one inline keyboard button; Data comes back in the press (64 bytes at most, Telegram's limit).
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data,omitempty"`
	URL  string `json:"url,omitempty"`
}

// User is the sender of a message or of a button press.
type User struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// FullMessage is a message with what a reply is matched by: its id, its sender and the message it replies to.
type FullMessage struct {
	MessageID int64  `json:"message_id"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
	Caption   string `json:"caption"`
	From      *User  `json:"from"`
	Chat      struct {
		ID    int64  `json:"id"`
		Type  string `json:"type"`
		Title string `json:"title"`
	} `json:"chat"`
	ReplyTo *struct {
		MessageID int64 `json:"message_id"`
		From      *User `json:"from"`
	} `json:"reply_to_message"`
}

// CallbackQuery is a press of an inline button.
type CallbackQuery struct {
	ID      string       `json:"id"`
	From    User         `json:"from"`
	Data    string       `json:"data"`
	Message *FullMessage `json:"message"`
}

// FullUpdate is an update with messages and button presses.
type FullUpdate struct {
	UpdateID      int64          `json:"update_id"`
	Message       *FullMessage   `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// GetFullUpdates waits up to wait for messages and button presses after offset.
func (c *Client) GetFullUpdates(ctx context.Context, token string, offset int64, wait time.Duration) ([]FullUpdate, error) {
	var out []FullUpdate
	body := map[string]any{"timeout": int(wait / time.Second), "allowed_updates": []string{"message", "callback_query"}}
	if offset != 0 {
		body["offset"] = offset
	}
	err := c.call(ctx, token, "getUpdates", body, &out)
	return out, err
}

// SendHTML sends one Telegram-HTML message, with rows of inline buttons when given, and returns its message id.
func (c *Client) SendHTML(ctx context.Context, token string, chat int64, html string, rows [][]Button, silent bool) (int64, error) {
	body := map[string]any{"chat_id": chat, "text": html, "parse_mode": "HTML", "disable_notification": silent,
		"link_preview_options": map[string]any{"is_disabled": true}}
	if len(rows) > 0 {
		body["reply_markup"] = map[string]any{"inline_keyboard": rows}
	}
	var m struct {
		MessageID int64 `json:"message_id"`
	}
	err := c.call(ctx, token, "sendMessage", body, &m)
	return m.MessageID, err
}

// EditHTML replaces a message's text and drops its buttons.
func (c *Client) EditHTML(ctx context.Context, token string, chat, message int64, html string) error {
	body := map[string]any{"chat_id": chat, "message_id": message, "text": html, "parse_mode": "HTML",
		"link_preview_options": map[string]any{"is_disabled": true}, "reply_markup": map[string]any{"inline_keyboard": [][]Button{}}}
	return c.call(ctx, token, "editMessageText", body, nil)
}

// AnswerCallback acknowledges a button press, with a short toast for the one who pressed it when text is set.
func (c *Client) AnswerCallback(ctx context.Context, token, id, text string) error {
	body := map[string]any{"callback_query_id": id}
	if text != "" {
		body["text"] = text
	}
	return c.call(ctx, token, "answerCallbackQuery", body, nil)
}
