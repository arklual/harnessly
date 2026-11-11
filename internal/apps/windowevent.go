package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// The first time a person opens an app's window in a chat, the chat's agent is given a turn with an event of the app
// it answers in a line or two what it can do there. Once for an app in a chat, kept on disk so a restart of
// the runtime does not greet the open windows again.

// OpenEventTag is the envelope of the event in the message's text: the feed and the agent tell it from the person's
// own words by it.
const OpenEventTag = "app-event"

// OpenEvent is the text of the message that gives the agent its turn: the app, its name and the screen the window
// shows ("" when it named none).
func OpenEvent(app, name, screen string) string {
	plain := func(s string) string {
		return strings.NewReplacer("<", "", ">", "", `"`, "'").Replace(oneLine(s, windowBriefDesc))
	}
	where := ""
	if screen != "" {
		where = `, on the screen "` + plain(screen) + `"`
	}
	return "<" + OpenEventTag + ` app="` + app + `" event="opened">` + "\n" +
		"The person opened the app " + plain(name) + " in the right pane beside this chat" + where + ". " +
		"This is an event of the app, not the person's words. Answer in one or two lines what you can do for them here; " +
		"call no tool that changes anything until the person asks.\n" +
		"</" + OpenEventTag + ">"
}

// WindowOpens remembers the apps whose window was opened in each chat.
type WindowOpens struct {
	path string
	mu   sync.Mutex
	seen map[string][]string
}

var (
	windowOpensMu sync.Mutex
	windowOpens   = map[string]*WindowOpens{}
)

// OpenWindowOpens is the process's one WindowOpens of a data folder.
func OpenWindowOpens(dataDir string) *WindowOpens {
	path := filepath.Join(dataDir, "apps", "window-opens.json")
	windowOpensMu.Lock()
	defer windowOpensMu.Unlock()
	if o := windowOpens[path]; o != nil {
		return o
	}
	o := &WindowOpens{path: path}
	windowOpens[path] = o
	return o
}

// Claim marks the app's window as opened in the chat; true only the first time. A mark that could not be written is
// not a first time: better no greeting than one at every report.
func (o *WindowOpens) Claim(conv, app string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen == nil {
		o.seen = map[string][]string{}
		if raw, err := os.ReadFile(o.path); err == nil {
			_ = json.Unmarshal(raw, &o.seen)
		}
	}
	if slices.Contains(o.seen[conv], app) {
		return false
	}
	o.seen[conv] = append(o.seen[conv], app)
	raw, err := json.Marshal(o.seen)
	if err != nil {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(o.path), 0o700); err != nil {
		return false
	}
	return writeFileAtomic(o.path, raw) == nil
}
