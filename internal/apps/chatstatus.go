package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// The status of the document a chat works on, for its agent. A view bound to a document in a chat reads or
// moves the document's status (hl.workflow); the runtime keeps it per chat and app, and puts it in the folder of every
// launch of that chat as status.json, beside the launch's grant. That folder is a read-only bind inside the agent's wall,
// so the guard reads a status the agent cannot write; a launch made later gets the kept one. The agent is told the
// status at its next turn, and the app's rules by status (agent.tools.when) hold from the next call.

// ChatStatusFile is the name of the status file in a launch's folder.
const ChatStatusFile = "status.json"

// ChatDocStatus is the status of one app's document in a chat.
type ChatDocStatus struct {
	App     string `json:"app"`
	Doc     string `json:"doc"`
	Version int    `json:"version"`
	Status  string `json:"status"`
	// At is when it was noted (ms): past MaxChatDocs the oldest goes.
	At int64 `json:"at"`
}

// MaxChatDocs bounds the documents kept for one chat.
const MaxChatDocs = 32

// chatDocKey keys a document's status in a chat's record: a chat may work on several documents of one app, and each
// keeps its own (the guard takes the strictest of them).
func chatDocKey(app, doc string) string { return app + "/" + doc }

var chatIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func (r *Registry) chatStatusPath(conv string) string {
	return filepath.Join(r.Root, "chat-status", conv+".json")
}

// ChatStatuses are the statuses kept for a chat, by app and document; none for a chat that has none.
func (r *Registry) ChatStatuses(conv string) map[string]ChatDocStatus {
	out := map[string]ChatDocStatus{}
	if !chatIDRe.MatchString(conv) {
		return out
	}
	raw, err := os.ReadFile(r.chatStatusPath(conv))
	if err != nil || json.Unmarshal(raw, &out) != nil {
		return map[string]ChatDocStatus{}
	}
	return out
}

// NoteChatStatus keeps the status of an app's document in a chat and hands it to the chat's launches; it says whether
// it changed. A chat id that is not one is ignored.
func (r *Registry) NoteChatStatus(conv string, s ChatDocStatus) (bool, error) {
	if !chatIDRe.MatchString(conv) || s.App == "" || s.Doc == "" {
		return false, nil
	}
	all := r.ChatStatuses(conv)
	key := chatDocKey(s.App, s.Doc)
	if old, ok := all[key]; ok && old.Version == s.Version && old.Status == s.Status {
		return false, nil
	}
	all[key] = s
	for len(all) > MaxChatDocs {
		oldest := ""
		for k, v := range all {
			if oldest == "" || v.At < all[oldest].At {
				oldest = k
			}
		}
		delete(all, oldest)
	}
	raw, err := json.Marshal(all)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(r.chatStatusPath(conv)), 0o700); err != nil {
		return false, err
	}
	if err := writeFileAtomic(r.chatStatusPath(conv), raw); err != nil {
		return false, err
	}
	root := filepath.Join(filepath.Dir(r.Root), "app-launch")
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, own := r.Grant(e.Name()); own == conv {
			_ = writeFileAtomic(filepath.Join(root, e.Name(), ChatStatusFile), raw)
		}
	}
	return true, nil
}

// writeChatStatus puts a chat's kept statuses in a new launch's folder.
func writeChatStatus(sessionDir string, statuses map[string]ChatDocStatus) error {
	raw, err := json.Marshal(statuses)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(sessionDir, ChatStatusFile), raw)
}

// ReadChatStatus reads a launch's status file: the statuses of the chat's documents by app. Nothing there, or a file
// that is not one, is none.
func ReadChatStatus(path string) map[string][]string {
	out := map[string][]string{}
	raw, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var all map[string]ChatDocStatus
	if json.Unmarshal(raw, &all) != nil {
		return out
	}
	for _, s := range all {
		if s.App != "" && !slices.Contains(out[s.App], s.Status) {
			out[s.App] = append(out[s.App], s.Status)
		}
	}
	for _, list := range out {
		sort.Strings(list)
	}
	return out
}

// ChatStatusNotice is what the agent is told of the statuses of the chat's documents: the status, the version, and
// what the app's rules of that status keep the agent from. names gives an app's name and workflow.
func ChatStatusNotice(statuses map[string]ChatDocStatus, manifest func(app string) *Manifest) string {
	keys := make([]string, 0, len(statuses))
	for k := range statuses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var lines []string
	for _, k := range keys {
		s := statuses[k]
		m := manifest(s.App)
		if m == nil || m.Workflow == nil {
			continue
		}
		st, ok := m.Workflow.status(s.Status)
		if !ok {
			continue
		}
		line := "- " + m.Name + ": the document " + s.Doc + " is at version " + strconv.Itoa(s.Version) + ", status «" + st.Title + "» (" + st.ID + ")"
		if !st.Edit {
			line += "; it is not edited in this status: a change starts the next version as a draft"
		}
		if m.Agent != nil && m.Agent.Tools != nil {
			var rules []string
			for _, w := range m.Agent.Tools.When {
				if slices.Contains(w.Status, s.Status) {
					rules = append(rules, w.Deny...)
				}
			}
			if len(rules) > 0 {
				line += "; the app does not allow now: " + strings.Join(rules, ", ")
			}
		}
		lines = append(lines, line+".")
	}
	if len(lines) == 0 {
		return ""
	}
	return "Status of the documents this chat works on:\n" + strings.Join(lines, "\n")
}
