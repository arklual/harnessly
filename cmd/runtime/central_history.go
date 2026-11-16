package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/delegation"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const centralHistoryTimeout = 20 * time.Second

// centralConversationID accepts the stable hub URL as well as the raw conversation id.
func centralConversationID(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) == 3 && parts[0] == "agents" && parts[1] == "c" {
			ref = parts[2]
		} else {
			return "", fmt.Errorf("not a hub conversation URL")
		}
	}
	if !strings.HasPrefix(ref, "tab-") || len(ref) > 200 || strings.ContainsAny(ref, "/?# ") {
		return "", fmt.Errorf("not a conversation id or hub conversation URL")
	}
	return ref, nil
}

func (v *v2Live) centralHistoryRead(ctx context.Context, ref, before string, limit int) (string, error) {
	id, err := centralConversationID(ref)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = delegation.ReadDefault
	}
	if limit > 100 {
		limit = 100
	}
	status, raw, err := agent.CallLocal(nil, v.d.DataDir, os.Getenv("RUNTIME_AGENT_TOKEN_HEADER"), "/history/read", hubproto.HistoryRead{Conversation: id, Before: before, Limit: limit}, centralHistoryTimeout)
	if err != nil {
		return "", err
	}
	if status != 200 {
		var body struct {
			Error  string `json:"error"`
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &body)
		if body.Error == "" {
			body.Error = string(raw)
		}
		return "", fmt.Errorf("hub history %s", body.Error)
	}
	var page hubproto.HistoryPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return "", fmt.Errorf("hub returned invalid history: %w", err)
	}
	var rec wire.Conversation
	var snap wire.ConvSnapshot
	if json.Unmarshal(page.Conversation, &rec) != nil {
		return "", fmt.Errorf("hub returned an invalid conversation page")
	}
	for _, raw := range page.Turns {
		var turn wire.Turn
		if json.Unmarshal(raw, &turn) != nil {
			return "", fmt.Errorf("hub returned an invalid conversation page")
		}
		snap.Turns = append(snap.Turns, turn)
	}
	for _, raw := range page.Entries {
		var entry wire.Entry
		if json.Unmarshal(raw, &entry) != nil {
			return "", fmt.Errorf("hub returned an invalid conversation page")
		}
		snap.Entries = append(snap.Entries, entry)
	}
	rec.ID = id
	if page.BeforeTurnID != nil {
		snap.BeforeTurnID = page.BeforeTurnID
	}
	text := delegation.Transcript(rec, snap, limit)
	if page.BeforeTurnID != nil {
		text += "\nprevious: " + *page.BeforeTurnID + " (pass it as before to read the older page)\n"
	}
	if !page.Complete {
		text += "\nThe hub has not finished copying this chat yet.\n"
	}
	return text, nil
}
