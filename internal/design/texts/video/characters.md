# Video: heroes, things and backdrops

A flat scene of boxes and sliding text looks dead. A scene that explains is about someone doing something: the hero comes in, waves,
points at the chart, is surprised, is glad. Write the scene as the hero's actions, then lay the things and the camera around them.
Everything is drawn in code by the kit (no outside art, no model): heroes with parts and joints, things, backdrops, and a rig that turns
"waves" into tweens on the scene's time.

## Wiring

After `support.js`, in the board's `<head>`. Order matters; take only what the scene uses:

```html
<script src="/__lib/motion/gsap.min.js"></script>
<script src="/__lib/motion/MorphSVGPlugin.min.js"></script>   <!-- the rig morphs mouths: needed with any hero -->
<script src="/__lib/motion/DrawSVGPlugin.min.js"></script>    <!-- tk.drawHand, tk.draw -->
<script src="/__lib/motion/tk.js"></script>
<script src="/__lib/motion/art/art.js"></script>
<script src="/__lib/motion/art/heroes.js"></script>
<script src="/__lib/motion/art/props.js"></script>
<script src="/__lib/motion/art/backdrops.js"></script>
<script src="/__lib/motion/rig.js"></script>
```

A drawing goes on the board as an empty `<svg data-tk-art="name">`; the kit fills it before your motion builds. Size it with CSS (`height: 70vh;
width: calc(70vh * 400 / 600)`), place it with CSS. Colours follow the deck's theme (`--deck-acc`, `-acc-soft`, `-ink`, `-panel`, `-line`, `-bg`),
so one scene reads in any theme.

```html
<div class="world">
  <svg class="bg" data-tk-art="bg-office"></svg>            <!-- position:absolute; inset:0; width:100%; height:100% -->
  <svg class="hero" data-tk-art="hero-person"></svg>
  <svg class="chart" data-tk-art="prop-chart"></svg>
</div>
```

## What there is

| Name | What | Parts you can move (`data-part`) |
| --- | --- | --- |
| `hero-person` | a figure, arms in two parts | the hero parts below |
| `hero-round` | a round mascot with a leaf, small arms | the same, plus `tuft` |
| `hero-robot` | a boxy robot, screen face, antenna | the same, plus `tuft` |
| `prop-laptop` `prop-phone` | a laptop, a phone with an app | `screen` |
| `prop-chart` | a card with bars and a rising line | `bar` (5), `line`, `dot` (5), `arrow` |
| `prop-cloud` `prop-server` | a cloud with an arrow, a server rack | `arrow`, `led` |
| `prop-gear` `prop-rocket` `prop-bulb` | a gear, a rocket with a flame, a light bulb | `gear`, `flame`, `glow` `ray` |
| `prop-doc` `prop-folder` `prop-bubble` `prop-check` | a document, a folder with sheets, a chat bubble, a tick | `row`, `sheet`, `dot`, `ring` `check` |
| `bg-office` `bg-city` `bg-abstract` | a room, a street, soft shapes (1600x900, fills the frame) | layers `far` `mid` `near` (`data-layer`, `data-depth`) |

A hero is 400x600 and stands on y = 560; things have their own sizes (`tk.art.size('prop-chart')`). Hero parts: `root` (jump), `body` (breath),
`head`, `face`, `eyeL` `eyeR`, `pupilL` `pupilR`, `browL` `browR`, `cheekL` `cheekR`, `mouth`, `armL` `armR` (+ `foreL` `foreR`), `legL` `legR`,
`shadow`. `tk.part('.chart', 'bar')` gives the elements of a part; `tk.pivots('.chart')` makes a thing's parts turn about their own points (do it
once for any thing whose parts you scale or turn).

## The hero's actions

```js
tk.sceneLength(9);                       // once: the length of the scene, for what lasts all of it
tkVideo.motion(function () {
  var hero = tk.rig('.hero');            // sets the joints; throws if it is not a hero
  hero.alive();                          // breathing and blinking for the whole scene: every hero, always

  hero.walk('-48vw', '0vw', 1.7, { at: 0.2 });          // from, to, seconds
  hero.wave({ at: 2.1 });                                // {arm: 'L'} for the left one
  hero.talk(1.3, { at: 2.1, text: 'Привет!' });          // the mouth follows the letters; without text, a run of syllables
  hero.point('.chart', { at: 3.7, hold: 1.9 });          // an arm to the target, the eyes follow; release after `hold`
  hero.emotion('surprised', { at: 5.5 });                // 'happy' | 'surprised' | 'sad' | 'neutral'; {hold} returns to neutral
  hero.cheer({ at: 6.5 });                               // happy, a jump, arms up
});
```

Every action takes `at` (seconds from the scene's start) in the last argument. Others: `look(dir | target)` ('left', 'right', 'up', 'down', 'center', a
selector, an element, `{x, y}` on the screen), `blink()`, `nod({times})`, `shrug()`, `jump({height, arms})`, `stand('12vw')` (where it starts).
Actions end at rest, so they chain. Two actions at once on one part (waving and pointing with one arm, two `talk`s) make the check report
`motion-overlap`: use the other arm, or one after another. Talking and waving at once are fine: different parts.

Rules of the hero:
- **A hero is never still.** `hero.alive()` is the secondary action: breath and blinks under everything else. Without it the hero is a picture.
- **One main move at a time.** While the hero points, the eye goes to the chart, not to a waving hand and a jumping mascot.
- **Turn the hero to what it speaks of:** `point` and `look` do it; put the target on the board first, then call the hero (the rig measures
  where the target stands when you call it). Anything that moves a target with `fromTo` (`tk.enter`, `tk.inertia({from})`) comes after.
- **A hero comes from somewhere:** `walk` in from beyond the frame, or `tk.enter` with a drop (`tk.physical.drop`); do not fade it in in place.
- The mouth only moves while it talks; a silent hero with a moving mouth reads as a fault. A voice-less scene shows what it says in type.

## Things, moves and weight

| Call | What it does | Needs |
| --- | --- | --- |
| `tk.morph(a, b, {at, dur})` | one shape into another | MorphSVGPlugin |
| `tk.drawHand(paths, {at, dur, jitter, fps, seed})` | a line drawn by hand, with a small steady-in-time shake | DrawSVGPlugin |
| `tk.squash(el, {at, amount, dur})` | weight: squash, stretch, settle | |
| `tk.inertia(el, {at, dur, from, x, y, scale, overshoot})` | a move that overshoots and settles | |
| `tk.parallax(layers, {at, dur, amount, depths})` | layers drift by depth; `amount` in px, `'6vw'` or drawing units | |
| `tk.camera(world, {...})` `.drift` `.shake` | zoom and pan; a slow wander; a knock that dies away (`xPercent`, `yPercent`, `rotation`) | |
| `tk.particles(box, {at, kind, count, x, y, from, seed, dur})` | `'confetti'`, `'sparks'`, `'dust'` thrown from a point or a target | |

```js
tk.pivots('.chart');
var bars = tk.part('.chart', 'bar');
tk.start(bars, { scaleY: 0 });
tk.tl.to(bars, { scaleY: 1, duration: 0.55, stagger: 0.12, ease: 'back.out(1.6)' }, 4.1);   // bars grow from the floor
tk.drawHand('.chart [data-part=line]', { at: 4.7, dur: 1.3 });                              // the line is drawn
tk.particles('.fx', { at: 6.5, kind: 'confetti', x: '25%', y: '30%', angle: -90, spread: 170, seed: 'left' });  // a cannon from a corner
tk.parallax('.bg [data-layer]', { at: 0, dur: 9, amount: 70 });                              // the room drifts by depth
tk.camera.drift('.world', { at: 0, dur: 9 });                                                // a living camera
tk.camera.shake('.shake', { at: 6.5, dur: 0.35 });                                           // a knock: on its own wrapper
```

- **Particles are thrown from a seed** and a path worked out for each: `seed` is a name, the same name gives the same burst. Give a burst a life that
  ends before the scene does (`at + dur` below the scene's length).
- **One wrapper per kind of camera move:** `tk.camera` zooms (`x`, `y`, `scale`), `drift` and `shake` use `xPercent`, `yPercent`, `rotation`; put
  `shake` on its own wrapper inside `.world` so it does not meet `drift`.
- Weight and follow-through: `tk.squash` on what lands, `tk.inertia` on what arrives, `tk.particles` on what happens. Not on every element:
  the eye needs rest.

## The craft of an explainer

- **Think in beats:** the hero enters (0-2 s), says the thing (2-4 s), shows it (4-7 s), reacts (7-9 s). One beat is one sentence of the script.
- **Depth:** a backdrop with parallax, the hero in front, things between. A 1-2% camera drift keeps a still moment alive.
- **Consistency:** one hero and one backdrop per video, the same theme; the same hero looks the same on every scene.
- **9:16:** the backdrop fills (`slice`), the hero and things sit in the middle 80%: size by `vh` and `vmin`, place by `vw` offsets from the middle.
- **Your own drawing:** `tk.art.define('mine', {w, h, body})` with `data-part` and `data-pivot="x y"` makes a thing the rig and `tk.part` understand;
  colour with the classes the kit uses (`f-acc`, `f-soft`, `s-ink`: fills and strokes of the theme) and keep the style: flat, round, one dark for faces.
