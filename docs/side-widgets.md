# Widgets under the right column and the left menu

On a computer the chat's right column (the recap «Что сейчас происходит», the pictures, the commands in the background)
has the person's **widgets** under it: small cards they add, move by hand, fold and remove. Some come with harnessly,
others an agent makes on request. And the person lays out the **left menu**: an installed app stands among the main
buttons or under «Приложения». Epic HRN-1509 (HRN-1510 widgets, HRN-1511 widgets by agents, HRN-1512 the menu).

## What the person sees

- **«+ Виджет»** under the column opens the gallery: «Лимиты» (five hours and the week left, by subscription),
  «Машина» (CPU, memory, disk), «Подчаты» (the delegated chats at work), «Выкат» (the person's merge requests and their
  pipelines, when GitLab is connected), «Заметка», «Часы» (the time and a timer of 5, 15 or 25 minutes), then the
  widgets agents made.
- A card's name folds it; its «⋯» folds or removes it; a card is carried by hand to another place.
- The list, the order, what is folded, a note's text and a running timer are the person's: they are kept in their
  settings (`prefs["hx-widgets"]`, the synced choices of HRN-1493), so every computer shows the same.
- The widgets show only where the column has room beside the chat and the right pane is shut. On a phone there is no
  room beside the chat; the limits are on the model's pill there.

## What stays cheap

- A folded card, or one scrolled out of the stack's view, draws nothing: no frame, no request, no timer. It keeps its
  last height while away, so the stack does not jump.
- The data come from the page's own stores (the limits the composer already asks for, the machine's load of the active
  machine, the chats' list); the widgets add no subscription of their own but the pipelines (every two minutes while the
  card is on screen and the page visible).
- The stack is a layer beside the feed: typing and the feed's scroll never draw it.

## Widgets an agent makes

The person says «сделай виджет, который показывает…»; the agent calls the MCP tool `widget_create` or
`harnessly widget create`. Both are one core (`internal/sidewidget`): the widget is a package `local.widget-<name>`
written under `<data>/side-widgets/<name>/` and installed as a dev install of that folder; the page meets the new view
of the `side` target and adds it to the person's widgets once (one they removed stays removed).

```bash
harnessly widget create limits-forecast --title "Прогноз лимитов" --sources limits --smart "Хватит ли лимита до вечера?" --every 30
harnessly widget update limits-forecast --every 15
harnessly widget list --side
harnessly widget remove --side limits-forecast
```

MCP tools: `widget_create`, `widget_update`, `widget_list`, `widget_remove` (the same fields as the flags).

The view's manifest (`views[].widget`, the `side` target, docs/apps/reference.md):

| Field | What it is |
| --- | --- |
| `size` | The card's height: `s` 96 px (default), `m` 160, `l` 240. |
| `refresh` | Seconds between the host's data given again while the card is on screen (30 at least); 0 gives them once. |
| `sources` | The host's data the view gets in `hl.context.data.sources`: `limits`, `machine`, `subagents`. Nothing else of the page reaches it. |
| `smart` | `{prompt, every}`: a request a model answers over the sources in at most three short lines, every `every` minutes (5 to 1440, default 30) and at once on the card's button; the answer is in `hl.context.data.smart` (`{text, at, asking}`). |

A card of this kind can also show data from the person's connected services (mail, a tracker): that is `hl.connections`, which an app's view
has and a card made by `widget create` does not (it has no manifest to name the connection in), so such a widget is an app with a `side`
view. See [`apps/tracker-side`](apps/tracker-side) and `docs/build-apps/14-connections.md`.

### It shows at once, with no reload

Every write of the installed list (a widget made, changed or removed by `harnessly widget` / the MCP tools, an app
installed or removed) is told to the open pages on the machine channel of the socket: the event `machine_apps`
(`{at}`, no list; a burst of writes within 120 ms goes out once). The page reads the machine's apps again
(`stores/apps-changed.ts` → `refreshInstalledApps`), so a new widget joins the stack, a removed one leaves, and a changed
one (`widget_update` of `html`/`css`/`js`) is a new install of the app (its `updatedAt` moves): the card's frame starts
anew on it. The old poll of three minutes stays only as a fallback.

### Widgets of a chat, and a fast open

A widget an agent makes is for the chat it made it in: `harnessly widget create --scope chat|global` (the MCP tool's
`scope`; chat is the default, global for what is the person's everywhere). The chat is the one the agent's session
runs (the CLI sends its tmux session, the MCP tool knows its chat); it is written into the view's widget block
(`views[].widget.chat`, `internal/apps`), so an `update` with `--scope global|chat` moves it. The page shows an app's
widget in its chat and in the chats the person pinned it to; the person's word (`chats` of the item in
`prefs["hx-widgets"]`: `all`, or a list of chats) wins over the agent's, and is changed in the card's menu («Только в
этом чате», «Показывать во всех чатах») and from another chat's «+ Виджет» (it pins the widget there too). The built-in
widgets are global until the person pins one. A widget for a job is removed by the agent when the job is done (say
«Готово» in it, report, `widget remove --side`), or dismissed by the person from the menu.

Opening a chat no longer waits for the machine's list of apps (a read that starts the view server): the last list of the
apps that have a widget is kept in the browser (`features/widgets/cache.ts`, `hx-wg-apps:<machine>`) and the stack is
drawn from it at once; the live list replaces it when it comes. With nothing kept yet the cards are placeholders
(a shimmering bar), not a blank. `features/widgets/apps-cache.test.tsx` measures it: with a machine that answers the list
in 800 ms the stack has its apps after 800 ms cold and at the first render from the kept list.

### A progress widget, offered by the agent

When the person asks to be told when something long ends (a render, a build, a deploy, a download, a migration), or an
agent starts work of more than about five minutes, the agent offers a card under the right column itself: a
`notice_ask` choice «Сделать виджет прогресса справа?» with «Сделать виджет справа» and «Не надо». On the first it makes
the widget (`widget_create`) and keeps it true with `widget_update`; on the second it makes nothing. The text agents
are given is in `cmd/runtime/agent_skills.go` (`progressOfferPromptText`).

What the person answered is the agent's memory, not a store of ours: the note `side-widget-offers.md` in the memory
folder of the project (Claude Code's memory, synced by docs/hub/memory-sync.md), one line per answer,
`date | kind | length | yes / no / asked` (kind: render, build, deploy, download, migration, training, other; length:
short is 5 to 15 minutes, long over 15; the kind `any` is a standing rule). It is plain text the person reads and edits.
The agent decides by it: `asked`, or two `yes` and no `no` for the kind and length → makes the widget without asking;
the last answer `no` → neither offers nor makes it; otherwise offers once.

### Smart widgets

The page asks `POST <platform>/side-widgets/smart {key, prompt, data}` only while the card is unfolded and on screen,
no more often than its interval, and keeps the last answer per widget in the browser, so a reload does not ask again.
The machine answers with the model picked for «Умные виджеты» in Settings → Фоновые задачи (bgmodel); with
none picked, the machine's own Claude Haiku by its subscription. It holds its own floor too: one answer a minute per
widget, sixty an hour for all of them.

### Safety

A widget is a sandboxed app view: an opaque frame and the bridge, no permission, no network. It gets the data its
manifest names and the smart answer, never a secret, another chat or the page; it runs no code in the host's page.
The agent of a chat with apps (inside the wall) may make these widgets and nothing else of the apps: the route is
`/api/cli/apps/side-widgets`, and the package it writes has no permission to ask for.

## The left menu

The rail's top holds the main buttons (new chat, search, Sinopia Design, «Приложения», «Ещё»). An installed app —
Доски, Документы, Артефакты, one an agent made — is carried by hand from under «Приложения» up among the main buttons and
back, right in the rail or in Настройки → Оформление → «Меню слева» (where the pin raises and lowers it too). The layout
is the person's settings (`nav`: `order`, `hidden`, `apps`), the same on every device. There is no command for it: an
agent asked to move an app says where the person does it.

## Checks

- `web/app/next/features/widgets/model.test.ts`, `web/app/next/features/sidebar/nav-layout.test.ts`.
- `internal/sidewidget` (the package, the service, the MCP tools), `internal/bgmodel/widget_test.go`,
  `cmd/runtime/routes_side_widgets_test.go`.
- Agent scenarios: `evals/agent/side-widget-*.yaml`, `evals/agent/widget-offer-*.yaml`, `evals/agent/widget-scope-*.yaml` (docs/agent-evals.md).
- The push: `cmd/runtime/v2_machine_apps_test.go`, `web/app/next/features/app-views/apps-pushed.test.tsx`.
