# Video: overlays (titles, callouts, highlights)

A scene may carry a layer of **overlays**: short texts and marks drawn over the board (or over a recording of an interface) as a
function of time. They live in the scene's meta, not in the board's HTML, so the person can move, retype and retime them in the
player with the mouse, and you can do the same by command. Never edit `meta/` by hand.

## Layer or board code?

- **Layer** (this guide): a title or a lower third over a recording, a callout with an arrow to a button, a frame around an area, a key
  cap, a counting number, a sticker. Anything the person may want to nudge afterwards. The recording of an interface is a board whose
  content you do not own: overlays are the only way to say something over it.
- **Board code** (`guide motion`): choreography of the scene's own objects, drawn paths, camera moves, anything that is the picture itself.
- Do not draw the same words twice: if a headline is an overlay, it is not also on the board.

## Kinds

| kind | what it is | notes |
|---|---|---|
| `title` | a big line or two | plain ink on the scene by default; `--theme dark\|light\|accent` puts it on a plate |
| `caption` | a lower third: plate with an accent bar | a name, a step, a sentence under the picture |
| `callout` | a note with an arrow to a point | needs `--tx --ty`: where the arrow ends (fractions of the frame) |
| `highlight` | a frame around an area, the rest dimmed | the box is the area; `--text` is a small label |
| `key` | key caps | `--text "Ctrl + K"`: split on `+`, the caps are pressed once they are in |
| `counter` | a number that counts up | `--value 12400 [--from 0] [--decimals 0]`, `--text "{n} ₽ a month"` |
| `badge` | a sticker | "New", "Beta", a price |

## Place, time, look

- `--x --y --w --h`: the box, fractions of the frame (0..1 from the top left); the box stays inside the frame. The text is fitted into
  the box: its **height sets the size** of the text, a longer text shrinks to fit.
- `--at` / `--out`: seconds from the scene's start (`--out end` or omitted: to the scene's end). An overlay is longer than half a second
  when it carries words; hold a title about 2 s plus a second per 12 words.
- `--enter`: `fade`, `rise`, `pop`, `mask` (a wipe), `typewriter`, `words` (one by one), `scramble` (letters settle). The text styles
  play as a fade on a highlight. `--leave`: `none`, `fade`, `fall`, `shrink`.
- `--theme`: `auto` (the deck's colors), `accent`, `dark`, `light`; the font is the deck's. `--anchor left|center|right` aligns the text.
- Vary the ways in: one scene with five typewriters looks like a template. One strong move per scene, the rest quiet (`fade`, `rise`).
- Do not stack more than three overlays at one moment. Give a reader about 0.35 s per word, the first word appears no earlier than
  the thing it names.

- Two overlays must not stand on each other at the same time. `add` and `set` answer with a warning (`overlaps`: the ids) when the
  box crosses another one by more than a fifth while both are on screen; the change is saved, move one or shift its times. A highlight
  is an area and is not counted. The text color of a plate is kept at 4.5:1 or better by the layer itself.

## 9:16

The same list serves both formats. Without anything else, the 9:16 frame moves each overlay into the **safe zone** (6 % at the sides,
8 % at the top, 17 % at the bottom, where the players put their own buttons) and shrinks it to fit. For a vertical layout of your own add
`--portrait x,y,w,h[,tx,ty]` (`--portrait none` removes it). Look at the vertical frame (`frame --format 9:16`) whenever a scene has
overlays and the video has that format.

## Commands

```
{{cli}} design video overlay ls <id> "<scene>"
{{cli}} design video overlay add <id> "<scene>" --kind callout --text "Pay here" --at 1.2s --out 4s --x 0.58 --y 0.16 --w 0.3 --h 0.1 --tx 0.5 --ty 0.45
{{cli}} design video overlay set <id> "<scene>" callout-1 --text "Pay in 2 clicks" --at 1.5s
{{cli}} design video overlay rm <id> "<scene>" callout-1
```

In a video's chat the same exists as tools: `sinopia_overlay_list`, `sinopia_overlay_add`, `sinopia_overlay_set`, `sinopia_overlay_remove`
(times in seconds).

## A person's edit wins

Every overlay remembers who wrote it last: `human` or `agent` (`overlay ls` shows it). When the person moved, retyped or retimed an
overlay, `set` and `rm` refuse it and say so. Leave it as it is; if their change breaks something, tell them. Use `--force` (`force: true`)
only when the person **asked you** for exactly this change to that overlay. An overlay you changed with `--force` is yours again.
Overlays you add are yours until a person touches them. Do not recreate a removed overlay.

## After a change

Look: `{{cli}} design video frame <id> "<scene>" --strip 0:6:0.5s` shows the moments the overlays come in and go. Only the scene whose
overlays changed is rendered again. Text overflow and small text are reported by `design video check` (9:16: at least 28 px).
