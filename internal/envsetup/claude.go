package envsetup

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The kinds that change the person's Claude Code (docs/hub/environment.md, «Recommended»):
//
//   - plugin: `claude plugin marketplace add` when the marketplace is not known yet, then `claude plugin install`. A
//     plugin in place is left as it is: Claude Code updates its plugins itself.
//   - claude-settings: keys of ~/.claude/settings.json — `always` set every time, `ifAbsent` only where the person has
//     none (for "env", variable by variable) — and a block of ~/.claude/CLAUDE.md between marks of the item. Nothing
//     else of either file is touched; a settings.json that is not JSON is left alone.
//
// Turned off (the item left the list), neither is undone: what it put in stays.

// diskHome is where the run's home is on this machine's disk: a sandbox run writes into its own folder.
func (ex *execution) diskHome() string {
	if ex.sandbox && ex.tmpHome != "" {
		return ex.tmpHome
	}
	return ex.r.Home
}

func (ex *execution) plugin(i int) {
	src := ex.srcs[i]
	name, market, ok := strings.Cut(src.Plugin, "@")
	if !ok || name == "" || market == "" || src.Marketplace == "" {
		ex.fail(i, "плагин указан не полностью: нужно имя@маркетплейс и адрес маркетплейса")
		return
	}
	plugins := filepath.Join(ex.diskHome(), ".claude", "plugins")
	if hasKey(filepath.Join(plugins, "installed_plugins.json"), src.Plugin, "plugins") {
		ex.done(i, hubproto.EnvSkipped, "уже стоит")
		return
	}
	if !ex.modifies {
		ex.pend(i, "не поставлен — поставится при обновлении")
		return
	}
	run := func(args ...string) bool {
		exit, err := ex.runLogged(i, Cmd{Name: "claude", Args: args, Dir: ex.home, Env: ex.envWith(nil), Timeout: gitTimeout},
			"claude "+strings.Join(args, " "))
		switch {
		case errors.Is(err, exec.ErrNotFound):
			ex.fail(i, "нет Claude Code (команды claude): поставьте его, и плагин встанет при следующем обновлении")
		case err != nil || exit != 0:
			ex.fail(i, cmp.Or(lastLogLine(ex.logs[i].String()), "claude закончился с ошибкой"))
		default:
			return true
		}
		return false
	}
	if !hasKey(filepath.Join(plugins, "known_marketplaces.json"), market, "") {
		if !run("plugin", "marketplace", "add", src.Marketplace) {
			return
		}
	}
	if run("plugin", "install", src.Plugin) {
		ex.done(i, hubproto.EnvOK, "поставлен")
	}
}

// hasKey says whether the JSON object of a file (or its object under `under`) has the key.
func hasKey(path, key, under string) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	if under != "" {
		if json.Unmarshal(m[under], &m) != nil {
			return false
		}
	}
	_, ok := m[key]
	return ok
}

func (ex *execution) claudeSettings(i int) {
	it, src := ex.items[i], ex.srcs[i]
	dir := filepath.Join(ex.diskHome(), ".claude")
	settingsPath := filepath.Join(dir, "settings.json")
	var changed []string

	if len(src.Always) > 0 || len(src.IfAbsent) > 0 {
		cur, raw, err := readSettings(settingsPath)
		if err != nil {
			ex.fail(i, "~/.claude/settings.json не разобран как JSON — не трогаю его")
			return
		}
		next, err := mergeSettings(cur, src.Always, src.IfAbsent)
		if err != nil {
			ex.fail(i, err.Error())
			return
		}
		out, _ := json.MarshalIndent(next, "", "  ")
		out = append(out, '\n')
		if !bytes.Equal(bytes.TrimSpace(out), bytes.TrimSpace(raw)) {
			changed = append(changed, "settings.json")
			if ex.modifies {
				if err := writeKeeping(settingsPath, out); err != nil {
					ex.fail(i, "settings.json не записан: "+err.Error())
					return
				}
			}
		}
	}
	if src.ClaudeMd != "" {
		mdPath := filepath.Join(dir, "CLAUDE.md")
		raw, err := os.ReadFile(mdPath)
		if err != nil && !os.IsNotExist(err) {
			ex.fail(i, "CLAUDE.md не прочитан: "+err.Error())
			return
		}
		out, err := withBlock(raw, it.ItemID, src.ClaudeMd)
		if err != nil {
			ex.fail(i, err.Error())
			return
		}
		if !bytes.Equal(out, raw) {
			changed = append(changed, "CLAUDE.md")
			if ex.modifies {
				if err := writeKeeping(mdPath, out); err != nil {
					ex.fail(i, "CLAUDE.md не записан: "+err.Error())
					return
				}
			}
		}
	}
	switch {
	case len(changed) == 0:
		ex.done(i, hubproto.EnvSkipped, "уже так")
	case !ex.modifies:
		ex.pend(i, "отличается: "+strings.Join(changed, ", ")+" — обновится при обновлении")
	default:
		ex.logs[i].WriteString("записано: " + strings.Join(changed, ", ") + "\n")
		ex.done(i, hubproto.EnvOK, "записано: "+strings.Join(changed, ", "))
	}
}

// readSettings reads settings.json: an absent or empty file is an empty object, anything but an object is an error.
func readSettings(path string) (map[string]json.RawMessage, []byte, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) || (err == nil && len(bytes.TrimSpace(raw)) == 0) {
		return map[string]json.RawMessage{}, raw, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, nil, errors.New("not an object")
	}
	return m, raw, nil
}

// mergeSettings puts always over cur and ifAbsent under it. Only the keys Claude Code's list allows are written, and in
// "env" only the variables of its list: the machine holds to them whatever the hub sent.
func mergeSettings(cur, always, ifAbsent map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	out := make(map[string]json.RawMessage, len(cur))
	for k, v := range cur {
		out[k] = v
	}
	for _, pass := range []struct {
		vals  map[string]json.RawMessage
		force bool
	}{{always, true}, {ifAbsent, false}} {
		for k, v := range pass.vals {
			if !slices.Contains(hubproto.ClaudeSettingsKeys, k) {
				return nil, errors.New("настройку " + k + " окружение не ставит")
			}
			if k != "env" {
				if _, has := out[k]; pass.force || !has {
					out[k] = v
				}
				continue
			}
			var add map[string]string
			if err := json.Unmarshal(v, &add); err != nil {
				return nil, errors.New("env — не набор строк")
			}
			env := map[string]any{}
			if raw, ok := out["env"]; ok && json.Unmarshal(raw, &env) != nil {
				return nil, errors.New("env в settings.json — не объект, не трогаю его")
			}
			for n, val := range add {
				if !slices.Contains(hubproto.ClaudeEnvKeys, n) {
					return nil, errors.New("переменную " + n + " окружение не ставит")
				}
				if _, has := env[n]; pass.force || !has {
					env[n] = val
				}
			}
			b, _ := json.Marshal(env)
			out["env"] = b
		}
	}
	return out, nil
}

func blockMarks(id string) (string, string) {
	return "<!-- harnessly:" + id + " -->", "<!-- /harnessly:" + id + " -->"
}

// withBlock puts text between the item's marks in a CLAUDE.md: in place of the old block, or at the end.
func withBlock(raw []byte, id, text string) ([]byte, error) {
	open, end := blockMarks(id)
	if strings.Contains(text, "<!-- harnessly:") || strings.Contains(text, "<!-- /harnessly:") {
		return nil, errors.New("в тексте для CLAUDE.md не может быть меток harnessly")
	}
	block := open + "\n" + strings.TrimSpace(text) + "\n" + end
	s := string(raw)
	if a := strings.Index(s, open); a >= 0 {
		if b := strings.Index(s[a:], end); b >= 0 {
			return []byte(s[:a] + block + s[a+b+len(end):]), nil
		}
	}
	switch {
	case s == "":
		return []byte(block + "\n"), nil
	case strings.HasSuffix(s, "\n\n"):
	case strings.HasSuffix(s, "\n"):
		s += "\n"
	default:
		s += "\n\n"
	}
	return []byte(s + block + "\n"), nil
}

// writeKeeping writes a file of the person's in one step (a temporary file, then a rename), with the mode it had.
func writeKeeping(path string, data []byte) error {
	// A file linked from elsewhere (the person's dotfiles) is written where it really is, the link kept.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func lastLogLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "$ ") && !strings.HasPrefix(l, "(") {
			return oneLine(l)
		}
	}
	return ""
}
