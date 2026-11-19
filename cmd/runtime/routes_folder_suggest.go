package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/bgmodel"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

// folderSuggestBodyMax bounds a request: a few prompts and the folders with some titles.
const folderSuggestBodyMax = 64 << 10

// folderSuggestGap is the least time between two questions about one chat; folderSuggestHour caps all of them.
const (
	folderSuggestGap  = 30 * time.Second
	folderSuggestHour = 60
)

// folderSuggestGuard keeps a page from asking the model more often than it makes sense.
type folderSuggestGuard struct {
	mu   sync.Mutex
	last map[string]time.Time
	hour []time.Time
}

func (g *folderSuggestGuard) take(key string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.last == nil {
		g.last = map[string]time.Time{}
	}
	if at, ok := g.last[key]; ok && now.Sub(at) < folderSuggestGap {
		return false
	}
	kept := g.hour[:0]
	for _, at := range g.hour {
		if now.Sub(at) < time.Hour {
			kept = append(kept, at)
		}
	}
	g.hour = kept
	if len(g.hour) >= folderSuggestHour {
		return false
	}
	g.last[key] = now
	g.hour = append(g.hour, now)
	return true
}

// folderSuggestRoutes is the folder the app offers for a chat that has none:
//
//	POST /api/platform/folder-suggest {chat, prompts: [..], folders: [{id, name, titles: [..]}]}
//	  -> {folderId: "<id>" | null}
//
// The page asks after a few prompts of a chat without a folder; the answer is one of the folders it named, or none.
func folderSuggestRoutes(d deps) []gate.Route {
	runner := bgRunnerFor(d)
	guard := &folderSuggestGuard{}
	path := platformAPI + "/folder-suggest"
	handle := func(w http.ResponseWriter, r *http.Request) {
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
			Chat    string               `json:"chat"`
			Prompts []string             `json:"prompts"`
			Folders []bgmodel.FolderInfo `json:"folders"`
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, folderSuggestBodyMax))
		if err != nil || json.Unmarshal(raw, &b) != nil || strings.TrimSpace(b.Chat) == "" || len(b.Chat) > 200 ||
			len(b.Prompts) == 0 || len(b.Folders) == 0 {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		if !runner.Usable(bgmodel.FeatureFolder) {
			secretJSON(w, http.StatusConflict, map[string]string{"error": "no-model",
				"message": locale.Pick("No model can suggest a folder.", "Некому подсказать папку: нет модели.")})
			return
		}
		if !guard.take(b.Chat, time.Now()) {
			secretJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too-often"})
			return
		}
		id, _, err := runner.SuggestFolder(r.Context(), b.Prompts, b.Folders)
		if err != nil {
			message := locale.Pick("The model did not answer.", "Модель не ответила.")
			if last, ok := runner.Last()[bgmodel.FeatureFolder]; ok && last.Message != "" {
				message = last.Message
			}
			secretJSON(w, http.StatusBadGateway, map[string]string{"error": bgmodel.ReasonOf(err), "message": message})
			return
		}
		if id == "" {
			secretJSON(w, http.StatusOK, map[string]any{"folderId": nil})
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"folderId": id})
	}
	return []gate.Route{{Match: func(r *http.Request) bool { return pathIs(r, path) }, Handler: http.HandlerFunc(handle)}}
}
