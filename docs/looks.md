# Looks: the person's own theme and backgrounds

A person sets how harnessly looks: the colours, the background of the chats (a gradient, a colour, a picture of their
own, an animated scene), grain, darkening and blur under the text, roundness, density, font and the look of the chat's
parts (bubbles, turns, the field, the sidebar). A look goes on every chat, on one folder of the sidebar or on one chat; a
chat wears its own, else its folder's, else the one for all. With no look the app is exactly as it always was.

An agent changes it from a chat with the same data (`theme_set`, `harnessly theme`), and the page applies it with
«Применено · Отменить».

## A look is data

A look holds no CSS and no code itself (the one field that reaches code is `fx`, «Живые эффекты (fx)» below). Every field is a number in a range or one of a closed list
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

## Живые эффекты (fx)

A look may carry an optional live effect that plays over (or behind) the chat: snow that settles on the blocks, a
lamp, a laser. It is the look's `fx`:

```json
{"fx": {"preset": "snowplay", "back": false, "power": 70}}
{"fx": {"script": "<sha256 of an uploaded script>", "back": true, "power": 50}}
```

- Exactly one of `preset` and `script` (`internal/looks/look.go`, `Fx`): `preset` is one of the page's built-in effects
  (`FxPresets`), `script` the lowercase hex sha256 of a script kept like a picture. `back` puts it behind the chat's
  blocks instead of over them; `power` is 0–100 (default: the effect's own).
- Built-in effects (`fx/*.js` next to the runtime, each loaded only when a look wears it, with its pictures as data:
  URLs): `snowplay` (Снежная игра: real-looking snow caps on the blocks, the mouse sweeps them, a double click calls a
  snowplough, Ded Moroz flies by), `nightlamp` (Ночник: a lamp that lights up while the agent works), `laser` (Лазер:
  prints a new answer line by line under a mask of `hx.colors.stage`, a new prompt is outlined), `newyear` (Мастерская:
  a New Year workshop painted under the chat, snow and bows over it), `elves` (Фабрика эльфов: the elves' workshop, each
  prompt on a conveyor belt, elves crafting while the agent works, a bow on the finished answer), `weather` (Погода: the
  sky and the land by the sun's height and the real weather where the person is). `newyear`, `elves` and `weather` always
  run a back layer as well. `harnessly theme fx-presets` lists them. Shared code: `snow-lib.js` (flakes at three depths,
  snow caps drawn once per change), `scene-lib.js` (a picture fitted like cover, twinkling lights, steam, bows).
- «Погода» asks the browser for the place (the person allows it once), rounds it to 0.1° and asks Open-Meteo for the
  weather every 15 minutes (`features/looks/weather-feed.ts`); the frame gets it as `hx.weather` and the `weather` event.
  Without a place it follows the clock and the time zone.
- A script is kept by `PUT /api/v2/me/looks` next to the pictures: bytes that begin with the exact line `//hx-fx v1`
  (then a newline), valid UTF-8, at most 256 KB (`wire.MaxLookFx`); recognised by their content, not by what the request
  says. `GET /api/v2/me/looks/<sha256>` serves them like a picture: `text/plain`, `nosniff`,
  `default-src 'none'; sandbox`, immutable.
- The page runs a script only in a sandboxed frame: an opaque origin (no `allow-same-origin`), no network (a CSP of
  `default-src 'none'`), no way to open windows or reach the page. It sees only what the page tells it: the pointer, the
  blocks' rectangles, the agent's state. Never the chat's text. Under `prefers-reduced-motion` the page does not run it at all.

### An agent writes one

An agent may write the code of a custom effect, but only as such a script against the SDK below, never as CSS or as
anything else. The request carries it next to the look: MCP `theme_set` takes `fx_code` (the JS), the CLI
`harnessly theme set '<look>' --fx-file effect.js`; the runtime's body field is `fxCode` (valid UTF-8, up to
`looks.MaxFxCode` = 128 KB, and the look must be one of the agent's own, not a preset or reset). The person's page uploads
the code, sets `look.fx.script` to its hash and applies the look, so the look in the request may have no `fx` or an `fx`
with only `back` and `power`. The hash and the check of the code are the page's: `Look.Validate` allows an `fx` with no
effect only for a request that comes with code, and `Inbox.Submit` checks the code's size and UTF-8.

### The SDK the page gives a script

The script runs in a transparent frame covering the chat's stage, with `pointer-events: none`. Its global is `hx`:

- `hx.canvas`, `hx.ctx` (2d, already sized to the frame with the device pixel ratio), `hx.w`, `hx.h` (CSS pixels),
  `hx.dpr`.
- `hx.layer`: `"front"` or `"back"`; `hx.power`: 0–1; `hx.mode`: `"light"` or `"dark"`; `hx.reduced`: the person
  prefers reduced motion.
- `hx.colors`: `{accent, fg, bg, stage, hues: [css colors]}`, the look's own tokens; `stage` is the colour the feed is drawn on.
- `hx.weather` (`weather` only): `{source, lat, lon, code, cloud, temp, wind, windDir, rain, snowfall, sunrise, sunset}`.
- `hx.pointer`: `{x, y, vx, vy, down, inside}`, in frame coordinates.
- `hx.blocks`: an array of `{id, kind, x, y, w, h}` in frame coordinates, `kind` one of `composer`, `user` (a prompt),
  `answer`. Rectangles only, of what is on screen (at most 48), measured on scroll and about three times a second.
  `hx.blockAt(x, y)` finds the one under a point; `hx.rand(a, b)` is a random number.
- `hx.agent`: `{state: "idle" | "working" | "waiting", since}`.
- `hx.on(event, fn)` (returns the unsubscribe): `start` (the first size is known); `frame` `(t, dt)`; `resize`;
  `theme` (the mode, colours or power changed); `pointer`; `down`, `up`, `dblclick` `({x, y, block})`; `prompt`
  `({block})` (a new prompt appeared); `answer-start`; `answer` `({block, done})`; `answer-end`; `state` `({state})`;
  `blocks` (the rectangles changed).

The script cannot fetch, open windows, read the page or the chat's text; an error in it is logged once in the page's
console as `[живая тема]`. The runtime is `web/app/next/features/looks/fx/runtime.js`; the built-in effects next to it
(`snowplay.js`, `nightlamp.js`, `laser.js`) are full examples. The host side is `features/looks/fx-layer.tsx`. A
minimal one:

```js
//hx-fx v1
hx.on('frame', (t, dt) => {
  hx.ctx.clearRect(0, 0, hx.w, hx.h)
  hx.ctx.fillStyle = hx.colors.accent
  hx.ctx.globalAlpha = 0.4 * hx.power
  hx.ctx.fillRect(hx.pointer.x - 4, hx.pointer.y - 4, 8, 8)
})
```

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
  `why`, `fx_code`).
- CLI: `harnessly theme get|presets|fx-presets`, `harnessly theme preset <id>`, `harnessly theme set '<look JSON>'`,
  `harnessly theme reset`, each with `--scope` and `--why`; `--fx-file effect.js` on `set` sends the code of a custom live
effect.

Both go to one core (`looks.Inbox.Submit`): the runtime keeps the request for 10 minutes, the person's open page takes it
(`GET <platform>/look-requests`), applies it, shows «Применено · Отменить» and answers (`POST …/look-requests/<id>/done`).
The tool answers applied, or waiting when no page of the person is open. The folder and chat scopes are the calling
chat's.

Scenarios: `evals/agent/theme-*.yaml` (`theme-fx.yaml` for a live effect).
