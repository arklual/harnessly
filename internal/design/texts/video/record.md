# Video: recording a product's interface

For "record a demo": the real interface of a site, shot as a script of actions, with a drawn cursor, a camera that drives to the
clicks and a browser window, all laid on in the scene. Do not rebuild the interface as HTML for this and do not shoot the screen.

## 1. The script and the recording

```yaml
# demo.yaml
name: demo                      # records/demo.mp4 and records/demo.json
url: https://app.example.com/login
viewport: {width: 1920, height: 1080}   # the page's size in CSS pixels (the default)
scale: 2                        # 3840x2160 for a 1080p video: the camera can zoom 2x and the text stays sharp
theme: light                    # light | dark
steps:
  - {do: type, into: "input[name=email]", text: "ann@example.com"}
  - {do: type, into: "input[type=password]", secret: DEMO_PASSWORD}   # the value is never in the script
  - {do: click, on: "text=Sign in"}
  - {do: wait-for, on: "text=Dashboard", timeout: 20000}
  - {do: click, on: "text=New project", pause: 400}
  - {do: type, into: "#name", text: "Spring launch"}
  - {do: key, key: Enter}
  - {do: scroll, by: 700, ms: 900}
  - {do: wait, ms: 1200}
```

Actions: `open url`, `click on`, `hover on`, `type into + text|secret`, `key`, `scroll by|to (+ ms)`, `wait ms`, `wait-for on (+ timeout)`; any step
may add `pause` (ms of stillness after it). A target is a CSS selector or `text=Words` (the smallest visible element that holds them;
`text=/regexp/i` too). Prefer `text=` for buttons and links: it survives a restyle. The cursor moves along an arc to the target at a
person's pace, the page sees every move (hover states work) and typing runs at a person's speed.

```
{{cli}} design video record <project> demo.yaml           # → <project>/records/demo.mp4 + demo.json
{{cli}} secret run --env DEMO_PASSWORD=<name in the safe> -- {{cli}} design video record <project> demo.yaml
```

- The page runs on a clock of its own: one frame is 1/30 s of the page's time, however slow the machine is, so animations are smooth and no
  frame is lost (`dropped` in the answer counts those with no new picture; it should be 0). Cost: about 0.2-0.5 s of work per frame, so a
  one-minute clip takes 6-15 minutes. Shoot short, shoot once, run it in the background (`run_in_background`).
- It needs `chrome-headless-shell` and `ffmpeg`. Without it the command says how to install it.
- The first `open` (or `url`) is not in the clip: the clip starts when the page has loaded. Later `open`s are cut the same way.
- Timers of the page (`setTimeout`, polling) go with the page's clock, so a spinner that waits "3 s" waits 3 s of the clip. A request to a
  server takes real time but only a frame or two of the clip: put `wait-for` before the next click, and a `wait` where a person would look.
- **Secrets.** A password is typed only `secret: ENV_NAME` into a password field; the value comes from the environment of the process
  (start it with `harnessly secret run`), never from the file. The command refuses `text` in a password field and `secret` in any other
  field (it would be on screen). The log keeps `••••••••` and a check stops the run if a value got into it. Do not record a page that shows
  a secret.
- The log (`demo.json`) lists the events in order: `open`, `click`, `hover`, `type`, `key`, `scroll`, each with the time in ms of the clip, the
  cursor's place and the element's box. `durMs` is the clip's length: set the scene's length from it (`{{cli}} design video scene set ... --dur`).

## 2. The scene

```html
<script src="/__lib/motion/screen.js"></script>
<main data-transition="fade"><div id="demo" style="width:100vw;height:100vh"></div></main>
<script>
tkScreen(document.getElementById('demo'), {
  src: 'records/demo.mp4', log: 'records/demo.json',
  at: 0.4,              // scene seconds when the clip starts
  zoom: 1.7,            // how close the camera goes to a click or to typing
});
</script>
```

`tkScreen` draws the window (bar, address, shadow, round corners), the clip frame by frame (the same frame for the same time, in the player
and in the render), the cursor along the logged path with a ring on every click, and the camera. Everything is a function of the scene's
time. It needs no GSAP; with `tk.js` loaded after it, use `tkScreen` (not `tk.screen`).

Edit without shooting again (all in the options):

- **Time:** `speed: 1.5`, `trim: [2, 41]` (clip seconds kept), `cuts: [[8, 14.5]]` (clip seconds cut out: a wait that is not worth showing). The
  scene's length is then `await tkScreen.duration('records/demo.json', {speed, trim, cuts})` seconds.
- **Camera:** automatic by default (it drives to every click and typing, stays, goes back). `zoom`, `lead` (seconds ahead of the click it starts),
  `hold`, `skip: [0, 3]` (clicks, hovers and typing counted from 0 in the log's order) for those it should ignore, `camera: false` for none. By hand: `camera: [{at: 3, x: 960, y: 400, zoom: 1.8, dur: 1}, {at: 7}]`
  (scene seconds, page coordinates from the log's `bbox`; no `x`/`y` is the whole page again).
- **Look:** `frame: 'none'` (the clip fills the box, no window), `chrome: 'dark'`, `url: false` or `url: 'example.com/app'` (the text shown instead of the address; by default the logged address without its port), `pad: 4`, `cursor: false`, `size: 1.2`, `hand: true`,
  `ring: false`, `intro: false`.
- **Captions and labels** go in the scene as its own layer over the window (a block with `data-build-in` or GSAP), not in the recorded page.
  They stay editable and move with the scene's time.

## 3. Quality rules

1. Shoot at `scale: 2` for a 1080p video (`scale: 3` only for a 4K one); the default is right. A page shot at 1x blurs when the camera zooms.
2. Zoom 1.5-1.8. Past 2 the text is only big, not clearer. The camera should come to a click before the cursor does and go back unhurried.
3. Look at the result, do not trust the log: `{{cli}} design video frame <project> <scene> --strip 0:12:1s`, and a frame at the deepest zoom. Small text
   must read, nothing may be cut off by the window, the cursor must not hide what is clicked.
4. A pause of 300-500 ms after a click lets the page answer on screen. A long load is cut with `cuts`, not shown.
5. Do not shoot what the person has not seen on a real account: use a test account and test data, and hide nothing by editing the clip.
6. One recording, many scenes: a scene may use the same clip with another `trim`, `at` and camera.
