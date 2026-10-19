package catalog

import (
	"slices"
	"strings"
)

const terminalOnly = "Работает только в терминале: интерфейс команды не переносится в веб"

const (
	copyNote  = "Скопируйте ответ кнопкой в ленте: буфер обмена машины здесь бесполезен"
	loginNote = "Вход на машине управляется централизованно"
)

// terminalRows: commands whose interface only works in the terminal; hidden, running one opens the terminal.
func terminalRows(pairs [][2]string) []builtin {
	out := make([]builtin, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, builtin{name: p[0], description: p[1], execution: "terminal", output: "screen", hidden: true, note: terminalOnly})
	}
	return out
}

// claudeBuiltins: the curated built-ins of Claude Code 2.1.282. A command missing here still runs as typed.
var claudeBuiltins = append([]builtin{
	{name: "compact", description: "Сжать историю диалога, чтобы освободить контекст", argumentHint: "[инструкция]", execution: "send", output: "transcript"},
	{name: "context", description: "Показать, чем занят контекст", execution: "send", output: "transcript"},
	{name: "rename", description: "Переименовать сессию", argumentHint: "<имя>", execution: "send", output: "transcript"},
	{name: "add-dir", description: "Дать агенту доступ к ещё одной папке", argumentHint: "<путь>", execution: "send", output: "transcript"},
	{name: "init", description: "Создать CLAUDE.md с описанием проекта", execution: "send", output: "reply", welcome: true},
	{name: "plan", description: "Включить режим плана: сначала план, потом правки", execution: "send", output: "transcript"},
	{name: "recap", description: "Кратко пересказать, что уже сделано в сессии", execution: "send", output: "transcript"},
	{name: "fast", description: "Переключить быстрый режим ответа", execution: "send", output: "transcript"},
	{name: "version", description: "Версия Claude Code", execution: "send", output: "transcript"},
	{name: "agents", description: "Субагенты: список и настройка", execution: "menu", output: "screen"},
	{name: "reload-plugins", description: "Перечитать плагины без перезапуска", execution: "send", output: "transcript"},
	{name: "reload-skills", description: "Перечитать навыки без перезапуска", execution: "send", output: "transcript"},
	{name: "model", description: "Сменить модель", argumentHint: "[модель]", execution: "model", output: "transcript"},
	{name: "effort", description: "Сменить глубину рассуждения", argumentHint: "[уровень]", execution: "effort", output: "transcript"},
	{name: "usage", aliases: []string{"cost", "stats"}, description: "Лимиты подписки и расход", execution: "menu", output: "screen"},
	{name: "status", description: "Версия, модель, аккаунт и подключения сессии", execution: "menu", output: "screen"},
	{name: "mcp", description: "MCP-серверы: подключение и статус", execution: "menu", output: "screen"},
	{name: "permissions", aliases: []string{"allowed-tools"}, description: "Правила разрешений для инструментов", execution: "menu", output: "screen"},
	{name: "hooks", description: "Хуки сессии", execution: "menu", output: "screen"},
	{name: "plugin", aliases: []string{"plugins"}, description: "Плагины: установка и включение", execution: "menu", output: "screen"},
	{name: "skills", description: "Список навыков", execution: "menu", output: "screen"},
	{name: "export", description: "Выгрузить диалог в файл", execution: "menu", output: "screen"},
	{name: "tasks", aliases: []string{"bashes"}, description: "Фоновые задачи сессии", execution: "menu", output: "screen"},
	{name: "config", aliases: []string{"settings"}, description: "Настройки Claude Code", execution: "menu", output: "screen"},
	{name: "resume", aliases: []string{"continue"}, description: "Продолжить одну из прошлых сессий", argumentHint: "[сессия]", execution: "menu", output: "screen"},
	{name: "clear", aliases: []string{"reset", "new"}, description: "Начать диалог заново", execution: "restart", output: "none"},
	{name: "help", description: "Все команды", execution: "help", output: "none"},
	{name: "exit", aliases: []string{"quit"}, description: "Завершить сессию агента", execution: "confirm", output: "none",
		note: "Агент в этом диалоге остановится. Продолжить можно будет перезапуском."},
	{name: "copy", description: "Скопировать последний ответ", execution: "blocked", output: "none", note: copyNote},
	{name: "login", description: "Войти в аккаунт", execution: "blocked", output: "none", note: loginNote},
	{name: "logout", description: "Выйти из аккаунта", execution: "blocked", output: "none", note: loginNote},
}, terminalRows([][2]string{
	{"theme", "Тема терминала"},
	{"terminal-setup", "Настроить клавиши терминала"},
	{"memory", "Редактировать файлы памяти"},
	{"ide", "Подключение к IDE"},
	{"tui", "Режим отрисовки терминала"},
	{"color", "Цвет сессии"},
	{"scroll-speed", "Скорость прокрутки"},
	{"output-style", "Стиль ответов"},
	{"btw", "Вопрос в сторону, не прерывая работу"},
	{"doctor", "Проверить установку Claude Code"},
	{"upgrade", "Обновить подписку"},
	{"privacy-settings", "Настройки приватности"},
})...)

// codexBuiltins: the curated built-ins of Codex CLI 0.152. Codex keeps their results on its screen.
var codexBuiltins = append([]builtin{
	{name: "model", description: "Выбрать модель и глубину рассуждения", execution: "menu", output: "screen"},
	{name: "permissions", aliases: []string{"approvals"}, description: "Что Codex разрешено делать", execution: "menu", output: "screen"},
	{name: "review", description: "Ревью текущих изменений", execution: "menu", output: "reply"},
	{name: "fork", description: "Ответвить диалог", execution: "menu", output: "screen"},
	{name: "experimental", description: "Экспериментальные возможности", execution: "menu", output: "screen"},
	{name: "personality", description: "Стиль общения Codex", execution: "menu", output: "screen"},
	{name: "memories", description: "Память Codex", execution: "menu", output: "screen"},
	{name: "hooks", description: "Хуки и доверие к ним", execution: "menu", output: "screen"},
	{name: "resume", description: "Продолжить одну из прошлых сессий", execution: "menu", output: "screen"},
	{name: "init", description: "Создать AGENTS.md с инструкциями для Codex", execution: "send", output: "reply", welcome: true},
	{name: "compact", description: "Сжать историю диалога, чтобы освободить контекст", execution: "send", output: "reply"},
	{name: "plan", description: "Режим плана", execution: "send", output: "reply"},
	{name: "fast", description: "Быстрый режим: в 1,5 раза быстрее, расход выше", execution: "send", output: "none"},
	{name: "goal", description: "Поставить цель, к которой Codex идёт сам", argumentHint: "[цель|clear|edit|pause|resume]", execution: "send", output: "reply", welcome: true},
	{name: "rename", description: "Переименовать диалог", argumentHint: "<имя>", execution: "send", output: "none"},
	{name: "cd", description: "Сменить рабочую папку", argumentHint: "<путь>", execution: "send", output: "none"},
	{name: "stop", description: "Остановить фоновые задачи", execution: "send", output: "none"},
	{name: "status", description: "Модель, папка, права и лимиты сессии", execution: "send", output: "screen"},
	{name: "diff", description: "Показать git diff, включая новые файлы", execution: "send", output: "screen"},
	{name: "mcp", description: "Подключённые MCP-инструменты", argumentHint: "[verbose]", execution: "send", output: "screen"},
	{name: "usage", description: "Расход токенов", argumentHint: "[daily|weekly|cumulative]", execution: "send", output: "screen"},
	{name: "rollout", description: "Путь к файлу сессии", execution: "send", output: "screen"},
	{name: "ps", description: "Фоновые терминалы", execution: "send", output: "screen"},
	{name: "pwd", description: "Текущая папка", execution: "send", output: "screen"},
	{name: "new", description: "Начать диалог заново", execution: "restart", output: "none"},
	{name: "clear", description: "Очистить экран и начать заново", execution: "restart", output: "none"},
	{name: "skills", description: "Навыки: вызываются через $", execution: "skills", output: "none"},
	{name: "exit", aliases: []string{"quit"}, description: "Завершить сессию Codex", execution: "confirm", output: "none",
		note: "Codex в этом диалоге остановится. Продолжить можно будет перезапуском."},
	{name: "archive", description: "Убрать диалог в архив", execution: "confirm", output: "none", note: "Диалог уйдёт в архив Codex, а сессия завершится."},
	{name: "delete", description: "Удалить диалог", execution: "confirm", output: "none", note: "Диалог Codex удалится навсегда, восстановить его не получится.", danger: true},
	{name: "copy", description: "Скопировать последний ответ", execution: "blocked", output: "none", note: copyNote},
	{name: "logout", description: "Выйти из аккаунта", execution: "blocked", output: "none", note: loginNote},
}, terminalRows([][2]string{
	{"side", "Побочный диалог"},
	{"btw", "Вопрос в сторону"},
	{"agent", "Переключиться на другой агентный тред"},
	{"subagents", "Субагенты"},
	{"mention", "Упомянуть файл"},
	{"theme", "Тема подсветки"},
	{"title", "Заголовок окна терминала"},
	{"statusline", "Строка статуса"},
	{"pet", "Питомец в терминале"},
	{"keymap", "Переназначить клавиши"},
	{"vim", "Режим Vim в поле ввода"},
	{"apps", "Приложения"},
	{"plugins", "Плагины"},
	{"ide", "Контекст из IDE"},
	{"app", "Открыть приложение Codex"},
	{"import", "Импорт настроек"},
	{"approve", "Разрешить повтор после отказа авто-ревью"},
	{"debug-config", "Отладка слоёв конфигурации"},
	{"test-approval", "Проверка запроса разрешения"},
	{"feedback", "Отправить отзыв"},
	{"raw", "Сырой вывод"},
	{"setup-default-sandbox", "Настроить песочницу"},
	{"sandbox-add-read-dir", "Разрешить песочнице читать папку"},
})...)

// IsCommandName says whether a slash command may have this name.
func IsCommandName(name string) bool { return commandName.MatchString(name) }

// Builtin is the curated built-in of an agent CLI ("claude" or "codex") behind a name or one of its aliases, compared in
// lower case. False for anything else — skills, plugins, typos — which the CLI gets typed as is.
func Builtin(provider, name string) (Item, bool) {
	rows := claudeBuiltins
	switch provider {
	case "claude":
	case "codex":
		rows = codexBuiltins
	default:
		return Item{}, false
	}
	wanted := strings.ToLower(name)
	for _, row := range rows {
		if row.name == wanted || slices.Contains(row.aliases, wanted) {
			return builtinItem(row), true
		}
	}
	return Item{}, false
}
