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
)

const (
	maxNameRunes   = 80
	maxHintRunes   = 200
	maxSourceBytes = 64 << 10
	maxTextBytes   = 48 << 10
	maxArgs        = 32
	maxArgRunes    = 512
	maxSecretRefs  = 16
)

var secretName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// The keys a source may have, by kind. A source never holds a secret: an MCP server names the secrets it needs in
// secretRefs and the machine looks them up by name.
var sourceKeys = map[string][]string{
	store.CatalogSkill: {"body", "ref"},
	store.CatalogMCP:   {"transport", "command", "args", "url", "secretRefs"},
	store.CatalogRepo:  {"url", "branch"},
	store.CatalogDoc:   {"url", "text"},
}

// ValidKind says whether kind is a catalog kind.
func ValidKind(kind string) bool { _, ok := sourceKeys[kind]; return ok }

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
	if u.User != nil {
		return httpx.Invalid(field, "В адресе не должно быть логина и пароля")
	}
	return nil
}

// CleanSource checks the source of a catalog item and returns it in a canonical form. A source holds the text or the
// address of the item and nothing secret; whatever is not understood is refused, so that nothing unchecked reaches a
// machine.
func CleanSource(kind string, raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return json.RawMessage("{}"), nil
	}
	if len(raw) > maxSourceBytes {
		return nil, httpx.Invalid("source", "Источник слишком большой")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, httpx.Invalid("source", "Источник — объект JSON")
	}
	allowed := sourceKeys[kind]
	out := map[string]any{}
	for k, v := range m {
		if !slices.Contains(allowed, k) {
			return nil, httpx.Invalid("source."+k, "Это поле здесь не используется")
		}
		switch k {
		case "args", "secretRefs":
			var list []string
			if err := json.Unmarshal(v, &list); err != nil {
				return nil, httpx.Invalid("source."+k, "Нужен список строк")
			}
			if len(list) > maxArgs || (k == "secretRefs" && len(list) > maxSecretRefs) {
				return nil, httpx.Invalid("source."+k, "Слишком много значений")
			}
			for _, s := range list {
				if k == "secretRefs" && !secretName.MatchString(s) {
					return nil, httpx.Invalid("source.secretRefs", "Секрет называют по имени: буквы, цифры и подчёркивание")
				}
				if utf8.RuneCountInString(s) > maxArgRunes {
					return nil, httpx.Invalid("source."+k, "Слишком длинное значение")
				}
			}
			out[k] = list
		default:
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, httpx.Invalid("source."+k, "Нужна строка")
			}
			limit := maxArgRunes
			if k == "body" || k == "text" {
				if len(s) > maxTextBytes {
					return nil, httpx.Invalid("source."+k, "Слишком длинный текст")
				}
				limit = maxTextBytes
			}
			if utf8.RuneCountInString(s) > limit {
				return nil, httpx.Invalid("source."+k, "Слишком длинное значение")
			}
			if s != "" {
				switch {
				case k == "url" && kind == store.CatalogMCP:
					if err := checkURL("source.url", s, "https", "http"); err != nil {
						return nil, err
					}
				case k == "url" && kind == store.CatalogRepo:
					if err := checkURL("source.url", s, "https", "ssh"); err != nil {
						return nil, err
					}
				case k == "url", k == "ref":
					if err := checkURL("source."+k, s, "https", "http"); err != nil {
						return nil, err
					}
				case k == "transport" && s != "stdio" && s != "http" && s != "sse":
					return nil, httpx.Invalid("source.transport", "stdio, http или sse")
				}
			}
			out[k] = s
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
