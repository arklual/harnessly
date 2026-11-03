package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The hub's environment list (docs/hub/environment.md): env.set brings the whole list of catalog items this machine's
// chats are to have. The agent keeps it in environment.json of the data folder (atomically, so a crash leaves the old
// list or the new one), then applies what it has a place for and tells the hub how each item went. Not to be mixed with
// env.deliver, which carries environment variables.
//
// Only what the agent owns is written: <data folder>/environment/claude-plugin, a Claude Code plugin folder with the
// skills of the list. The runtime does not pass that folder to the chat yet, so an item written there is reported
// "pending" with the reason, not "ok": the hub never shows an item as in effect that no chat can see.

const (
	envMaxItems  = 500
	envMaxSource = 256 << 10
	envFile      = "environment.json"
)

// envItemID is what an item id looks like; it names a folder, so nothing else is let through.
var envItemID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// envHeld is the file: the list and how it went.
type envHeld struct {
	Rev     int64                 `json:"rev"`
	Hash    string                `json:"hash"`
	Items   []hubproto.EnvItem    `json:"items"`
	Applied []hubproto.EnvApplied `json:"applied"`
}

// envKeeper is the list the agent holds.
type envKeeper struct {
	path   string
	plugin string // the folder the skills are written to

	mu   sync.Mutex
	held envHeld
}

func newEnvKeeper(dataDir string) *envKeeper {
	k := &envKeeper{path: filepath.Join(dataDir, envFile), plugin: filepath.Join(dataDir, "environment", "claude-plugin")}
	raw, err := os.ReadFile(k.path)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		Log("warn", "the environment file is unreadable and is ignored until the hub sends a new list", F{"error", err.Error()})
	default:
		var h envHeld
		if err := json.Unmarshal(raw, &h); err != nil || hubproto.EnvHash(h.Items) != h.Hash {
			Log("warn", "the environment file is damaged and is ignored until the hub sends a new list")
		} else {
			k.held = h
		}
	}
	return k
}

// Held is the revision and the hash of the list held; 0 and "" for none.
func (k *envKeeper) Held() (int64, string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.held.Rev, k.held.Hash
}

// State is what the hub is told: the list held and how each item went.
func (k *envKeeper) State() hubproto.EnvState {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.stateLocked()
}

func (k *envKeeper) stateLocked() hubproto.EnvState {
	applied := k.held.Applied
	if applied == nil {
		applied = []hubproto.EnvApplied{}
	}
	return hubproto.EnvState{Rev: k.held.Rev, Hash: k.held.Hash, Applied: applied}
}

// Set keeps set and applies it. The list is on disk before it counts; an item that cannot be applied does not fail the
// others. A list whose hash is the one held is not applied again, only answered.
func (k *envKeeper) Set(set hubproto.EnvSet) (hubproto.EnvState, error) {
	if len(set.Items) > envMaxItems {
		return hubproto.EnvState{}, fmt.Errorf("too many items")
	}
	if hubproto.EnvHash(set.Items) != set.Hash {
		return hubproto.EnvState{}, fmt.Errorf("the hash is not the list's")
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if set.Hash == k.held.Hash && k.held.Rev != 0 {
		k.held.Rev = set.Rev // the same list under a newer revision
		if err := k.saveLocked(); err != nil {
			return hubproto.EnvState{}, err
		}
		return k.stateLocked(), nil
	}
	next := envHeld{Rev: set.Rev, Hash: set.Hash, Items: set.Items}
	next.Applied = k.apply(set.Items)
	old := k.held
	k.held = next
	if err := k.saveLocked(); err != nil {
		k.held = old
		return hubproto.EnvState{}, err
	}
	return k.stateLocked(), nil
}

func (k *envKeeper) saveLocked() error {
	raw, err := json.Marshal(k.held)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return err
	}
	tmp := k.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, k.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// apply puts each item where it belongs on this machine and says how it went, in the order of items.
func (k *envKeeper) apply(items []hubproto.EnvItem) []hubproto.EnvApplied {
	out := make([]hubproto.EnvApplied, 0, len(items))
	keep := map[string]bool{}
	for _, it := range items {
		a := hubproto.EnvApplied{ItemID: it.ItemID}
		switch {
		case !envItemID.MatchString(it.ItemID):
			a.Status, a.Message = hubproto.EnvError, "the item id is not a plain name"
		case len(it.Source) > envMaxSource:
			a.Status, a.Message = hubproto.EnvError, "the source is too large"
		case it.Kind == hubproto.EnvKindSkill:
			a.Status, a.Message = k.writeSkill(it, keep)
		case it.Kind == hubproto.EnvKindMCP:
			a.Status, a.Message = hubproto.EnvPending, "kept; the runtime has no way to start an MCP server for a chat yet"
		case it.Kind == hubproto.EnvKindRepo:
			a.Status, a.Message = hubproto.EnvPending, "kept; the runtime does not clone repositories for a chat yet"
		case it.Kind == hubproto.EnvKindDoc:
			a.Status, a.Message = hubproto.EnvPending, "kept; the runtime does not hand documents to a chat yet"
		default:
			a.Status, a.Message = hubproto.EnvError, "unknown kind "+it.Kind
		}
		out = append(out, a)
	}
	k.prune(keep)
	return out
}

// writeSkill writes a skill's body as <plugin>/skills/<id>/SKILL.md.
func (k *envKeeper) writeSkill(it hubproto.EnvItem, keep map[string]bool) (string, string) {
	var src struct {
		Body string `json:"body"`
		Ref  string `json:"ref"`
	}
	if err := json.Unmarshal(it.Source, &src); err != nil {
		return hubproto.EnvError, "the source is not a JSON object"
	}
	if src.Body == "" {
		return hubproto.EnvPending, "kept; a skill given by a reference is not fetched yet"
	}
	dir := filepath.Join(k.plugin, "skills", it.ItemID)
	body := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", yamlLine(it.ItemID), yamlLine(it.Name), src.Body)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return hubproto.EnvError, "the skill was not written: " + err.Error()
	}
	if err := writeFileAtomic(filepath.Join(dir, "SKILL.md"), []byte(body)); err != nil {
		return hubproto.EnvError, "the skill was not written: " + err.Error()
	}
	if err := writeFileAtomic(filepath.Join(k.plugin, ".claude-plugin", "plugin.json"), []byte(`{"name":"hub-environment","version":"0.0.0","description":"Skills from the hub's environment list."}`+"\n")); err != nil {
		return hubproto.EnvError, "the plugin file was not written: " + err.Error()
	}
	keep[it.ItemID] = true
	return hubproto.EnvPending, "written to the environment plugin folder; the runtime does not load it into a chat yet"
}

// prune removes the skills of the plugin folder that are not in the list any more.
func (k *envKeeper) prune(keep map[string]bool) {
	ents, err := os.ReadDir(filepath.Join(k.plugin, "skills"))
	if err != nil {
		return
	}
	for _, e := range ents {
		if e.IsDir() && !keep[e.Name()] && envItemID.MatchString(e.Name()) {
			_ = os.RemoveAll(filepath.Join(k.plugin, "skills", e.Name()))
		}
	}
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// yamlLine makes s safe as one scalar of a front matter line.
func yamlLine(s string) string {
	b, _ := json.Marshal(s) // a JSON string is a YAML double-quoted scalar
	return string(b)
}

func (h *hub) opEnvSet(req map[string]json.RawMessage) (any, func(), error) {
	rev, _ := field[int64](req, "rev")
	hash, _ := field[string](req, "hash")
	items, _ := field[[]hubproto.EnvItem](req, "items")
	if rev <= 0 {
		return nil, nil, invalid("rev must be a positive number")
	}
	st, err := h.environment.Set(hubproto.EnvSet{Rev: rev, Hash: hash, Items: items})
	if err != nil {
		return nil, nil, invalid("the environment list was not taken: %v", err)
	}
	h.audit.Record(hubproto.OpEnvSet, true, map[string]any{"rev": rev, "items": len(items)})
	return st, nil, nil
}
