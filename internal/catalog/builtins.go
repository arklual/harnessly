package catalog

import (
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The texts of the tables below are English and Russian; the web page lists the same English ones in its dictionary.
var (
	terminalOnly = locale.T("Works only in the terminal: this command has no web interface", "Работает только в терминале: интерфейс команды не переносится в веб")
	copyNote     = locale.T("Copy the reply with the button in the feed: the machine's clipboard is no use here", "Скопируйте ответ кнопкой в ленте: буфер обмена машины здесь бесполезен")
	loginNote    = locale.T("Sign-in on the machine is managed centrally", "Вход на машине управляется централизованно")
)

// plain is a hint that reads the same in every language.
func plain(s string) locale.Text { return locale.T(s, s) }

// terminalRow is a command by name and what it does.
type terminalRow struct {
	name        string
	description locale.Text
}

// terminalRows: commands whose interface only works in the terminal; hidden, running one opens the terminal.
func terminalRows(rows []terminalRow) []builtin {
	out := make([]builtin, 0, len(rows))
	for _, r := range rows {
		out = append(out, builtin{name: r.name, description: r.description, execution: "terminal", output: "screen", hidden: true, note: terminalOnly})
	}
	return out
}

// claudeBuiltins: the curated built-ins of Claude Code 2.1.282. A command missing here still runs as typed.
var claudeBuiltins = append([]builtin{
	{name: "compact", description: locale.T("Compact the conversation history to free up context", "Сжать историю диалога, чтобы освободить контекст"), argumentHint: locale.T("[instructions]", "[инструкция]"), execution: "send", output: "transcript"},
	{name: "context", description: locale.T("Show what is using the context", "Показать, чем занят контекст"), execution: "send", output: "transcript"},
	{name: "rename", description: locale.T("Rename the session", "Переименовать сессию"), argumentHint: locale.T("<name>", "<имя>"), execution: "send", output: "transcript"},
	{name: "add-dir", description: locale.T("Give the agent access to another folder", "Дать агенту доступ к ещё одной папке"), argumentHint: locale.T("<path>", "<путь>"), execution: "send", output: "transcript"},
	{name: "init", description: locale.T("Create a CLAUDE.md that describes the project", "Создать CLAUDE.md с описанием проекта"), execution: "send", output: "reply", welcome: true},
	{name: "plan", description: locale.T("Turn on plan mode: plan first, then edit", "Включить режим плана: сначала план, потом правки"), execution: "send", output: "transcript"},
	{name: "recap", description: locale.T("Summarize what the session has done so far", "Кратко пересказать, что уже сделано в сессии"), execution: "send", output: "transcript"},
	{name: "fast", description: locale.T("Toggle fast reply mode", "Переключить быстрый режим ответа"), execution: "send", output: "transcript"},
	{name: "version", description: locale.T("Claude Code version", "Версия Claude Code"), execution: "send", output: "transcript"},
	{name: "agents", description: locale.T("Subagents: list and settings", "Субагенты: список и настройка"), execution: "menu", output: "screen"},
	{name: "reload-plugins", description: locale.T("Reload plugins without a restart", "Перечитать плагины без перезапуска"), execution: "send", output: "transcript"},
	{name: "reload-skills", description: locale.T("Reload skills without a restart", "Перечитать навыки без перезапуска"), execution: "send", output: "transcript"},
	{name: "model", description: locale.T("Change the model", "Сменить модель"), argumentHint: locale.T("[model]", "[модель]"), execution: "model", output: "transcript"},
	{name: "effort", description: locale.T("Change the reasoning effort", "Сменить глубину рассуждения"), argumentHint: locale.T("[level]", "[уровень]"), execution: "effort", output: "transcript"},
	{name: "usage", aliases: []string{"cost", "stats"}, description: locale.T("Subscription limits and usage", "Лимиты подписки и расход"), execution: "menu", output: "screen"},
	{name: "status", description: locale.T("Version, model, account and connections of the session", "Версия, модель, аккаунт и подключения сессии"), execution: "menu", output: "screen"},
	{name: "mcp", description: locale.T("MCP servers: connections and status", "MCP-серверы: подключение и статус"), execution: "menu", output: "screen"},
	{name: "permissions", aliases: []string{"allowed-tools"}, description: locale.T("Permission rules for tools", "Правила разрешений для инструментов"), execution: "menu", output: "screen"},
	{name: "hooks", description: locale.T("Session hooks", "Хуки сессии"), execution: "menu", output: "screen"},
	{name: "plugin", aliases: []string{"plugins"}, description: locale.T("Plugins: install and enable", "Плагины: установка и включение"), execution: "menu", output: "screen"},
	{name: "skills", description: locale.T("List of skills", "Список навыков"), execution: "menu", output: "screen"},
	{name: "export", description: locale.T("Export the conversation to a file", "Выгрузить диалог в файл"), execution: "menu", output: "screen"},
	{name: "tasks", aliases: []string{"bashes"}, description: locale.T("Background tasks of the session", "Фоновые задачи сессии"), execution: "menu", output: "screen"},
	{name: "config", aliases: []string{"settings"}, description: locale.T("Claude Code settings", "Настройки Claude Code"), execution: "menu", output: "screen"},
	{name: "resume", aliases: []string{"continue"}, description: locale.T("Resume a previous session", "Продолжить одну из прошлых сессий"), argumentHint: locale.T("[session]", "[сессия]"), execution: "menu", output: "screen"},
	{name: "clear", aliases: []string{"reset", "new"}, description: locale.T("Start the conversation over", "Начать диалог заново"), execution: "restart", output: "none"},
	{name: "help", description: locale.T("All commands", "Все команды"), execution: "help", output: "none"},
	{name: "exit", aliases: []string{"quit"}, description: locale.T("End the agent session", "Завершить сессию агента"), execution: "confirm", output: "none",
		note: locale.T("The agent in this conversation will stop. You can continue by restarting it.", "Агент в этом диалоге остановится. Продолжить можно будет перезапуском.")},
	{name: "copy", description: locale.T("Copy the last reply", "Скопировать последний ответ"), execution: "blocked", output: "none", note: copyNote},
	{name: "login", description: locale.T("Log in to the account", "Войти в аккаунт"), execution: "blocked", output: "none", note: loginNote},
	{name: "logout", description: locale.T("Log out of the account", "Выйти из аккаунта"), execution: "blocked", output: "none", note: loginNote},
}, terminalRows([]terminalRow{
	{"theme", locale.T("Terminal theme", "Тема терминала")},
	{"terminal-setup", locale.T("Set up terminal key bindings", "Настроить клавиши терминала")},
	{"memory", locale.T("Edit memory files", "Редактировать файлы памяти")},
	{"ide", locale.T("Connect to an IDE", "Подключение к IDE")},
	{"tui", locale.T("Terminal rendering mode", "Режим отрисовки терминала")},
	{"color", locale.T("Session color", "Цвет сессии")},
	{"scroll-speed", locale.T("Scroll speed", "Скорость прокрутки")},
	{"output-style", locale.T("Reply style", "Стиль ответов")},
	{"btw", locale.T("Ask a side question without interrupting the work", "Вопрос в сторону, не прерывая работу")},
	{"doctor", locale.T("Check the Claude Code installation", "Проверить установку Claude Code")},
	{"upgrade", locale.T("Upgrade the subscription", "Обновить подписку")},
	{"privacy-settings", locale.T("Privacy settings", "Настройки приватности")},
})...)

// codexBuiltins: the curated built-ins of Codex CLI 0.152. Codex keeps their results on its screen.
var codexBuiltins = append([]builtin{
	{name: "model", description: locale.T("Choose the model and reasoning effort", "Выбрать модель и глубину рассуждения"), execution: "menu", output: "screen"},
	{name: "permissions", aliases: []string{"approvals"}, description: locale.T("What Codex is allowed to do", "Что Codex разрешено делать"), execution: "menu", output: "screen"},
	{name: "review", description: locale.T("Review the current changes", "Ревью текущих изменений"), execution: "menu", output: "reply"},
	{name: "fork", description: locale.T("Fork the conversation", "Ответвить диалог"), execution: "menu", output: "screen"},
	{name: "experimental", description: locale.T("Experimental features", "Экспериментальные возможности"), execution: "menu", output: "screen"},
	{name: "personality", description: locale.T("Codex's communication style", "Стиль общения Codex"), execution: "menu", output: "screen"},
	{name: "memories", description: locale.T("Codex memory", "Память Codex"), execution: "menu", output: "screen"},
	{name: "hooks", description: locale.T("Hooks and their trust", "Хуки и доверие к ним"), execution: "menu", output: "screen"},
	{name: "resume", description: locale.T("Resume a previous session", "Продолжить одну из прошлых сессий"), execution: "menu", output: "screen"},
	{name: "init", description: locale.T("Create an AGENTS.md with instructions for Codex", "Создать AGENTS.md с инструкциями для Codex"), execution: "send", output: "reply", welcome: true},
	{name: "compact", description: locale.T("Compact the conversation history to free up context", "Сжать историю диалога, чтобы освободить контекст"), execution: "send", output: "reply"},
	{name: "plan", description: locale.T("Plan mode", "Режим плана"), execution: "send", output: "reply"},
	{name: "fast", description: locale.T("Fast mode: 1.5 times faster, higher usage", "Быстрый режим: в 1,5 раза быстрее, расход выше"), execution: "send", output: "none"},
	{name: "goal", description: locale.T("Set a goal that Codex works toward on its own", "Поставить цель, к которой Codex идёт сам"), argumentHint: locale.T("[goal|clear|edit|pause|resume]", "[цель|clear|edit|pause|resume]"), execution: "send", output: "reply", welcome: true},
	{name: "rename", description: locale.T("Rename the conversation", "Переименовать диалог"), argumentHint: locale.T("<name>", "<имя>"), execution: "send", output: "none"},
	{name: "cd", description: locale.T("Change the working folder", "Сменить рабочую папку"), argumentHint: locale.T("<path>", "<путь>"), execution: "send", output: "none"},
	{name: "stop", description: locale.T("Stop background tasks", "Остановить фоновые задачи"), execution: "send", output: "none"},
	{name: "status", description: locale.T("Model, folder, permissions and limits of the session", "Модель, папка, права и лимиты сессии"), execution: "send", output: "screen"},
	{name: "diff", description: locale.T("Show the git diff, new files included", "Показать git diff, включая новые файлы"), execution: "send", output: "screen"},
	{name: "mcp", description: locale.T("Connected MCP tools", "Подключённые MCP-инструменты"), argumentHint: plain("[verbose]"), execution: "send", output: "screen"},
	{name: "usage", description: locale.T("Token usage", "Расход токенов"), argumentHint: plain("[daily|weekly|cumulative]"), execution: "send", output: "screen"},
	{name: "rollout", description: locale.T("Path to the session file", "Путь к файлу сессии"), execution: "send", output: "screen"},
	{name: "ps", description: locale.T("Background terminals", "Фоновые терминалы"), execution: "send", output: "screen"},
	{name: "pwd", description: locale.T("Current folder", "Текущая папка"), execution: "send", output: "screen"},
	{name: "new", description: locale.T("Start the conversation over", "Начать диалог заново"), execution: "restart", output: "none"},
	{name: "clear", description: locale.T("Clear the screen and start over", "Очистить экран и начать заново"), execution: "restart", output: "none"},
	{name: "skills", description: locale.T("Skills: invoked with $", "Навыки: вызываются через $"), execution: "skills", output: "none"},
	{name: "exit", aliases: []string{"quit"}, description: locale.T("End the Codex session", "Завершить сессию Codex"), execution: "confirm", output: "none",
		note: locale.T("Codex in this conversation will stop. You can continue by restarting it.", "Codex в этом диалоге остановится. Продолжить можно будет перезапуском.")},
	{name: "archive", description: locale.T("Archive the conversation", "Убрать диалог в архив"), execution: "confirm", output: "none", note: locale.T("The conversation goes to Codex's archive and the session ends.", "Диалог уйдёт в архив Codex, а сессия завершится.")},
	{name: "delete", description: locale.T("Delete the conversation", "Удалить диалог"), execution: "confirm", output: "none", note: locale.T("The Codex conversation will be deleted for good and cannot be restored.", "Диалог Codex удалится навсегда, восстановить его не получится."), danger: true},
	{name: "copy", description: locale.T("Copy the last reply", "Скопировать последний ответ"), execution: "blocked", output: "none", note: copyNote},
	{name: "logout", description: locale.T("Log out of the account", "Выйти из аккаунта"), execution: "blocked", output: "none", note: loginNote},
}, terminalRows([]terminalRow{
	{"side", locale.T("Side conversation", "Побочный диалог")},
	{"btw", locale.T("Side question", "Вопрос в сторону")},
	{"agent", locale.T("Switch to another agent thread", "Переключиться на другой агентный тред")},
	{"subagents", locale.T("Subagents", "Субагенты")},
	{"mention", locale.T("Mention a file", "Упомянуть файл")},
	{"theme", locale.T("Highlighting theme", "Тема подсветки")},
	{"title", locale.T("Terminal window title", "Заголовок окна терминала")},
	{"statusline", locale.T("Status line", "Строка статуса")},
	{"pet", locale.T("Terminal pet", "Питомец в терминале")},
	{"keymap", locale.T("Remap keys", "Переназначить клавиши")},
	{"vim", locale.T("Vim mode in the input", "Режим Vim в поле ввода")},
	{"apps", locale.T("Apps", "Приложения")},
	{"plugins", locale.T("Plugins", "Плагины")},
	{"ide", locale.T("Context from the IDE", "Контекст из IDE")},
	{"app", locale.T("Open the Codex app", "Открыть приложение Codex")},
	{"import", locale.T("Import settings", "Импорт настроек")},
	{"approve", locale.T("Allow a retry after an auto-review refusal", "Разрешить повтор после отказа авто-ревью")},
	{"debug-config", locale.T("Debug configuration layers", "Отладка слоёв конфигурации")},
	{"test-approval", locale.T("Test a permission request", "Проверка запроса разрешения")},
	{"feedback", locale.T("Send feedback", "Отправить отзыв")},
	{"raw", locale.T("Raw output", "Сырой вывод")},
	{"setup-default-sandbox", locale.T("Set up the sandbox", "Настроить песочницу")},
	{"sandbox-add-read-dir", locale.T("Let the sandbox read a folder", "Разрешить песочнице читать папку")},
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
