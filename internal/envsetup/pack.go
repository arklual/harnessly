package envsetup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// An environment pack (hubproto/pack.go, docs/hub/packs.md): the folder of a repository with harnessly/pack.json is
// installed as a Claude Code plugin of its own, <link>/environment/packs/<id>, which the runtime loads into the new
// chats beside the hub's environment plugin. The pack's version is in its plugin.json, in the mark file next to it and
// in the step's message, which is what the hub shows. A newer commit of the repository is a newer pack: nothing else
// is needed on the machine.

// packMark is PackMarkFile: which pack, of which item and version, and the hash of everything installed.
type packMark struct {
	ID      string `json:"id"`
	ItemID  string `json:"itemId"`
	Version string `json:"version"`
	Hash    string `json:"hash"`
}

func (ex *execution) packsDir() string {
	return filepath.Join(ex.r.LinkDir, filepath.FromSlash(hubproto.PacksDir))
}

func readMark(dir string) (packMark, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, hubproto.PackMarkFile))
	var m packMark
	if err != nil || json.Unmarshal(raw, &m) != nil {
		return m, false
	}
	return m, true
}

func (ex *execution) pack(i int) {
	it, src := ex.items[i], ex.srcs[i]
	if src.Path == "" {
		ex.fail(i, "не сказано, какая папка репозитория — пакет (path)")
		return
	}
	if src.Repo == "" && src.RepoURL == "" {
		ex.fail(i, "пакет берётся из репозитория: нужен repo из списка или repoUrl")
		return
	}
	if ex.sandbox {
		ex.done(i, hubproto.EnvSkipped, "в песочнице пакеты не ставятся")
		return
	}
	w, err := ex.locate(i, true)
	if err != nil {
		ex.fail(i, err.Error())
		return
	}
	defer w.cleanup()
	root := ex.hostPath(w.file)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(hubproto.PackManifestFile)))
	if err != nil {
		ex.fail(i, "в папке нет "+hubproto.PackManifestFile)
		return
	}
	m, err := hubproto.ParsePack(raw)
	if err != nil {
		ex.fail(i, err.Error())
		return
	}
	if other, ok := ex.packIDs[m.ID]; ok && other != it.ItemID {
		ex.fail(i, "пакет с id "+m.ID+" уже есть в списке (элемент "+other+")")
		return
	}
	if ex.packIDs == nil {
		ex.packIDs = map[string]string{}
	}
	ex.packIDs[m.ID] = it.ItemID

	files, hash, err := readPackTree(root, m)
	if err != nil {
		ex.fail(i, err.Error())
		return
	}
	dest := filepath.Join(ex.packsDir(), m.ID)
	label := m.ID + " " + m.Version
	have, ok := readMark(dest)
	same := ok && have.Hash == hash && have.ItemID == it.ItemID
	switch {
	case !ex.modifies && same:
		ex.done(i, hubproto.EnvSkipped, "стоит: "+label)
	case !ex.modifies:
		ex.pend(i, "не поставлен — поставится при обновлении: "+label)
	case same:
		ex.done(i, hubproto.EnvSkipped, "уже стоит: "+label)
	default:
		if err := installPack(dest, m, files, it.ItemID, hash); err != nil {
			ex.fail(i, "пакет не поставлен: "+err.Error())
			return
		}
		ex.done(i, hubproto.EnvOK, "поставлен: "+label)
	}
	ex.packSafe(i, m, label)
}

// packSafe asks the person for the secrets the pack names. The pack stays installed; the step waits (pending) until
// they are in the safe, and then says so.
func (ex *execution) packSafe(i int, m hubproto.PackManifest, label string) {
	if m.Safe == nil || ex.run.Steps[i].Status == hubproto.EnvError {
		return
	}
	var missing []string
	for _, s := range m.Safe.Secrets {
		if s.Optional || (ex.r.Secrets != nil && ex.r.Secrets.Has(s.Name)) {
			continue
		}
		missing = append(missing, s.Name)
		if ex.modifies && ex.r.Secrets != nil {
			ex.needSecret(s.Name, "Пакет окружения «"+m.Name+"»: "+s.Why)
		}
	}
	if len(missing) > 0 {
		ex.pend(i, label+": положите в сейф "+strings.Join(missing, ", "))
	}
}

// packFile is a file of the pack's folder to copy.
type packFile struct {
	rel  string // slash path in the pack
	data []byte
	mode fs.FileMode
}

// readPackTree reads the folder of a pack for copying: no links, no hidden folders, no more than the limits. hash is
// taken over the manifest, the names, the modes and the contents, so that any change shows.
func readPackTree(root string, m hubproto.PackManifest) ([]packFile, string, error) {
	var files []packFile
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." {
			return err
		}
		slash := filepath.ToSlash(rel)
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("в пакете ссылка %s: ссылки не берутся", slash)
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || slash == "harnessly" {
				return filepath.SkipDir // .git and the like; the manifest is read apart
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("в пакете особый файл %s", slash)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if len(files) >= hubproto.PackMaxFiles || total > hubproto.PackMaxBytes {
			return errors.New("пакет больше предела: 2000 файлов или 16 МБ")
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, packFile{rel: slash, data: data, mode: info.Mode().Perm()})
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	sort.Slice(files, func(a, b int) bool { return files[a].rel < files[b].rel })
	have := func(rel string) *packFile {
		for k := range files {
			if files[k].rel == rel {
				return &files[k]
			}
		}
		return nil
	}
	for _, t := range m.Tools {
		if have(t.Path) == nil {
			return nil, "", fmt.Errorf("инструмент %s: в пакете нет файла %s", t.Name, t.Path)
		}
	}
	if m.MCP != "" {
		f := have(m.MCP)
		if f == nil {
			return nil, "", fmt.Errorf("в пакете нет файла MCP %s", m.MCP)
		}
		var v struct {
			Servers map[string]json.RawMessage `json:"mcpServers"`
		}
		if json.Unmarshal(f.data, &v) != nil || len(v.Servers) == 0 {
			return nil, "", fmt.Errorf("%s: нужен объект {\"mcpServers\": {…}}", m.MCP)
		}
	}
	if m.Skills != "" && !slices.ContainsFunc(files, func(f packFile) bool { return strings.HasPrefix(f.rel, m.Skills+"/") }) {
		return nil, "", fmt.Errorf("в пакете нет папки скиллов %s", m.Skills)
	}
	h := sha256.New()
	h.Write(hubproto.CanonicalJSON(mustJSON(m)))
	for _, f := range files {
		fmt.Fprintf(h, "\x00%s\x00%o\x00", f.rel, f.mode)
		h.Write(f.data)
	}
	return files, hex.EncodeToString(h.Sum(nil)), nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// installPack writes the plugin of a pack next to its place and swaps it in, so that a chat never meets a half folder.
func installPack(dest string, m hubproto.PackManifest, files []packFile, itemID, hash string) error {
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
	next, err := os.MkdirTemp(parent, "."+m.ID+"-new-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(next)
	put := func(rel string, data []byte, mode fs.FileMode) error {
		p := filepath.Join(next, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return err
		}
		return os.WriteFile(p, data, mode)
	}
	byRel := map[string]packFile{}
	for _, f := range files {
		byRel[f.rel] = f
	}
	for _, f := range files {
		// The folder as it is, but for what the plugin makes itself.
		if f.rel == ".mcp.json" || f.rel == "hooks/hooks.json" || strings.HasPrefix(f.rel, ".claude-plugin/") || strings.HasPrefix(f.rel, "bin/") {
			continue
		}
		if err := put(f.rel, f.data, f.mode|0o600); err != nil {
			return err
		}
	}
	for _, t := range m.Tools {
		if err := put("bin/"+t.Name, byRel[t.Path].data, 0o755); err != nil {
			return err
		}
	}
	if m.MCP != "" {
		if err := put(".mcp.json", byRel[m.MCP].data, 0o600); err != nil {
			return err
		}
	}
	if m.Skills != "" && m.Skills != "skills" {
		for _, f := range files {
			if rest, ok := strings.CutPrefix(f.rel, m.Skills+"/"); ok {
				if err := put("skills/"+rest, f.data, f.mode|0o600); err != nil {
					return err
				}
			}
		}
	}
	if len(m.Hooks) > 0 {
		if err := put("hooks/hooks.json", packHooksJSON(m), 0o600); err != nil {
			return err
		}
	}
	manifest := map[string]string{"name": hubproto.PackPluginPrefix + m.ID, "version": m.Version, "description": m.Name + " — пакет окружения"}
	if err := put(".claude-plugin/plugin.json", append(mustJSON(manifest), '\n'), 0o600); err != nil {
		return err
	}
	if m.Safe != nil {
		if err := put(hubproto.PackSafeFile, append(mustJSONIndent(map[string]any{"version": m.Version, "safe": m.Safe}), '\n'), 0o600); err != nil {
			return err
		}
	}
	mark := mustJSONIndent(packMark{ID: m.ID, ItemID: itemID, Version: m.Version, Hash: hash})
	if err := put(hubproto.PackMarkFile, append(mark, '\n'), 0o600); err != nil {
		return err
	}
	old := dest + ".old"
	_ = os.RemoveAll(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return err
		}
	}
	if err := os.Rename(next, dest); err != nil {
		_ = os.Rename(old, dest) // put the old one back
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

func mustJSONIndent(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

// packHooksJSON is hooks/hooks.json of the plugin: ${PACK} in a command becomes the plugin's folder.
func packHooksJSON(m hubproto.PackManifest) []byte {
	type hook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type group struct {
		Matcher string `json:"matcher,omitempty"`
		Hooks   []hook `json:"hooks"`
	}
	events := map[string][]group{}
	for _, h := range m.Hooks {
		t := h.Timeout
		if t == 0 {
			t = 10
		}
		cmd := strings.ReplaceAll(h.Command, "${PACK}", "${CLAUDE_PLUGIN_ROOT}")
		events[h.Event] = append(events[h.Event], group{Matcher: h.Matcher, Hooks: []hook{{Type: "command", Command: cmd, Timeout: t}}})
	}
	return append(mustJSONIndent(map[string]any{"hooks": events}), '\n')
}

// pruneEnvPacks takes off the packs whose items left the list, after a full run on the machine itself.
func (ex *execution) prunePacks() {
	ents, err := os.ReadDir(ex.packsDir())
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".old") {
			_ = os.RemoveAll(filepath.Join(ex.packsDir(), name)) // what an interrupted install left
			continue
		}
		if _, keep := ex.packIDs[name]; keep || !hubproto.PackIDRe.MatchString(name) {
			continue
		}
		if mark, ok := readMark(filepath.Join(ex.packsDir(), name)); !ok || mark.ID != name {
			continue // not ours to remove
		}
		if err := os.RemoveAll(filepath.Join(ex.packsDir(), name)); err != nil {
			ex.r.logf("warn", "envsetup: a pack not taken off", "id", name, "error", err.Error())
			continue
		}
		ex.r.logf("info", "envsetup: a pack taken off", "id", name)
	}
}
