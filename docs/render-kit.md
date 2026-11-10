# The render kit and `harnessly doctor render`

Pictures of boards, PDF and PPTX exports, checks and video exports need a headless Chrome or Chromium, ffmpeg and
fonts. "The programs are on `PATH`" does not mean they draw: a browser that cannot start its sandbox, a missing font set
or an ffmpeg built without x264 pass that test and fail on the first export. The render kit pins what the built-in engine
is tested with, and the doctor checks that it draws.

A passing render check says nothing about guest links: whether a runtime may serve designs to guests is checked by
`harnessly doctor share` ([design-sharing.md](design-sharing.md)).

## The check

```sh
harnessly doctor render          # steps with results, exit 1 when one fails
harnessly doctor render --json   # the same as JSON (what a monitor or a CI job reads)
```

It uses the same browser pipeline and ffmpeg command line as an export, in a temporary folder, and sends nothing
anywhere:

| Step | What it does | Failure codes |
|---|---|---|
| `tools` | finds Chrome, ffmpeg, ffprobe | `RND-101` no Chrome, `RND-102` no ffmpeg, `RND-103` no ffprobe |
| `frame` | starts the browser, draws a test page, takes 12 frames; the first must be a PNG of the asked size (640×360), with several colours and bright text pixels | `RND-201` the browser did not start, `RND-202` no picture, `RND-203` not a PNG, `RND-204` wrong size, `RND-205` blank or flat |
| `encode` | encodes the frames into a 2-second H.264 MP4 | `RND-301` ffmpeg failed (no libx264, bad build) |
| `probe` | reads the clip back with ffprobe: codec `h264`, `yuv420p`, 640×360, 2 s ± 0.3 | `RND-302` unreadable, `RND-303` codec, `RND-304` duration, `RND-305` size |
| `versions` | compares Chrome and ffmpeg with the pinned kit | `RND-401`, a warning only: it never fails the check |

A failed step prints a hint; the steps after it are marked `skip`. In a container Chromium needs
`RUNTIME_DESIGN_CHROME_ARGS=--no-sandbox` (the image sets it).

### Readiness in the API

`GET` of the design engine status (`ready`) is always `true` by default, as before. With
`RUNTIME_DESIGN_READY_PROBE=1` it follows this check: `ready` is the result of the doctor, and the status gets a
`render` object (`checking`, `ok`, `code`, `step`, `hint`, `at`). The result is cached (15 minutes after a pass,
1 minute after a failure, and at once when Chrome or ffmpeg change on disk); the check runs in the background, never
on a request. Until the first result `ready` is `false` and `render.checking` is `true`.

## The pinned set

`deploy/renderkit/versions.env` is the one file with the versions, as apt version patterns for Debian
`bookworm`: Chromium `156.*`, ffmpeg `7:5.1.*`, fonts-liberation, fonts-noto-color-emoji.

- **The container image** sources the file and installs exactly these (`apt-get install chromium=156.*`); a pattern
  that stops matching fails the build instead of drawing with another Chromium.
- **The machine installer** (`install.sh`, profile `dir`) carries the same values (the hub serves the script alone;
  a test fails when they differ) and pins them on Debian bookworm. On other systems it installs what the package manager
  has and the doctor's `versions` step says how it differs. The full installer is a separate task.
- **CI** installs the pinned set on a bookworm base and runs `harnessly doctor render` (job `render-doctor`).

To move a pin: change the value in `versions.env` and in `install.sh`, raise `RENDER_KIT_REV`, let `render-doctor` pass.
Debian's security updates inside a pattern are taken; a new major is a decision.

Without a container or with `RUNTIME_DESIGN_CHROME`, `RUNTIME_DESIGN_FFMPEG` and `RUNTIME_DESIGN_FFPROBE` you may use
other versions: the doctor decides whether they draw.
