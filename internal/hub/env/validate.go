package env

import (
	"bytes"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	maxNameRunes   = 80
	maxHintRunes   = 200
	maxSourceBytes = 64 << 10
	maxTextBytes   = 48 << 10
	maxCmdBytes    = 4 << 10 // a script's run and check
	maxArgs        = 32
	maxArgRunes    = 512
	maxSecretRefs  = 16
)

var secretName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// hostRe is a bare host name with an optional port: no scheme, no path, no login.
var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:[0-9]{1,5})?$`)

// brewRe finds a call of Homebrew: the machines are Linux.
var brewRe = regexp.MustCompile(`(^|[\s;&|(])brew\s`)

// The keys a source may have, by kind. A source never holds a secret: an MCP server names the secrets it needs in
// secretRefs, a script in secrets, and the machine looks them up by name.
var sourceKeys = map[string][]string{
	store.CatalogSkill:  {"body", "ref"},
	store.CatalogMCP:    {"transport", "command", "args", "url", "secretRefs"},
	store.CatalogRepo:   {"url", "branch", "dir"},
	store.CatalogDoc:    {"url", "text"},
	store.CatalogAccess: {"service", "host", "secret"},
	store.CatalogScript: {"from", "body", "fileName", "repo", "repoUrl", "branch", "path", "url", "run", "rerun", "check", "secrets"},
	// A plugin: the marketplace (owner/repo on GitHub or a git address) and plugin@marketplace.
	store.CatalogPlugin: {"marketplace", "plugin"},
	// Settings of Claude Code: keys set always, keys set only where the person has none, the CLAUDE.md block.
	store.CatalogClaudeSettings: {"always", "ifAbsent", "claudeMd"},
}

// objectKeys are the keys whose value is an object of Claude Code's settings.
var objectKeys = []string{"always", "ifAbsent"}

// pluginRe is plugin@marketplace; marketRe a GitHub owner/repo.
var (
	pluginRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}@[a-z0-9][a-z0-9._-]{0,63}$`)
	marketRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
)

// listKeys are the keys whose value is a list of strings.
var listKeys = []string{"args", "secretRefs", "secrets"}

// ValidKind says whether kind is a catalog kind.
func ValidKind(kind string) bool { _, ok := sourceKeys[kind]; return ok }

// kindsRu lists the kinds for the message of a refused one.
const kindsRu = "skill, mcp, repo, doc, script, access, plugin или claude-settings"

// CleanText trims a name or a hint and checks its length in characters.
func CleanText(field, raw string, max int, required bool) (string, error) {
	v := strings.Join(strings.Fields(raw), " ")
	n := utf8.RuneCountInString(v)
	if required && n == 0 {
		return "", httpx.Invalid(field, "Заполните поле")
	}
	if n > max {
		return "", httpx.Invalid(field, "Слишком длинно")
	}
	for _, r := range v {
		if r < ' ' || r == 0x7f {
			return "", httpx.Invalid(field, "Лишние символы")
		}
	}
	return v, nil
}

func checkURL(field, raw string, schemes ...string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !slices.Contains(schemes, u.Scheme) {
		return httpx.Invalid(field, "Нужен адрес вида "+schemes[0]+"://…")
	}
	// ssh://git@host names the account every clone goes through; it is no secret. A password never is allowed.
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword || u.Scheme != "ssh" {
			return httpx.Invalid(field, "В адресе не должно быть логина и пароля")
		}
	}
	return nil
}

// checkDir: a folder on the machine is "~/…" or relative to the home, and stays inside it.
func checkDir(field, s string) error {
	switch {
	case strings.HasPrefix(s, "/"), s == "~", strings.HasPrefix(s, "~") && !strings.HasPrefix(s, "~/"):
		return httpx.Invalid(field, "Папка — «~/…» или путь от домашней папки")
	case slices.Contains(strings.Split(strings.ReplaceAll(s, "\\", "/"), "/"), ".."):
		return httpx.Invalid(field, "«..» в пути не допускается")
	}
	return nil
}

// checkRelPath: a path inside a repository, never outside it.
func checkRelPath(field, s string) error {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || slices.Contains(strings.Split(strings.ReplaceAll(s, "\\", "/"), "/"), "..") {
		return httpx.Invalid(field, "Путь внутри репозитория, без «..»")
	}
	return nil
}

// cleanString checks one string value of a source.
func cleanString(kind, k, s string) error {
	field := "source." + k
	limit := maxArgRunes
	switch {
	case k == "claudeMd":
		if len(s) > hubproto.ClaudeMdMax {
			return httpx.Invalid(field, "Слишком длинный текст для CLAUDE.md")
		}
		limit = hubproto.ClaudeMdMax
	case k == "body" || k == "text":
		if len(s) > maxTextBytes {
			return httpx.Invalid(field, "Слишком длинный текст")
		}
		limit = maxTextBytes
	case kind == store.CatalogScript && (k == "run" || k == "check"):
		if len(s) > maxCmdBytes {
			return httpx.Invalid(field, "Слишком длинная команда")
		}
		limit = maxCmdBytes
	}
	if utf8.RuneCountInString(s) > limit {
		return httpx.Invalid(field, "Слишком длинное значение")
	}
	if s == "" {
		return nil
	}
	switch {
	case k == "url" && kind == store.CatalogScript:
		if u, err := url.Parse(s); err != nil || u.Scheme != "https" {
			return httpx.Invalid(field, "Скрипт по ссылке — только https")
		}
		return checkURL(field, s, "https")
	case k == "url" && kind == store.CatalogMCP:
		return checkURL(field, s, "https", "http")
	case (k == "url" && kind == store.CatalogRepo) || k == "repoUrl":
		return checkURL(field, s, "https", "ssh")
	case k == "url", k == "ref":
		return checkURL(field, s, "https", "http")
	case k == "plugin" && !pluginRe.MatchString(s):
		return httpx.Invalid(field, "Плагин пишут как имя@маркетплейс")
	case k == "marketplace":
		if marketRe.MatchString(s) {
			return nil
		}
		return checkURL(field, s, "https")
	case k == "transport" && s != "stdio" && s != "http" && s != "sse":
		return httpx.Invalid(field, "stdio, http или sse")
	case k == "dir":
		return checkDir(field, s)
	case k == "path":
		return checkRelPath(field, s)
	case k == "fileName" && strings.ContainsAny(s, "/\\"):
		return httpx.Invalid(field, "Нужно имя файла без папок")
	case k == "from" && !slices.Contains([]string{"text", "repo", "file", "url"}, s):
		return httpx.Invalid(field, "text, repo, file или url")
	case k == "rerun" && s != "once" && s != "check":
		return httpx.Invalid(field, "once или check")
	case k == "service" && s != "gitlab" && s != "github":
		return httpx.Invalid(field, "gitlab или github")
	case k == "host" && !hostRe.MatchString(s):
		return httpx.Invalid(field, "Нужен адрес сервера без http:// и пути, например gitlab.example.org")
	case k == "secret" && !hubproto.SafeNameRe.MatchString(s):
		return httpx.Invalid(field, "Секрет называют по имени: буквы, цифры, точка, дефис и подчёркивание")
	}
	return nil
}

// cleanScript checks that the keys of a script fit together and that nothing in it calls brew.
func cleanScript(out map[string]any) error {
	str := func(k string) string { s, _ := out[k].(string); return s }
	for _, k := range []string{"body", "run", "check"} {
		if brewRe.MatchString(str(k)) {
			return httpx.Invalid("source."+k, "Машины на Linux: brew недоступен")
		}
	}
	switch str("from") {
	case "":
		return httpx.Invalid("source.from", "Выберите, откуда взять скрипт")
	case "text":
		if strings.TrimSpace(str("body")) == "" {
			return httpx.Invalid("source.body", "Напишите команды скрипта")
		}
	case "file":
		if strings.TrimSpace(str("body")) == "" {
			return httpx.Invalid("source.body", "В файле нет текста")
		}
		if str("fileName") == "" {
			return httpx.Invalid("source.fileName", "Укажите имя файла")
		}
	case "repo":
		if str("repo") == "" && str("repoUrl") == "" {
			return httpx.Invalid("source.repo", "Выберите репозиторий или укажите его адрес")
		}
		if str("path") == "" {
			return httpx.Invalid("source.path", "Укажите файл в репозитории")
		}
	case "url":
		if str("url") == "" {
			return httpx.Invalid("source.url", "Укажите адрес скрипта")
		}
	}
	if str("rerun") == "check" && strings.TrimSpace(str("check")) == "" {
		return httpx.Invalid("source.check", "Для «пока проверка не пройдёт» нужна команда проверки")
	}
	return nil
}

// CleanSource checks the source of a catalog item and returns it in a canonical form. A source holds the text or the
// address of the item and nothing secret; whatever is not understood is refused, so that nothing unchecked reaches a
// machine.
func CleanSource(kind string, raw json.RawMessage) (json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if t := bytes.TrimSpace(raw); len(t) > 0 && !bytes.Equal(t, []byte("null")) {
		if len(raw) > maxSourceBytes {
			return nil, httpx.Invalid("source", "Источник слишком большой")
		}
		if err := json.Unmarshal(raw, &m); err != nil || m == nil {
			return nil, httpx.Invalid("source", "Источник — объект JSON")
		}
	}
	allowed := sourceKeys[kind]
	out := map[string]any{}
	for k, v := range m {
		if !slices.Contains(allowed, k) {
			return nil, httpx.Invalid("source."+k, "Это поле здесь не используется")
		}
		if slices.Contains(objectKeys, k) {
			obj, err := cleanClaudeSettings("source."+k, v)
			if err != nil {
				return nil, err
			}
			out[k] = obj
			continue
		}
		if slices.Contains(listKeys, k) {
			var list []string
			if err := json.Unmarshal(v, &list); err != nil {
				return nil, httpx.Invalid("source."+k, "Нужен список строк")
			}
			if len(list) > maxArgs || (k != "args" && len(list) > maxSecretRefs) {
				return nil, httpx.Invalid("source."+k, "Слишком много значений")
			}
			for _, s := range list {
				if k == "secretRefs" && !secretName.MatchString(s) {
					return nil, httpx.Invalid("source.secretRefs", "Секрет называют по имени: буквы, цифры и подчёркивание")
				}
				if k == "secrets" && !hubproto.SafeNameRe.MatchString(s) {
					return nil, httpx.Invalid("source.secrets", "Секрет называют по имени: буквы, цифры, точка, дефис и подчёркивание")
				}
				if utf8.RuneCountInString(s) > maxArgRunes {
					return nil, httpx.Invalid("source."+k, "Слишком длинное значение")
				}
			}
			out[k] = list
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return nil, httpx.Invalid("source."+k, "Нужна строка")
		}
		if err := cleanString(kind, k, s); err != nil {
			return nil, err
		}
		out[k] = s
	}
	switch kind {
	case store.CatalogScript:
		if err := cleanScript(out); err != nil {
			return nil, err
		}
	case store.CatalogAccess:
		if s, _ := out["service"].(string); s == "" {
			return nil, httpx.Invalid("source.service", "gitlab или github")
		}
	case store.CatalogPlugin:
		if s, _ := out["plugin"].(string); s == "" {
			return nil, httpx.Invalid("source.plugin", "Плагин пишут как имя@маркетплейс")
		}
		if s, _ := out["marketplace"].(string); s == "" {
			return nil, httpx.Invalid("source.marketplace", "Нужен адрес маркетплейса")
		}
	case store.CatalogClaudeSettings:
		if len(out) == 0 {
			return nil, httpx.Invalid("source", "Нужны настройки или текст для CLAUDE.md")
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// AudienceIn is an audience as a request writes it.
type AudienceIn struct {
	Mode  string   `json:"mode"`
	Teams []string `json:"teams"`
}

// CleanAudience checks the audience of an entry of a layer; teams lists the ids of the teams the layer may name.
// The team layer reaches its own team, so it takes tenant (all of the team) or off only.
func CleanAudience(layer string, in *AudienceIn, teams map[string]bool) (Audience, error) {
	if in == nil {
		return Audience{Mode: AudienceTenant, Teams: []string{}}, nil
	}
	a := Audience{Mode: in.Mode, Teams: []string{}}
	switch in.Mode {
	case AudienceTenant, AudienceOff:
		if len(in.Teams) > 0 {
			return a, httpx.Invalid("audience.teams", "Команды указывают только для режима «teams»")
		}
	case AudienceTeams:
		if layer == store.EnvLayerTeam {
			return a, httpx.Invalid("audience.mode", "Слой команды относится ко всей команде")
		}
		if len(in.Teams) == 0 {
			return a, httpx.Invalid("audience.teams", "Выберите команды")
		}
		for _, id := range in.Teams {
			if !teams[id] {
				return a, httpx.Invalid("audience.teams", "Команда не найдена")
			}
			if !slices.Contains(a.Teams, id) {
				a.Teams = append(a.Teams, id)
			}
		}
		slices.Sort(a.Teams)
	default:
		return a, httpx.Invalid("audience.mode", "tenant, teams или off")
	}
	return a, nil
}

// AudienceJSON is the audience as kept with an entry.
func AudienceJSON(a Audience) string {
	if a.Teams == nil {
		a.Teams = []string{}
	}
	b, _ := json.Marshal(a)
	return string(b)
}

// cleanClaudeSettings checks an object of Claude Code's settings: only the keys of hubproto.ClaudeSettingsKeys, an
// "env" of hubproto.ClaudeEnvKeys with string values, and no value bigger than a line or two.
func cleanClaudeSettings(field string, raw json.RawMessage) (map[string]any, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, httpx.Invalid(field, "Нужен объект настроек")
	}
	out := map[string]any{}
	for k, v := range obj {
		if !slices.Contains(hubproto.ClaudeSettingsKeys, k) {
			return nil, httpx.Invalid(field+"."+k, "Эту настройку Claude Code окружение не ставит")
		}
		if k == "env" {
			var env map[string]string
			if err := json.Unmarshal(v, &env); err != nil {
				return nil, httpx.Invalid(field+".env", "Переменные — строки")
			}
			for n, val := range env {
				if !slices.Contains(hubproto.ClaudeEnvKeys, n) {
					return nil, httpx.Invalid(field+".env."+n, "Эту переменную окружение не ставит")
				}
				if utf8.RuneCountInString(val) > maxArgRunes {
					return nil, httpx.Invalid(field+".env."+n, "Слишком длинное значение")
				}
			}
			out[k] = env
			continue
		}
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			return nil, httpx.Invalid(field+"."+k, "Неверное значение")
		}
		switch val.(type) {
		case string, float64, bool:
		default:
			return nil, httpx.Invalid(field+"."+k, "Значение — строка, число или да/нет")
		}
		if s, ok := val.(string); ok && utf8.RuneCountInString(s) > maxArgRunes {
			return nil, httpx.Invalid(field+"."+k, "Слишком длинное значение")
		}
		out[k] = val
	}
	return out, nil
}
