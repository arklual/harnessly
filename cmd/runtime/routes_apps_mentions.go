package main

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// «@» of the chat's field asks the apps on in the chat (chat.mentions of their manifests): each one's tool is called
// from the machine with what is typed, all at once and each within mentionWait; an app that is slow, off, waiting for
// the person or answering something else is left out, and the palette lists the files and chats as before.
const (
	mentionWait     = 2 * time.Second
	mentionItems    = 8
	mentionQueryMax = 200
	mentionLabelMax = 80
	mentionDetail   = 160
	mentionInsert   = 2000
)

type mentionItem struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
	Insert string `json:"insert"`
}

type mentionGroup struct {
	App   string        `json:"app"`
	Title string        `json:"title"`
	Items []mentionItem `json:"items"`
}

// mentionItemsOf reads a tool's answer: structuredContent {items}, or the same JSON as the text of its first content.
func mentionItemsOf(raw json.RawMessage) []mentionItem {
	var res struct {
		IsError    bool `json:"isError"`
		Structured *struct {
			Items []mentionItem `json:"items"`
		} `json:"structuredContent"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &res) != nil || res.IsError {
		return nil
	}
	var items []mentionItem
	if res.Structured != nil {
		items = res.Structured.Items
	} else if len(res.Content) > 0 && res.Content[0].Type == "text" {
		var body struct {
			Items []mentionItem `json:"items"`
		}
		if json.Unmarshal([]byte(res.Content[0].Text), &body) != nil {
			return nil
		}
		items = body.Items
	}
	out := []mentionItem{}
	for _, it := range items {
		it.Label, it.Detail, it.Insert = clip(strings.TrimSpace(it.Label), mentionLabelMax), clip(strings.TrimSpace(it.Detail), mentionDetail), clip(it.Insert, mentionInsert)
		if it.Label == "" || strings.TrimSpace(it.Insert) == "" {
			continue
		}
		out = append(out, it)
		if len(out) == mentionItems {
			break
		}
	}
	return out
}

// clip keeps the first n characters.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func appMentionRoutes(d deps, reg *apps.Registry, chats appChats) []gate.Route {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			Conversation string `json:"conversation"`
			Query        string `json:"query"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b) != nil || utf8.RuneCountInString(b.Query) > mentionQueryMax {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
			return
		}
		rec, ok := chats.record(b.Conversation)
		if !ok {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		groups := make([]*mentionGroup, len(rec.Apps))
		var wg sync.WaitGroup
		for i, id := range rec.Apps {
			a, err := reg.Get(id)
			if err != nil || a.Manifest == nil || a.Manifest.Chat == nil || a.Manifest.Chat.Mentions == nil || a.Muted || a.Off || a.NeedsConsent() {
				continue
			}
			mn := *a.Manifest.Chat.Mentions
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(r.Context(), mentionWait)
				defer cancel()
				raw, err := callAppTool(ctx, d, reg, id, mn.Tool, map[string]any{"query": b.Query})
				if err != nil {
					return
				}
				if items := mentionItemsOf(raw); len(items) > 0 {
					groups[i] = &mentionGroup{App: id, Title: mn.Title, Items: items}
				}
			}()
		}
		wg.Wait()
		out := []mentionGroup{}
		for _, g := range groups {
			if g != nil {
				out = append(out, *g)
			}
		}
		// The chat may have turned the app off while it answered: only what is still on goes back.
		if now, ok := chats.record(b.Conversation); ok {
			out = slices.DeleteFunc(out, func(g mentionGroup) bool { return !slices.Contains(now.Apps, g.App) })
		}
		secretJSON(w, http.StatusOK, map[string]any{"groups": out})
	}
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(platformAPI + "/apps/mentions"), Handler: http.HandlerFunc(handler)},
		{Match: is("/agents" + platformAPI + "/apps/mentions"), Handler: http.HandlerFunc(handler)},
	}
}
