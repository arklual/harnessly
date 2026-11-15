package docs

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// A person called by @Name in a comment on a board hears of it in the bell (docs/hub/docs.md «Mentions on a
// board»). The boards service names them in message.mentions by a stable id it keeps for each person who was on the
// board signed in — only those, so a mention never shows a board to someone who has not had it:
//
//   - hub:<user id>  the person signed in from the harnessly app view: their id here;
//   - mail:<hex>     HMAC-SHA256 of the lower-case e-mail keyed with the webhook secret: the hub finds its own user with
//     that e-mail; nobody without the secret can read it.
//
// The agent (`{agent: true}`), the author, a name without an id, a person the hub does not know or has switched off are
// not told. The notice: the board's name, the author and the comment's start, and a link that opens the board on the
// thread in the app (`/apps/harnessly.boards?o=<slug>&th=<thread>`); the key `mention:<slug>:<message>` never shows twice.

// Bell rings one hub user's bell: a notice without a chat, with a link; it reports whether the notice was put.
type Bell func(ctx context.Context, userID, email, key, title, text, link string) bool

const (
	mentionTextMax = 140
	mentionsPerMsg = 10
	boardsAppID    = "harnessly.boards"
)

type boardMention struct {
	Name  string `json:"name"`
	ID    string `json:"id"`
	Agent bool   `json:"agent"`
}

// mentionsOf reads message.mentions: objects since HRN-1469; an older boards service sent plain names, which name nobody.
func mentionsOf(raw json.RawMessage) []boardMention {
	if len(raw) == 0 {
		return nil
	}
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	out := []boardMention{}
	for _, it := range items {
		var m boardMention
		if json.Unmarshal(it, &m) == nil && m.ID != "" && !m.Agent {
			out = append(out, m)
		}
		if len(out) == mentionsPerMsg {
			break
		}
	}
	return out
}

// ringBoardMentions tells each person a comment calls. It never fails the event: a person not found is skipped.
func (s *Service) ringBoardMentions(ctx context.Context, ev boardEvent) int {
	if s.bell == nil || ev.Message == nil || (ev.Event != "thread.created" && ev.Event != "thread.replied") {
		return 0
	}
	msg := ev.Message
	mentions := mentionsOf(msg.Mentions)
	if len(mentions) == 0 || msg.ID == "" {
		return 0
	}
	var emails map[string]string // read once, only when a mail: id comes
	rung, seen := 0, map[string]bool{}
	for _, m := range mentions {
		if m.ID == msg.AuthorID {
			continue
		}
		userID, email := s.mentionedUser(ctx, m.ID, &emails)
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		_, slug := appOfKey(ev.Slug)
		title := "Упоминание " + placeOfKey(ev.Slug) + " «" + firstNonEmpty(clip(strings.TrimSpace(ev.Title), 80), slug) + "»"
		author := firstNonEmpty(strings.TrimSpace(msg.Author), "Кто-то")
		text := author + ": " + mentionLine(msg.Text)
		link := threadLink(ev.Slug, ev.Thread.ID)
		if s.bell(ctx, userID, email, "mention:"+ev.Slug+":"+msg.ID, title, text, link) {
			rung++
		}
	}
	return rung
}

// mentionedUser turns a person id of the boards service into a hub user that is not switched off; "" when none.
func (s *Service) mentionedUser(ctx context.Context, id string, emails *map[string]string) (string, string) {
	kind, value, ok := strings.Cut(id, ":")
	if !ok || value == "" {
		return "", ""
	}
	switch kind {
	case "hub":
		u, err := s.db.S().UserByID(ctx, value)
		if err != nil || u.DisabledAt != nil {
			return "", ""
		}
		return u.ID, strings.ToLower(u.Email)
	case "mail":
		secret := s.cfg.BoardEventsSecret
		want, err := hex.DecodeString(value)
		if secret == "" || err != nil || len(want) != sha256.Size {
			return "", ""
		}
		if *emails == nil {
			all, err := s.db.S().ActiveUserEmails(ctx)
			if err != nil {
				return "", ""
			}
			*emails = all
		}
		for uid, email := range *emails {
			e := strings.ToLower(strings.TrimSpace(email))
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write([]byte(e))
			if hmac.Equal(mac.Sum(nil), want) {
				return uid, e
			}
		}
	}
	return "", ""
}

// mentionLine is the comment's start on one line: what the bell shows under the title.
func mentionLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	if r := []rune(line); len(r) > mentionTextMax {
		return strings.TrimSpace(string(r[:mentionTextMax-1])) + "…"
	}
	return line
}
