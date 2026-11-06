package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The hub's environment list (docs/hub/environment.md): env.set brings the whole list of catalog items this machine's
// chats are to have. The agent keeps it in environment.json of the link's folder (atomically, so a crash leaves the old
// list or the new one), then applies it and tells the hub how each item went. Not to be mixed with env.deliver, which
// carries environment variables.
//
// Everything is applied into one folder the agent owns, <link folder>/environment/claude-plugin: a Claude Code plugin
// ("hub-environment") the runtime loads into every new chat with --plugin-dir. A skill with a body is its SKILL.md; a
// skill by a reference, a document and a repository become short skills telling the agent where the thing is and when
// to use it; MCP servers go to the plugin's .mcp.json, their secrets referenced by name from the machine's environment
// (${NAME}), never written. The folder is rewritten as a whole on every list, so an item that left the list leaves the
// chats too.

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
	plugin string // the plugin folder the list is applied into
	// hasSecret says whether a secret an MCP server names is on this machine; nil knows none.
	hasSecret func(name string) bool
	// setupAllowed says whether the machine may be set up by the list; nil allows (tests).
	setupAllowed func() bool
	// runnerAlive says the runtime that does the setup is running; nil says yes (tests). aliveSeen is what the watcher saw last.
	runnerAlive func() bool
	aliveSeen   bool

	mu   sync.Mutex
	held envHeld
}

func newEnvKeeper(dataDir string) *envKeeper {
	k := &envKeeper{path: filepath.Join(dataDir, envFile), plugin: filepath.Join(dataDir, hubproto.EnvPluginDir)}
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

// Reapply applies the held list again (a secret it names came or went) and says whether any item's outcome changed;
// the hub is told then.
func (k *envKeeper) Reapply() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.held.Rev == 0 {
		return false
	}
	now := k.apply(k.held.Items)
	if slices.Equal(now, k.held.Applied) {
		return false
	}
	k.held.Applied = now
	if err := k.saveLocked(); err != nil {
		Log("warn", "the environment file was not saved", F{"error", err.Error()})
	}
	return true
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
	skills := map[string]string{} // item id → SKILL.md
	servers := map[string]any{}   // server name → its .mcp.json entry
	kindOf := map[string]string{} // item id → kind
	var repos []string            // a line per repository, for the one skill of them all
	// The setup kinds (access, scripts, repositories) are done by the runtime; their outcome is what it wrote for this very list.
	setup := k.setupStatuses(hubproto.EnvHash(items))
	for _, it := range items {
		a := hubproto.EnvApplied{ItemID: it.ItemID, Status: hubproto.EnvOK}
		var src envSource
		switch {
		case !envItemID.MatchString(it.ItemID):
			a.Status, a.Message = hubproto.EnvError, "the item id is not a plain name"
		case len(it.Source) > envMaxSource:
			a.Status, a.Message = hubproto.EnvError, "the source is too large"
		case json.Unmarshal(it.Source, &src) != nil:
			a.Status, a.Message = hubproto.EnvError, "the source is not a JSON object"
		case it.Kind == hubproto.EnvKindSkill:
			a.Status, a.Message = putSkill(skills, it, src)
		case it.Kind == hubproto.EnvKindDoc:
			a.Status, a.Message = putDoc(skills, it, src)
		case it.Kind == hubproto.EnvKindRepo:
			var line string
			line, a.Status, a.Message = repoLine(it, src)
			if line != "" {
				repos = append(repos, line)
			}
		case it.Kind == hubproto.EnvKindMCP:
			a.Status, a.Message = k.putMCP(servers, it, src)
		case it.Kind == hubproto.EnvKindScript || it.Kind == hubproto.EnvKindAccess:
			// Nothing for the chats; the runtime's account below says how the machine's side went.
		default:
			a.Status, a.Message = hubproto.EnvError, "unknown kind "+it.Kind
		}
		if hubproto.IsSetupKind(it.Kind) && a.Status != hubproto.EnvError {
			if k.setupAllowed != nil && !k.setupAllowed() {
				// A repository keeps its reference for the chats, which needs no setup: its status stays what the chat part is.
				if it.Kind != hubproto.EnvKindRepo {
					a.Status, a.Message = hubproto.EnvPending, "настройка машины выключена на этой машине (политика setup)"
				}
			} else if k.runnerAlive != nil && !k.runnerAlive() {
				a.Status, a.Message = hubproto.EnvPending, "настройщик машины не запущен"
			} else if got, ok := setup[it.ItemID]; ok {
				a.Status, a.Message = got.Status, got.Message
			} else {
				a.Status, a.Message = hubproto.EnvPending, "машина ещё настраивается"
			}
		}
		kindOf[it.ItemID] = it.Kind
		out = append(out, a)
	}
	if len(repos) > 0 {
		skills[reposSkillID] = reposSkill(repos)
	}
	skillErr, mcpErr := k.write(skills, servers)
	for i, a := range out {
		err := skillErr[a.ItemID]
		if kindOf[a.ItemID] == hubproto.EnvKindRepo {
			err = skillErr[reposSkillID]
		}
		if kindOf[a.ItemID] == hubproto.EnvKindMCP && a.Status != hubproto.EnvError {
			err = mcpErr
		}
		if err != nil {
			out[i].Status, out[i].Message = hubproto.EnvError, "not written: "+err.Error()
		}
	}
	return out
}

// envSource is what any kind's source may hold (internal/hub/env/validate.go).
type envSource struct {
	Body       string   `json:"body"`
	Ref        string   `json:"ref"`
	URL        string   `json:"url"`
	Text       string   `json:"text"`
	Branch     string   `json:"branch"`
	Dir        string   `json:"dir"`
	Transport  string   `json:"transport"`
	Command    string   `json:"command"`
	Args       []string `json:"args"`
	SecretRefs []string `json:"secretRefs"`
}

// envSecretRef is how a secret is named; anything else would not be a variable of the environment.
var envSecretRef = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

func skillFile(it hubproto.EnvItem, description, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", yamlLine(it.ItemID), yamlLine(description), strings.TrimSpace(body))
}

func putSkill(skills map[string]string, it hubproto.EnvItem, src envSource) (string, string) {
	switch {
	case strings.TrimSpace(src.Body) != "":
		skills[it.ItemID] = skillFile(it, it.Name, src.Body)
	case src.Ref != "":
		skills[it.ItemID] = skillFile(it, it.Name, fmt.Sprintf("The full text of the skill «%s» is at %s. When the task is about it, read that page first and follow it.", it.Name, src.Ref))
	default:
		return hubproto.EnvError, "the skill has neither a body nor a reference"
	}
	return hubproto.EnvOK, ""
}

func putDoc(skills map[string]string, it hubproto.EnvItem, src envSource) (string, string) {
	desc := "The team's document «" + it.Name + "»: read it when the task touches it."
	switch {
	case strings.TrimSpace(src.Text) != "":
		skills[it.ItemID] = skillFile(it, desc, "# "+it.Name+"\n\n"+src.Text)
	case src.URL != "":
		skills[it.ItemID] = skillFile(it, desc, fmt.Sprintf("The team's document «%s» is at %s. Read it when the task touches what it is about, and follow it.", it.Name, src.URL))
	default:
		return hubproto.EnvError, "the document has neither a text nor an address"
	}
	return hubproto.EnvOK, ""
}

// reposSkillID is the one skill that tells the chats where all the team's repositories are. One skill and not one per
// repository: every skill's description is in every chat's prompt, and a team has dozens of repositories.
const reposSkillID = "team-repositories"

// repoLine is a repository's line in that skill.
func repoLine(it hubproto.EnvItem, src envSource) (string, string, string) {
	if src.URL == "" {
		return "", hubproto.EnvError, "the repository has no address"
	}
	branch := ""
	if src.Branch != "" {
		branch = fmt.Sprintf(", branch %s", src.Branch)
	}
	where := src.Dir
	if where == "" {
		where = "~/code/" + repoDirName(src.URL)
	}
	return fmt.Sprintf("- «%s»: `%s` — %s%s", it.Name, where, src.URL, branch), hubproto.EnvOK, ""
}

func reposSkill(lines []string) string {
	body := "The team's repositories and where the machine's setup clones them. Work in that folder; if it is not there yet, " +
		"clone it there (`git clone <address> <folder>`).\n\n" + strings.Join(lines, "\n")
	return skillFile(hubproto.EnvItem{ItemID: reposSkillID}, "The team's repositories: which folder each one is in and its address.", body)
}

func (k *envKeeper) putMCP(servers map[string]any, it hubproto.EnvItem, src envSource) (string, string) {
	for _, ref := range src.SecretRefs {
		if !envSecretRef.MatchString(ref) {
			return hubproto.EnvError, "a secret is not named like a variable: " + ref
		}
	}
	var entry map[string]any
	switch src.Transport {
	case "stdio", "":
		if src.Command == "" {
			return hubproto.EnvError, "the server has no command"
		}
		entry = map[string]any{"command": src.Command}
		if len(src.Args) > 0 {
			entry["args"] = src.Args
		}
		if len(src.SecretRefs) > 0 {
			vars := map[string]string{}
			for _, ref := range src.SecretRefs {
				vars[ref] = "${" + ref + ":-}"
			}
			entry["env"] = vars
		}
	case "http", "sse":
		if src.URL == "" {
			return hubproto.EnvError, "the server has no address"
		}
		entry = map[string]any{"type": src.Transport, "url": src.URL}
		switch len(src.SecretRefs) {
		case 0:
		case 1:
			entry["headers"] = map[string]string{"Authorization": "Bearer ${" + src.SecretRefs[0] + ":-}"}
		default:
			return hubproto.EnvPending, "an HTTP server with more than one secret is not started: which header takes which is not said"
		}
	default:
		return hubproto.EnvError, "unknown transport " + src.Transport
	}
	servers[mcpServerName(servers, it)] = entry
	var missing []string
	for _, ref := range src.SecretRefs {
		if k.hasSecret == nil || !k.hasSecret(ref) {
			missing = append(missing, ref)
		}
	}
	if len(missing) > 0 {
		return hubproto.EnvPending, "written; the secret " + strings.Join(missing, ", ") + " is not on this machine, so the server starts without it"
	}
	return hubproto.EnvOK, ""
}

// mcpServerName is the server's name in .mcp.json: the item's name made a plain word, or its id when that is taken.
func mcpServerName(servers map[string]any, it hubproto.EnvItem) string {
	var b strings.Builder
	for _, r := range strings.ToLower(it.Name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 40 {
		name = strings.Trim(name[:40], "-")
	}
	if _, taken := servers[name]; name == "" || taken {
		name = it.ItemID
	}
	return name
}

// envPluginManifest names the plugin; Claude shows its tools as mcp__plugin_hub-environment_<server>__<tool>.
const envPluginManifest = `{"name":"hub-environment","version":"1.0.0","description":"The team's environment from the hub: skills, documents, repositories and MCP servers."}` + "\n"

// write makes the plugin folder hold exactly skills and servers. It answers the skills it could not write, by id, and
// the error of .mcp.json; when the plugin's own file fails, everything fails with it.
func (k *envKeeper) write(skills map[string]string, servers map[string]any) (map[string]error, error) {
	failed := map[string]error{}
	if len(skills) == 0 && len(servers) == 0 {
		_ = os.RemoveAll(k.plugin) // nothing for the chats: no plugin, no empty folder loaded
		return failed, nil
	}
	if err := writeFileAtomic(filepath.Join(k.plugin, ".claude-plugin", "plugin.json"), []byte(envPluginManifest)); err != nil {
		for id := range skills {
			failed[id] = err
		}
		return failed, err
	}
	var mcpErr error
	mcp := filepath.Join(k.plugin, ".mcp.json")
	if len(servers) == 0 {
		_ = os.Remove(mcp)
	} else {
		raw, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
		if err == nil {
			err = writeFileAtomic(mcp, append(raw, '\n'))
		}
		mcpErr = err
	}
	for id, body := range skills {
		if err := writeFileAtomic(filepath.Join(k.plugin, "skills", id, "SKILL.md"), []byte(body)); err != nil {
			failed[id] = err
		}
	}
	k.prune(skills)
	return failed, mcpErr
}

// prune removes the skills of the plugin folder that are not in the list any more.
func (k *envKeeper) prune(keep map[string]string) {
	ents, err := os.ReadDir(filepath.Join(k.plugin, "skills"))
	if err != nil {
		return
	}
	for _, e := range ents {
		if _, ok := keep[e.Name()]; e.IsDir() && !ok && envItemID.MatchString(e.Name()) {
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

// repoDirName is the folder name the setup gives a repository by default: the last part of its address without .git.
func repoDirName(addr string) string {
	a := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(addr), "/"), ".git")
	if k := strings.LastIndexAny(a, "/:"); k >= 0 {
		a = a[k+1:]
	}
	if a == "" {
		return "repo"
	}
	return a
}
