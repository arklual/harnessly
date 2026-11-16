# Looks: the person's own theme and backgrounds

A person sets how harnessly looks: the colours, the background of the chats (a gradient, a colour, a picture of their
own, an animated scene), grain, darkening and blur under the text, roundness, density, font and the look of the chat's
parts (bubbles, turns, the field, the sidebar). A look goes on every chat, on one folder of the sidebar or on one chat; a
chat wears its own, else its folder's, else the one for all. With no look the app is exactly as it always was.

An agent changes it from a chat with the same data (`theme_set`, `harnessly theme`), and the page applies it with
«Применено · Отменить».

## A look is data

A look never holds CSS or code. Every field is a number in a range or one of a closed list
(`internal/looks/look.go`, `web/app/next/features/looks/types.ts`, the same limits on both sides); a field that is
neither is dropped, and a look that does not read is no look. So a look from anywhere (an agent, another device, a
preset) can only recolour the page: it cannot run code, load anything from outside or hide a button.

```json
{"v":1, "name":"Night Tokyo", "mode":"dark",
 "hues":[{"h":320,"s":80},{"h":200,"s":70}], "intensity":70, "accent":{"h":320,"s":80},
 "bg":{"kind":"scene", "scene":"rain", "speed":40},
 "grain":10, "dim":20, "blur":0,
 "radius":"round", "density":"cozy", "font":"inter",
 "parts":{"bubble":"glass", "turns":"plain", "composer":"glass", "sidebar":"tinted"}}
```

- `hues` (1–3): the theme's colours, the first is the accent unless `accent` is given; `intensity` says how much they
  tint the surfaces.
- `bg.kind`: `none`, `color`, `gradient` (from the hues), `image` (the sha256 of a picture the page uploaded), `scene`
  (`matrix`, `stars`, `aurora`, `rain`, `synthwave`, `twinkle`, `paper`, `lava`, `snow`, `topo`).
- The tokens are generated from the hues (`features/looks/tokens.ts`): the surfaces, lines and the accent. Text stays the
  base's; every colour text sits on is checked against it (WCAG 2: 4.5:1 for the secondary text and the accent, 3:1 for
  the faint text) and moved in lightness until it passes.

## Where it lives

- The prefs key `hx-looks` (`{v, all, folders, chats}`, at most 40 folders and chats with a look of their own) is
  synced like the other prefs: the hub keeps it for the person, every device reads it.
- Pictures go to `PUT /api/v2/me/looks` (png, jpeg, webp, avif up to 3 MB, sniffed by content) and are served by their
  hash from `GET /api/v2/me/looks/<sha256>` with `nosniff`, a sandbox CSP and an immutable cache. The hub keeps them in
  its object storage under `looks/` (or its data folder), a runtime on its own in `<data>/v2/looks`.
- Presets: `internal/looks/presets.json` (a test keeps the web copy equal).

## On the page

- `LookLayer` puts the look of the place the person is in on `<html>`: the tokens as inline custom properties, the
  parts' variants as `html[data-look-*]` (`styles/looks.css`), a forced light or dark through the theme. A change is
  written only when it differs, in one pass with the transitions off.
- `LookBackdrop` draws the background under the chat. A scene runs at the screen's rate (60 fps and up) in a worker on an `OffscreenCanvas`
  (a main-thread canvas where there is none), drawn at a reduced scale and stretched; it pauses when the page is hidden
  and stands still under `prefers-reduced-motion`.
- The rate is kept, the detail gives: the loop (`features/looks/runner.ts`) judges itself every ~2 s, and when a frame
  takes longer to draw than its budget (6 ms in a worker, 3 ms on the page's thread) or a fifth of the frames come late,
  it lowers the scene's detail one step (1 → 0.7 → 0.5 → 0.35: fewer particles, shorter trails, fewer rings, below 0.5 a
  smaller canvas). A new look starts at full detail; the step it fell to is on the canvas as `data-detail`.
- Over a scene nothing is blended anew each frame: the scene draws its own dim and grain into the canvas, and the glass
  parts (bubbles, turns, the field) drop their backdrop blur for a denser tint (`html[data-look-live]`), since a blur over
  a moving background is computed again on every frame.
- The editor: Настройки → Оформление (for all), «Оформление…» in a chat's menu, the palette button of a folder.

## From a chat

- MCP: `theme_get` (the looks now and the presets), `theme_set` (`preset` | `look` | `reset`, `scope` all|folder|chat,
  `why`).
- CLI: `harnessly theme get|presets`, `harnessly theme preset <id>`, `harnessly theme set '<look JSON>'`,
  `harnessly theme reset`, each with `--scope` and `--why`.

Both go to one core (`looks.Inbox.Submit`): the runtime keeps the request for 10 minutes, the person's open page takes it
(`GET <platform>/look-requests`), applies it, shows «Применено · Отменить» and answers (`POST …/look-requests/<id>/done`).
The tool answers applied, or waiting when no page of the person is open. The folder and chat scopes are the calling
chat's.

Scenarios: `evals/agent/theme-*.yaml`.
