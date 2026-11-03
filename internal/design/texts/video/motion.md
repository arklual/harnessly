# Video: real motion

Everything in a scene hangs on one time. GSAP is the main choice for 2D: choreography, type, drawn paths, morphs, camera. The kit is
served by the library, no CDN, and only what a board asks for is loaded.

## Wiring

In the board's `<head>`, after `support.js`:

```html
<script src="/__lib/motion/gsap.min.js"></script>
<script src="/__lib/motion/DrawSVGPlugin.min.js"></script>   <!-- only what this scene uses -->
<script src="/__lib/motion/tk.js"></script>
<script>
tkVideo.motion(function (root) {
  tk.enter('.title', { at: 0.3, from: 'up' });
  tk.draw('.route path', { at: 0.9, dur: 1.4 });
  tk.tl.to('.dot', { x: '40vw', duration: 1.2, ease: 'power2.inOut' }, 1.0);
});
</script>
```

- `tkVideo.motion(fn)` runs fn once, after the board is drawn and the fonts and images are in. Every tween made inside is gathered and
  sought with the scene's time. Build everything in fn, never at the top of a script: the elements are not there yet.
- Do not play, pause or seek yourself, do not use `gsap.ticker`, `ScrollTrigger`, `onUpdate` with memory or `Math.random` at draw time. Random
  values chosen once in fn are fine: the runtime seeds them.
- `/__lib/motion/` has: `gsap.min.js`, `tk.js`, `rig.js` and `art/` (heroes, things, backdrops: `{{cli}} design video guide characters`), `CustomEase`, `CustomBounce`, `CustomWiggle`, `EasePack`, `DrawSVGPlugin`, `MorphSVGPlugin`,
  `MotionPathPlugin`, `SplitText`, `Physics2DPlugin`, `PhysicsPropsPlugin`, `ScrambleTextPlugin`, `TextPlugin`, `Flip` (all `.min.js`).
- Do not mix on one element: `data-build-in`/`data-tk-at` and GSAP both write its opacity and transform.
- Mark the beats for the time line: `data-tk-mark="1.0"` (seconds) on a `data-block` gives the person a moment to pin comments to and does
  not move anything. Give the scene a length: `{{cli}} design video scene set <id> "<scene>" --dur 8s`. `tkVideo.info().motionDur` is where
  your motion ends; the check warns when it runs past the scene.

## Presets

`tk.enter`, `tk.exit`, `tk.accent`, `tk.camera` (`.drift`, `.shake`), `tk.draw`, `tk.text`, `tk.physical.drop`, `tk.physical.hop`, and the living ones: `tk.morph`,
`tk.drawHand`, `tk.squash`, `tk.inertia`, `tk.parallax`, `tk.particles`. A scene with a character is written as the hero's actions with `tk.rig`:
`{{cli}} design video guide characters`. Each takes `at` in seconds, adds
to `tk.tl` and returns it. Read the head of `/__lib/motion/tk.js` for options. Use them for the common moves and write plain tweens on
`tk.tl` for what makes the scene its own: presets alone look like presets.

## Rules that keep a frame honest

- **Never two tweens on one property of one target at one time.** The later one takes its start value on the first pass, so a frame reached
  by seeking differs from the same frame played. Move the camera with one tween at a time, or lay moves on separate wrappers. The check
  reports it as `motion-overlap`.
- **Start values with `fromTo` or `gsap.set` at the start,** moves with `to`. Do not use `from` after a `to` on the same property.
- **`DrawSVG` owns `stroke-dasharray`.** To draw a dashed line, draw a mask over it.
- **Animate `x`, `y`, `scale`, `rotation`, `opacity`.** `top`, `left`, `width`, `height` shift the layout and shimmer.
- **Sizes in `vw`, `vh`, `vmin`, `%` or `calc(var(--u) * n)`,** so that one scene serves 16:9 and 9:16.

## The craft

- **One moving idea at a time.** One main move per key element. The eye follows what moves: decide where it looks at every moment.
- **Timing.** Entrance 400-700 ms, accent 200-300 ms, exit about half of the entrance, stagger 40-80 ms. Overlap actions: the next starts
  before the previous ends. Then let it rest: after text appears, one second for every 2-3 words.
- **Easing has character.** Entrances `expo.out`, `power3.out`; exits `power2.in`; moves from place to place `power2.inOut`; something alive
  `back.out(1.7)`, `elastic.out(1, 0.5)`, `CustomBounce`. Never `linear` except for constant drift and rotation.
- **Principles.** Anticipation before a big move, follow-through and overlap after it, squash and stretch on what has weight, arcs instead of
  straight lines (`MotionPathPlugin`), secondary action on a slower layer, parallax on the depth layers.
- **A camera makes a scene.** Put the scene in one `.world` wrapper and move it: push in on the point, pan along a path. Slow drift of
  1-2% keeps a still frame alive.
- **Type.** Reveal by word or line with `SplitText` and a mask, not whole blocks. A number counts up with `snap`.
- **Safe zones.** Key content in the inner 80%. In 9:16 keep clear of the top 250 px and bottom 400 px of 1920.
- **Do not:** fade everything in the same way, bounce everything, move several things that compete, end on an empty frame, use a move that
  means nothing.
