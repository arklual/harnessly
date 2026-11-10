package apps

import "sort"

// Surface is the one description of what an app reaches in the host (HRN-690): the places it works in, the rights it
// asks for, the methods of the bridge and what each needs, the host tools of the agent. The manifest check, the bridge
// and the catalog read it here; the page's table (web/app/next/features/app-views/surface.gen.ts), the reference
// (docs/apps/reference.md) and the JSON Schema of app.json (docs/apps/app.schema.json) are written from it by
// `UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent`, and TestSurfaceFilesAreCurrent fails the build when one of them was edited by hand.

// SurfaceTarget is a place an app works in.
type SurfaceTarget struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Slot: a place of the interface filled by a view at a spot the page declares (HRN-753).
	Slot bool `json:"slot,omitempty"`
	// Pattern: the name stands for a family (sinopia.<kind>).
	Pattern bool `json:"pattern,omitempty"`
	// Owner is the builtin app whose screen the target names, as an alias (builtin.go); "" for the host's own.
	Owner string `json:"owner,omitempty"`
}

// SurfacePermission is a right an app asks for in `permissions`.
type SurfacePermission struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Param is what follows the right after ':' (files.read:<mask>); "" is a right written alone.
	Param string `json:"param,omitempty"`
	// Alone is the label of a right with a Param that may also be written without one (terminal.run: any command).
	Alone Text `json:"alone,omitzero"`
	// Unsupported: the host accepts the right in no call yet, so the check refuses it.
	Unsupported bool `json:"unsupported,omitempty"`
	// Class is how much the right lets an app do (HRN-849): it orders the install screen and says how loud it is, it
	// forbids nothing.
	Class string `json:"class"`
	// ParamClass is the class with a Param when it is not Class (secrets.write alone is the app's own keys, with a name
	// the person's shared one).
	ParamClass string `json:"paramClass,omitempty"`
	// Label is what the install screen says, in the person's words; {param} stands for what follows ':'.
	Label Text `json:"label"`
	// Threat is the second sentence of a high or critical right: what it can do to the person.
	Threat Text `json:"threat,omitzero"`
}

// Text is a line in both languages of the page.
type Text struct {
	Ru string `json:"ru"`
	En string `json:"en"`
}

// The classes of risk (HRN-849), from the most dangerous; the install screen lists them in this order.
const (
	ClassCritical = "critical"
	ClassHigh     = "high"
	ClassMedium   = "medium"
	ClassLow      = "low"
)

// Classes are the classes from the most dangerous.
var Classes = []string{ClassCritical, ClassHigh, ClassMedium, ClassLow}

// Loud says whether a class is shown with its threat and journals what it does: high and critical.
func Loud(class string) bool { return class == ClassHigh || class == ClassCritical }

// SurfaceMethod is a method of the host↔view bridge.
type SurfaceMethod struct {
	Name string `json:"name"`
	// Permission is the right the method needs; "" needs none.
	Permission string `json:"permission"`
	// SDK is how a view calls it through @harnessly/app-sdk.
	SDK string `json:"sdk,omitempty"`
	Doc string `json:"doc"`
	// Deprecated names what to call instead. The method keeps working (the SDK only grows, HRN-849); the app's inspector
	// says it is deprecated and what replaces it.
	Deprecated string `json:"deprecated,omitempty"`
}

// EventKinds are what events:<kind> names: what happened in the chat, without its content (HRN-850).
var EventKinds = []string{"turn", "tool", "message"}

// SDKProtocols are the versions of the bridge's protocol the host speaks (HRN-849). A view names its own with
// harnessly/protocol after the handshake (1 when it names none); the host speaks every one listed, and a
// behavior that is new is the view's only when it asked for the version that brings it. 2: a refused call resolves
// to {error: 'denied' | 'unsupported' | 'failed', code, message} instead of rejecting.
var SDKProtocols = []int{1, 2}

// SurfaceTool is a host tool the agent of a chat with apps gets from the harnessly-app server.
type SurfaceTool struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	// Owner is the builtin app whose backend answers the tool; "" for the host's own.
	Owner string `json:"owner,omitempty"`
}

var surfaceTargets = []SurfaceTarget{
	{Name: TargetChat, Doc: "The chat: the agent profile, /-commands of the palette, cards and widgets in the feed."},
	{Name: TargetPane, Doc: "A tab of the right pane beside the chat."},
	{Name: TargetApp, Doc: "A full screen of its own, opened from the apps page or by link."},
	{Name: TargetSettings, Doc: "A page in the settings.", Slot: true},
	{Name: TargetOnboarding, Doc: "A step of the first run.", Slot: true},
	{Name: TargetObjectAction, Doc: "A button at an object (a machine, a chat) that opens the view.", Slot: true},
	{Name: TargetHubSettings, Doc: "A page in the settings of a Hub.", Slot: true},
	{Name: TargetBranding, Doc: "The product's name and accent; declared values, no view.", Slot: true},
}

var surfacePermissions = []SurfacePermission{
	{Name: "chat.prompt", Class: ClassMedium, Doc: "Send a message to the agent of the chat (hl.chat.send).",
		Label: Text{Ru: "Отправлять сообщения агенту в этом чате", En: "Send messages to the agent of this chat"}},
	{Name: "chat.read", Class: ClassMedium, Doc: "Read the chat's messages.",
		Label: Text{Ru: "Читать сообщения чата", En: "Read the chat's messages"}},
	{Name: "chat.card", Class: ClassMedium, Doc: "Put a card of the app in the chat's feed.",
		Label: Text{Ru: "Показывать свои карточки в ленте чата", En: "Show its cards in the chat's feed"}},
	{Name: "agents.read", Class: ClassMedium, Doc: "List the agents of the workspace.",
		Label: Text{Ru: "Видеть список агентов пространства", En: "See the list of the workspace's agents"}},
	{Name: "pane.open", Class: ClassLow, Doc: "Open a view of the app in the right pane.",
		Label: Text{Ru: "Открывать своё окно в правой панели", En: "Open its view in the right pane"}},
	{Name: "storage", Class: ClassLow, Doc: "Keep data of the app per chat, per document and per person.",
		Label: Text{Ru: "Хранить свои данные", En: "Keep its own data"}},
	{Name: "storage.space.write", Class: ClassMedium, Doc: "Write the storage area shared by the whole space.",
		Label: Text{Ru: "Писать в общие данные пространства — их видят все участники", En: "Write data shared by the whole space — everyone in it sees them"}},
	{Name: "files.read", Param: "<mask>", Class: ClassMedium, Doc: "Read workspace files matching the mask.",
		Label: Text{Ru: "Читать файлы {param}", En: "Read the files {param}"}},
	{Name: "files.write", Param: "<mask>", Class: ClassMedium, Doc: "Write workspace files matching the mask.",
		Label: Text{Ru: "Менять файлы {param}", En: "Change the files {param}"}},
	{Name: "files.watch", Class: ClassMedium, Doc: "Hear which files under the files.read masks change (hl.files.watch); the names, not the contents.",
		Label: Text{Ru: "Следить, какие из этих файлов меняются", En: "Notice which of these files change"}},
	{Name: "git.read", Class: ClassMedium, Doc: "See git status, diffs and history of the chat's folder (secret files left out).",
		Label: Text{Ru: "Видеть изменения и историю git в папке чата", En: "See git changes and history in the chat's folder"}},
	{Name: "events", Param: "<kind>", Class: ClassMedium, Doc: "Hear what happens in the chat: turn (the agent's turn ended), tool (the agent called a tool), message (a message came).",
		Label: Text{Ru: "Узнавать в чате: {param}", En: "Hear in the chat: {param}"}},
	{Name: "secrets", Param: "<name>", Class: ClassHigh, Doc: "Have the machine put a secret the person typed into the app's server calls.",
		Label:  Text{Ru: "Использовать секрет {param}", En: "Use the secret {param}"},
		Threat: Text{Ru: "Сможет действовать от вашего имени там, куда ведёт этот ключ.", En: "It can act on your behalf wherever this key leads."}},
	{Name: "secrets.write", Param: "<name>", Class: ClassMedium, ParamClass: ClassHigh, Doc: "Put a value into the safe, never read it back (hl.secrets.put). Alone: the app's own secrets, those it holds secrets:<name> for; with a name (secrets.write:GITHUB_TOKEN): that secret of the person's safe, the one the agent uses.",
		Label:  Text{Ru: "Заменять секрет {param}", En: "Replace the secret {param}"},
		Alone:  Text{Ru: "Сохранять свои ключи в сейф", En: "Keep its own keys in the safe"},
		Threat: Text{Ru: "Сможет подменить ключ, которым пользуется агент.", En: "It can swap the key the agent uses."}},
	{Name: "terminal.run", Param: "<command>", Class: ClassHigh,
		Doc:   "Run a command in the chat's folder inside the agent's sandbox and get its exit code and output (hl.terminal.run). Alone: any command, through the shell; with a mask (terminal.run:npm test*, * is any run of characters but an option): only the commands it matches, without the shell; an option (a word starting with -) is matched only where the mask writes it.",
		Label: Text{Ru: "Запускать команды {param} в папке чата", En: "Run the commands {param} in the chat's folder"},
		Alone: Text{Ru: "Запускать любые команды в папке чата", En: "Run any command in the chat's folder"},
		Threat: Text{Ru: "Команда работает с правами агента: может менять и удалять файлы проекта и ходить в сеть.",
			En: "A command has the agent's rights: it can change and delete the project's files and reach the network."}},
	{Name: "process", Param: "<command>", Class: ClassHigh, Doc: "Start and stop long commands in the chat's folder inside the agent's sandbox (a dev server, a watcher), read their output, and open a local port in the right pane's browser (hl.process, hl.preview.open). Alone: any command, through the shell; with a mask (process:npm run dev*): only the commands it matches, as terminal.run's masks.",
		Label: Text{Ru: "Запускать долгие процессы {param} в папке чата и показывать их в панели", En: "Start the long processes {param} in the chat's folder and show them in the pane"},
		Alone: Text{Ru: "Запускать любые долгие процессы (dev-сервер) в папке чата и показывать их в панели", En: "Start any long process (a dev server) in the chat's folder and show it in the pane"},
		Threat: Text{Ru: "Процесс работает с правами агента, пока его не остановят: может менять файлы проекта и ходить в сеть.",
			En: "A process has the agent's rights until it is stopped: it can change the project's files and reach the network."}},
	{Name: "terminal.pty", Class: ClassCritical, Doc: "Open a shell on a terminal of its own in the chat's folder inside the agent's sandbox, type into it and get what it prints (hl.terminal.open): a whole terminal, as the right pane's own. 4 per app in a chat.",
		Label: Text{Ru: "Открывать свой терминал в папке чата", En: "Open its own terminal in the chat's folder"},
		Threat: Text{Ru: "В терминале можно выполнить что угодно с правами агента: менять и удалять файлы проекта, ходить в сеть.",
			En: "Anything can be run in the terminal with the agent's rights: change and delete the project's files, reach the network."}},
	{Name: "terminal.read", Class: ClassHigh, Doc: "Read what the chat's terminals show: the agent's and the chat's own shells, as text (hl.terminal.read).",
		Label: Text{Ru: "Видеть, что показывают терминалы чата", En: "See what the chat's terminals show"},
		Threat: Text{Ru: "В терминале может быть напечатано то, что не для чужих глаз: ключи, адреса, вывод команд.",
			En: "A terminal may show what is not for other eyes: keys, addresses, the output of commands."}},
	{Name: "browser.read", Class: ClassHigh, Doc: "See the agent's browser of the chat: its tabs, a page's text and elements, a picture of it (hl.browser.read).",
		Label: Text{Ru: "Видеть браузер агента в этом чате", En: "See the agent's browser in this chat"},
		Threat: Text{Ru: "Увидит открытые страницы и то, что на них: письма, кабинеты, личные данные.",
			En: "It sees the open pages and what is on them: mail, accounts, personal data."}},
	{Name: "browser.drive", Class: ClassHigh, Doc: "Act in the agent's browser of the chat: open and close tabs, go to an address, click, type, press keys, pick options (hl.browser.drive). Passwords, files and scripts stay out.",
		Label: Text{Ru: "Действовать в браузере агента в этом чате", En: "Act in the agent's browser in this chat"},
		Threat: Text{Ru: "Сможет нажимать и печатать на открытых страницах от вашего имени: отправить форму, написать письмо.",
			En: "It can click and type on the open pages on your behalf: send a form, write a message."}},
	{Name: "hooks.agent", Class: ClassCritical, Doc: "Hear each tool call of the chat's agent before it runs, and stop it or add context for the agent (hl.hooks.on). The call is never changed. A view that does not answer in 5 s lets it through, and the journal says so.",
		Label: Text{Ru: "Видеть каждое действие агента до запуска и останавливать его", En: "See each action of the agent before it runs, and stop it"},
		Threat: Text{Ru: "Увидит, что агент делает и с какими данными, и сможет мешать его работе.",
			En: "It sees what the agent does and with what data, and can get in the way of its work."}},
	{Name: "hooks.strict", Class: ClassCritical, Doc: "Strict mode of hooks.agent: a call the app's view did not answer in time, or with no view open, is stopped instead of let through.",
		Label: Text{Ru: "Строгий режим: без ответа приложения агент не делает ничего", En: "Strict mode: without the app's answer the agent does nothing"},
		Threat: Text{Ru: "Если окно приложения закрыто или зависло, агент встанет, пока вы не выключите хук.",
			En: "If the app's view is closed or stuck, the agent stops until you turn the hook off."}},
	{Name: "background", Class: ClassCritical, Doc: "Run a view of the app hidden in the page while the app is on in a chat, without the person opening it: its hooks and events work in every such chat.",
		Label:  Text{Ru: "Работать в фоне во всех чатах, где оно включено", En: "Work in the background in every chat it is on in"},
		Threat: Text{Ru: "Будет работать, даже когда его окно не открыто.", En: "It works even when its view is not open."}},
	{Name: "notify", Class: ClassLow, Doc: "Show the person a notification.",
		Label: Text{Ru: "Показывать уведомления", En: "Show notifications"}},
	{Name: "tools.page", Class: ClassMedium, Doc: "Offer tools from the view's page that the agent of the chat calls (hl.tools.register, navigator.modelContext.registerTool).",
		Label: Text{Ru: "Давать агенту свои инструменты из окна", En: "Give the agent tools from its view"}},
	{Name: "context.publish", Class: ClassMedium, Doc: "Publish what the view shows for the agent of the chat to read (hl.publish).",
		Label: Text{Ru: "Показывать агенту, что открыто в окне", En: "Tell the agent what its view shows"}},
	{Name: "sinopia.read", Class: ClassMedium, Doc: "Read the Sinopia document the view is shown in.",
		Label: Text{Ru: "Читать документ Sinopia, в котором оно открыто", En: "Read the Sinopia document it is open in"}},
	{Name: "sinopia.write", Class: ClassMedium, Doc: "Change the Sinopia document: patches, fields, new boards.",
		Label: Text{Ru: "Менять документ Sinopia", En: "Change the Sinopia document"}},
	{Name: "sinopia.comment", Class: ClassLow, Doc: "Comment on the Sinopia document.",
		Label: Text{Ru: "Комментировать документ Sinopia", En: "Comment on the Sinopia document"}},
	{Name: "sinopia.kind", Class: ClassLow, Doc: "Add Sinopia document types of the app's own.",
		Label: Text{Ru: "Добавлять свои типы документов Sinopia", En: "Add Sinopia document types of its own"}},
	{Name: "sinopia.export", Class: ClassLow, Doc: "Render the document to a file.", Unsupported: true,
		Label: Text{Ru: "Выгружать документ в файл", En: "Render the document to a file"}},
	{Name: "ai", Class: ClassHigh, Doc: "Call a model on the person's behalf, within the app's budget of the day (manifest `budget.usdPerDay`, $0.50 by default, $5 at most).",
		Label:  Text{Ru: "Вызывать модель от вашего имени — до {param} в день", En: "Call a model on your behalf — up to {param} a day"},
		Threat: Text{Ru: "Будет тратить ваш лимит модели в пределах этой суммы.", En: "It spends your model limit, up to this amount."}},
	{Name: "agents.run", Class: ClassHigh, Doc: "Start a task in a new chat under the app's chat, within the app's budget of the day (manifest `budget.runsPerDay`, 3 by default, 20 at most).",
		Label:  Text{Ru: "Запускать агента в новом чате — до {param} раз в день", En: "Start the agent in a new chat — up to {param} times a day"},
		Threat: Text{Ru: "Агент в новом чате работает с вашими правами и тратит ваш лимит модели.", En: "The agent of the new chat works with your rights and spends your model limit."}},
	{Name: "users.read", Class: ClassMedium, Doc: "Read the people of the space.", Unsupported: true,
		Label: Text{Ru: "Видеть людей пространства", En: "See the people of the space"}},
}

var surfaceMethods = []SurfaceMethod{
	{Name: "ui/initialize", Doc: "The view's handshake: the host answers its context."},
	{Name: "ui/notifications/initialized", Doc: "The view says it is ready."},
	{Name: "harnessly/protocol", SDK: "connect({protocol})", Doc: "The view names the version of the bridge's protocol it is written for (above 1); the host answers the one it speaks with it."},
	{Name: "harnessly/debug.log", Doc: "An error or console.error of the view (the SDK sends it), for the debug section of the app's inspector."},
	{Name: "ui/notifications/size-changed", SDK: "hl.resize(height)", Doc: "The view asks for a height."},
	{Name: "ui/open-link", Doc: "Open an address in a new tab."},
	{Name: "tools/call", SDK: "hl.tools.call(name, args)", Doc: "Call a tool of the app's own MCP server."},
	{Name: "ui/message", Permission: "chat.prompt", SDK: "hl.chat.send(text)", Doc: "A message to the agent of this chat, signed with the app's name."},
	{Name: "harnessly/chat.read", Permission: "chat.read", SDK: "hl.chat.read({last})", Doc: "The chat's last messages."},
	{Name: "harnessly/chat.card", Permission: "chat.card", SDK: "hl.chat.card(view, data)", Doc: "A card of the app in the feed."},
	{Name: "harnessly/pane.open", Permission: "pane.open", SDK: "hl.pane.open(target)", Doc: "Open a view of the app in the right pane."},
	{Name: "harnessly/agents.list", Permission: "agents.read", SDK: "hl.agents.list()", Doc: "The agents of the workspace."},
	{Name: "harnessly/agents.run", Permission: "agents.run", SDK: "hl.agents.run(prompt, {title})", Doc: "Start a task in a new chat under this one: {id}. The person sees it in the sidebar; counts against budget.runsPerDay."},
	{Name: "harnessly/ai.complete", Permission: "ai", SDK: "hl.ai.complete(prompt, {system, maxTokens, model})", Doc: "One answer of a model, no tools: {text, usd, spentToday, budget}. model is fast (default) or smart; refused as over_budget when the day's budget is spent."},
	{Name: "harnessly/secrets.put", Permission: "secrets.write", SDK: "hl.secrets.put(name, value, {shared})", Doc: "Put a value into the safe: {created}. The app's own secret by default; shared: true puts the person's secret of that name (secrets.write:<name>). The value is never given back."},
	{Name: "harnessly/storage.get", Permission: "storage", SDK: "hl.storage.<scope>.get(key)", Doc: "Read a key of the app's storage."},
	{Name: "harnessly/storage.set", Permission: "storage", SDK: "hl.storage.<scope>.set(key, value, {ifVersion})", Doc: "Write a key; ifVersion writes only over that version."},
	{Name: "harnessly/storage.delete", Permission: "storage", SDK: "hl.storage.<scope>.delete(key)", Doc: "Remove a key."},
	{Name: "harnessly/storage.keys", Permission: "storage", SDK: "hl.storage.<scope>.keys()", Doc: "The keys of an area."},
	{Name: "harnessly/settings.get", SDK: "hl.settings.get()", Doc: "The app's settings as the person saved them."},
	{Name: "harnessly/notify", Permission: "notify", SDK: "hl.notify(text)", Doc: "A notification to the person."},
	{Name: "harnessly/files.read", Permission: "files.read", SDK: "hl.files.read(path)", Doc: "Read a workspace file inside the app's mask."},
	{Name: "harnessly/files.write", Permission: "files.write", SDK: "hl.files.write(path, text)", Doc: "Write a workspace file inside the app's mask."},
	{Name: "harnessly/files.list", Permission: "files.read", SDK: "hl.files.list(folder)", Doc: "The files and folders of a folder that the app's masks reach."},
	{Name: "harnessly/files.search", Permission: "files.read", SDK: "hl.files.search(text, {glob, case, limit})", Doc: "Lines that hold a text in the files the app may read; with a glob alone, the files by name."},
	{Name: "harnessly/files.watch", Permission: "files.watch", SDK: "hl.files.watch(onChange)", Doc: "Start hearing which files under the masks change: harnessly/notifications/files-changed {paths}."},
	{Name: "harnessly/files.unwatch", Permission: "files.watch", SDK: "the function hl.files.watch returns", Doc: "Stop hearing file changes."},
	{Name: "harnessly/files.pick", SDK: "hl.files.pick({accept})", Doc: "The person picks a file of the chat's folder in the host's dialog; the view gets its path and text once, no mask needed. null when the person closes it."},
	{Name: "harnessly/git.status", Permission: "git.read", SDK: "hl.git.status()", Doc: "The branch and the changed files of the chat's folder."},
	{Name: "harnessly/git.diff", Permission: "git.read", SDK: "hl.git.diff({path, staged, ref})", Doc: "The unified diff of the chat's folder or a path in it."},
	{Name: "harnessly/git.log", Permission: "git.read", SDK: "hl.git.log({limit, path})", Doc: "The last commits."},
	{Name: "harnessly/terminal.run", Permission: "terminal.run", SDK: "hl.terminal.run(command, {cwd, timeout})", Doc: "Run a command in the chat's folder (or cwd in it) inside the agent's sandbox: {code, output, cut, timedOut, ms}. Timeout in seconds, 120 by default, 600 at most; 1 MiB of output."},
	{Name: "harnessly/process.start", Permission: "process", SDK: "hl.process.start(command, {cwd})", Doc: "Start a long command in the chat's folder inside the agent's sandbox: {id, command, running}. It runs until stopped, until the app is turned off in the chat, or until the runtime stops; 4 per app in a chat."},
	{Name: "harnessly/process.stop", Permission: "process", SDK: "hl.process.stop(id)", Doc: "Stop a process the app started (SIGTERM, then SIGKILL after 3 s)."},
	{Name: "harnessly/process.list", Permission: "process", SDK: "hl.process.list()", Doc: "The app's processes in this chat, running and recently ended, with their exit codes."},
	{Name: "harnessly/process.output", Permission: "process", SDK: "hl.process.output(id, {since})", Doc: "A process's output from an offset: {text, next, running, code, lost}; the machine keeps its last 256 KiB."},
	{Name: "harnessly/preview.open", Permission: "process", SDK: "hl.preview.open(port, {path})", Doc: "Open http://localhost:<port><path> in the right pane's browser, where the person sees the app's dev server."},
	{Name: "harnessly/hooks.subscribe", Permission: "hooks.agent", SDK: "hl.hooks.on('tool', fn)", Doc: "Hear the agent's tool calls before they run: harnessly/notifications/hook {id, tool, input, cut} (cut: the input was too big to send, 60 KiB); fn answers {decision: 'deny', reason} to stop it, {context} to add a line for the agent, or nothing to let it through."},
	{Name: "harnessly/hooks.result", Permission: "hooks.agent", Doc: "The view's word on a call it heard (the SDK sends it): {id, decision, reason, context}."},
	{Name: "harnessly/browser.read", Permission: "browser.read", SDK: "hl.browser.read(tool, args)", Doc: "A look at the agent's browser of the chat: tool is tabs, snapshot, screenshot or wait_for (the chrome_* tools' arguments); the answer is the tool's content, the page's text marked untrusted."},
	{Name: "harnessly/browser.drive", Permission: "browser.drive", SDK: "hl.browser.drive(tool, args)", Doc: "An action in the agent's browser of the chat: tool is open, navigate, close_tab, resize, click, hover, type, press_key, select or dialog; refs come from a snapshot. No password, file or script."},
	{Name: "harnessly/terminal.open", Permission: "terminal.pty", SDK: "hl.terminal.open({cols, rows, cwd})", Doc: "Open a shell on a terminal of its own in the chat's folder (or cwd in it) inside the agent's sandbox: {id}. What it prints comes as harnessly/notifications/terminal-output {id, data} (bytes), its end as harnessly/notifications/terminal-exit {id, code}."},
	{Name: "harnessly/terminal.write", Permission: "terminal.pty", SDK: "terminal.write(data)", Doc: "Type into an open terminal: a string or bytes, 64 KiB at most a call. Not counted in the 60 calls a second."},
	{Name: "harnessly/terminal.resize", Permission: "terminal.pty", SDK: "terminal.resize(cols, rows)", Doc: "Set an open terminal's size."},
	{Name: "harnessly/terminal.close", Permission: "terminal.pty", SDK: "terminal.close()", Doc: "End the shell of a terminal and everything it started."},
	{Name: "harnessly/terminal.read", Permission: "terminal.read", SDK: "hl.terminal.read({terminal, lines})", Doc: "What the chat's terminals show, as text: {terminals: [{id, text}]}; id \"agent\" is the agent's own, the others the chat's shells; lines of history, 200 by default, 2000 at most."},
	{Name: "harnessly/events.subscribe", Permission: "events", SDK: "hl.events.on(kind, fn)", Doc: "Hear the chat's events of the kinds the app holds events:<kind> for: harnessly/notifications/event {kind, at, …}."},
	{Name: "harnessly/tools.register", Permission: "tools.page", SDK: "hl.tools.register(tool, run)", Doc: "Offer the view's tools to the agent; the whole list each time."},
	{Name: "harnessly/tools.result", Permission: "tools.page", Doc: "The view's answer to an agent's call of its tool (the SDK sends it)."},
	{Name: "harnessly/context.publish", Permission: "context.publish", SDK: "hl.publish(data)", Doc: "What the view shows, for the agent to read with page_tools."},
	{Name: "harnessly/ui.toast", SDK: "hl.ui.toast(text, {tone, description})", Doc: "A short message at the bottom of the page; tone info, success, warning or error, and a second line."},
	{Name: "harnessly/ui.confirm", SDK: "hl.ui.confirm(text)", Doc: "A yes/no question in the host's own dialog."},
	{Name: "harnessly/keys.on", SDK: "hl.keys.on(scope, handlers)", Doc: "Run the host's shortcut commands of a screen (deck, video, site, pictures, files) while the view is up, on the keys the person set for them: harnessly/notifications/key {command, chord}."},
	{Name: "harnessly/keys.off", SDK: "the function hl.keys.on returns", Doc: "Stop running a screen's shortcut commands."},
	{Name: "harnessly/keys.press", SDK: "hl.keys.press(chord, {typing})", Doc: "A key pressed inside the view, run as the host's shortcut would run it; only while the view has the focus. Answers {ran}."},
	{Name: "harnessly/keys.hints", SDK: "hl.keys.hints(commands)", Doc: "The keys the person has for the host's commands, as the host writes them: {hints: {command: label or null}}."},
	{Name: "harnessly/slot.done", SDK: "hl.slot.done()", Doc: "A slot view (an onboarding step) says it is finished."},
	{Name: "harnessly/sinopia.document", Permission: "sinopia.read", SDK: "hl.sinopia.document()", Doc: "The document the view is shown in."},
	{Name: "harnessly/sinopia.selection", Permission: "sinopia.read", SDK: "hl.sinopia.selection()", Doc: "What the person selected."},
	{Name: "harnessly/sinopia.node", Permission: "sinopia.read", SDK: "hl.sinopia.node(id)", Doc: "One node of the document."},
	{Name: "harnessly/sinopia.query", Permission: "sinopia.read", SDK: "hl.sinopia.query(selector, options)", Doc: "Nodes matching a selector."},
	{Name: "harnessly/sinopia.fields", Permission: "sinopia.read", SDK: "hl.sinopia.fields()", Doc: "The values of the bound fields."},
	{Name: "harnessly/sinopia.mark", Permission: "sinopia.read", SDK: "hl.sinopia.mark(nodes, options)", Doc: "Highlight nodes in the editor."},
	{Name: "harnessly/sinopia.patch", Permission: "sinopia.write", SDK: "hl.sinopia.patch(ops)", Doc: "Change the document."},
	{Name: "harnessly/sinopia.setFields", Permission: "sinopia.write", SDK: "hl.sinopia.setFields(patch)", Doc: "Write bound fields back."},
	{Name: "harnessly/sinopia.boards.add", Permission: "sinopia.write", SDK: "hl.sinopia.boards.add(options)", Doc: "Add a board to the document."},
	{Name: "harnessly/sinopia.render", Permission: "sinopia.export", SDK: "hl.sinopia.render(options)", Doc: "Render the document."},
	{Name: "harnessly/sinopia.comment", Permission: "sinopia.comment", SDK: "hl.sinopia.comment(node, text)", Doc: "Comment on a node of the document."},
}

var surfaceTools = []SurfaceTool{
	{Name: "open", Doc: "Open an app's view in the person's right pane."},
	{Name: "data_get", Doc: "Read what the app keeps for this chat under a key."},
	{Name: "data_set", Doc: "Write what the app keeps for this chat under a key."},
	{Name: "settings_get", Doc: "Read the app's settings."},
	{Name: "logs", Doc: "Read the app's log: what its views reported and what the host refused."},
	{Name: "check", Doc: "Open an app's view in a headless browser of the machine and judge how it loaded."},
	{Name: "ask", Doc: "Put a choice to the person in the app's view and wait for the answer."},
	{Name: "widget_show", Doc: "Show an app's chat view in the feed with the data given."},
	{Name: "widget_render", Doc: "Draw a widget written on the spot in the feed."},
	{Name: "page_tools", Doc: "The tools and the context the open views of the chat's apps offer from the page."},
	{Name: "page_call", Doc: "Call a tool an open view offers; the view runs it in the person's page."},
	{Name: "catalog", Doc: "The places, rights, methods and contribution kinds an app can use."},
}

// The tables the check and the bridge read, made from the surface once.
var (
	methodPermission       = map[string]string{}
	permissions            = map[string]bool{}
	unsupportedPermissions = map[string]bool{}
	paramPermissions       = map[string]bool{}
)

func init() {
	for _, m := range surfaceMethods {
		methodPermission[m.Name] = m.Permission
	}
	for _, p := range surfacePermissions {
		switch {
		case p.Param != "":
			paramPermissions[p.Name] = true
			if p.Alone.Ru != "" {
				permissions[p.Name] = true
			}
		default:
			permissions[p.Name] = true
		}
		if p.Unsupported {
			unsupportedPermissions[p.Name] = true
		}
	}
}

// Catalog is the surface as `harnessly app catalog` and the agent's catalog tool give it.
type Catalog struct {
	Targets       []SurfaceTarget     `json:"targets"`
	Permissions   []SurfacePermission `json:"permissions"`
	Methods       []SurfaceMethod     `json:"methods"`
	Contributions []ContributionKind  `json:"contributions"`
	Tools         []SurfaceTool       `json:"tools"`
}

// SurfaceCatalog is the whole surface: the host's own and what the builtin apps declare (their aliases, points and
// backend tools).
func SurfaceCatalog() Catalog {
	return Catalog{
		Targets: allTargets(), Permissions: surfacePermissions, Methods: surfaceMethods,
		Contributions: contributionKinds(), Tools: allTools(),
	}
}

// allTargets are the host's targets and then the builtin apps' aliases.
func allTargets() []SurfaceTarget {
	out := append([]SurfaceTarget(nil), surfaceTargets...)
	for _, m := range Builtins() {
		for _, a := range m.Aliases {
			out = append(out, SurfaceTarget{Name: a.Name, Doc: a.Doc, Pattern: true, Owner: m.ID})
		}
	}
	return out
}

// allTools are the host's tools and then the builtin apps' backend tools.
func allTools() []SurfaceTool {
	out := append([]SurfaceTool(nil), surfaceTools...)
	for _, m := range Builtins() {
		if m.Backend == nil {
			continue
		}
		for _, t := range m.Backend.Tools {
			out = append(out, SurfaceTool{Name: t.Name, Doc: t.Description, Owner: m.ID})
		}
	}
	return out
}

// PermissionNames are the rights in the order the surface lists them.
func PermissionNames() []string {
	out := make([]string, 0, len(surfacePermissions))
	for _, p := range surfacePermissions {
		out = append(out, p.Name)
	}
	return out
}

// methodsNeeding are the methods a right opens, sorted.
func methodsNeeding(permission string) []string {
	var out []string
	for _, m := range surfaceMethods {
		if m.Permission == permission {
			out = append(out, m.Name)
		}
	}
	sort.Strings(out)
	return out
}
