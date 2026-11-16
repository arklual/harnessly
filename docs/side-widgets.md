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
- Agent scenarios: `evals/agent/side-widget-*.yaml` (docs/agent-evals.md).
