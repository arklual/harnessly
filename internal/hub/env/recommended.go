package env

import (
	"encoding/json"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// «Рекомендованное» (docs/hub/environment.md, «Recommended»): a group of the platform itself in every organization —
// plugins and settings of Claude Code an agent works better with, none of which the platform needs. It is optional and
// off: a person turns it on for themselves; turned off again, it stops being updated and what it put in stays. The
// organization's admins may put entries of their own into it on the organization's layer. Its items and entries live in
// the code, not in the database: they cannot be changed or removed in the hub.

// RecommendedGroupID is the group's id in every organization.
const RecommendedGroupID = "recommended"

const (
	recommendedItemPrefix  = "builtin-"
	recommendedEntryPrefix = "builtin-entry-"
	// recommendedPosition puts the group after the organization's own.
	recommendedPosition = 1 << 20
	officialMarketplace = "anthropics/claude-plugins-official"
)

type recommendedItem struct {
	id, kind, name, hint string
	source               any
}

func plugin(name string) map[string]string {
	return map[string]string{"marketplace": officialMarketplace, "plugin": name + "@claude-plugins-official"}
}

var recommendedItems = []recommendedItem{
	{"superpowers", store.CatalogPlugin, "Superpowers", "процесс работы: план, тесты, проверка перед сдачей", plugin("superpowers")},
	{"context7", store.CatalogPlugin, "Context7", "свежая документация библиотек", plugin("context7")},
	{"playwright", store.CatalogPlugin, "Playwright", "агент открывает страницы и проверяет их в браузере", plugin("playwright")},
	{"chrome-devtools", store.CatalogPlugin, "Chrome DevTools", "сеть, консоль и скорость страницы", plugin("chrome-devtools-mcp")},
	{"claude-settings", store.CatalogClaudeSettings, "Экономные настройки Claude", "сабагенты на Sonnet, разумные таймауты, правила экономии в CLAUDE.md", map[string]any{
		"always": map[string]any{
			"env": map[string]string{
				"CLAUDE_CODE_SUBAGENT_MODEL":          "sonnet",
				"BASH_DEFAULT_TIMEOUT_MS":             "120000",
				"BASH_MAX_TIMEOUT_MS":                 "600000",
				"CLAUDE_CODE_DISABLE_FEEDBACK_SURVEY": "1",
				"DISABLE_ERROR_REPORTING":             "1",
			},
			"subagentPromptCacheTtl": "1h",
		},
		"ifAbsent": map[string]any{"effortLevel": "medium", "includeCoAuthoredBy": false, "cleanupPeriodDays": 365},
		"claudeMd": recommendedClaudeMd,
	}},
}

const recommendedClaudeMd = `## Экономия токенов

- Мелкое и среднее делай сам: сабагент — это новый контекст с нуля.
- Сабагентам — модель sonnet; opus — только там, где нужно суждение, и одним агентом.
- Не больше 1–2 сабагентов разом.
- Контекст [1m] сабагентам не нужен. Отчёт сабагента — до 30 строк.
- Файлы читай кусками и по поиску, а не целиком; длинный вывод команд обрезай (| tail, | head).`

// addRecommended puts the platform's group, its items and its entries into an organization's world.
func (w *world) addRecommended() {
	w.groups = append(w.groups, store.EnvGroup{ID: RecommendedGroupID, OrgID: w.org.ID, Layer: store.EnvLayerOrg, ScopeID: w.org.ID,
		Name: "Рекомендованное", Hint: "плагины и настройки, с которыми агенту лучше; включает каждый у себя",
		Required: false, DefaultOn: false, Position: recommendedPosition})
	for i, r := range recommendedItems {
		src, _ := json.Marshal(r.source)
		id := recommendedItemPrefix + r.id
		w.items[id] = store.CatalogItem{ID: id, Kind: r.kind, Name: r.name, Hint: r.hint, Source: string(src)}
		w.entries = append(w.entries, store.EnvEntry{ID: recommendedEntryPrefix + r.id, OrgID: w.org.ID, Layer: store.EnvLayerOrg,
			ScopeID: w.org.ID, ItemID: id, Enabled: true, Audience: AudienceJSON(Audience{Mode: AudienceTenant}), Position: recommendedPosition + i,
			GroupID: RecommendedGroupID})
	}
}

// isBuiltInEntry says whether an entry is one of the platform's: nobody changes it in the hub.
func isBuiltInEntry(id string) bool { return strings.HasPrefix(id, recommendedEntryPrefix) }
