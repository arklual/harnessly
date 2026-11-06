# The board engine

The design library (prototypes, sites, decks; named by `RUNTIME_DESIGN_NAME`) keeps every design as plain folders on
your machine. Its boards are written in the board format of Claude Design, and a board draws itself with that format's
engine, which the board loads as `./support.js`.

**Nothing to set up.** harnessly ships its own engine (`internal/design/ownengine/support.js`, Apache 2.0), an
independent implementation of the format. It needs no account, so designs work with an API key, with Codex and offline.

## Which engine a project gets

A new project gets, in order:

1. the file named by `RUNTIME_DESIGN_ENGINE_FILE`;
2. the newest stable release this machine keeps (`RUNTIME_DESIGN_RELEASES_DIR`, by default the engine folder);
3. the built-in engine.

A project keeps the engine it was made with, until you upgrade it from the panel. Settings → the design section and
`harnessly design engine status` show what the machine has.

## Optional: Anthropic's engine

Anthropic's engine belongs to its vendor and is not part of harnessly. New Claude Design features reach it before the
built-in engine. To use it you need Claude Code on the machine, logged in with a claude.ai account (`claude`, then
`/login`); an API key will not do. Then, in a terminal of the machine:

```sh
harnessly design engine fetch
```

What happens:

1. A short non-interactive Claude Code session (model `haiku`, spend capped at $0.50) whose only tool is `Artifact`
   reads the current release of the design type and its file list. A hook (`harnessly design engine hook`) lets the
   tool only read and list.
2. The first time only, the session makes one private, empty artifact of the design type on your account (titled
   "Design engine (do not delete)"): claude.ai serves the type's files on it. Its address is remembered in
   `canvas.json` of the engine folder. Pass `--canvas <url>` to use one you made yourself.
3. The type's files (the engine and the format's rules, without the editor's own bundles) are saved byte for byte and
   checked against the checksums the tool reports, then kept as an immutable release:
   `<engine folder>/upstream/design/<release>/`, listed in `<engine folder>/releases.json`.

The engine folder is `RUNTIME_DESIGN_ENGINE_DIR`, by default `design-engine` in the runtime's data folder
(`~/.harnessly/design-engine`). Run the command again to pick up a newer release.

Run `harnessly design engine status` to see which engine this machine has.

### Official way to install the vendor's engine, and a mirror inside a company

The only supported way is the command above, run by a person who owns the claude.ai account on the machine that will
use it. harnessly does not ship the vendor's engine: it is not in the public source archive, the container image or the
installer, and no script of ours downloads it for you.

A company may serve it to its own machines from an internal mirror, **only if the vendor's licence allows it for that
company**. Check that first; this page is not legal advice. If it does:

1. On one machine with an account that may fetch it, run `harnessly design engine fetch`.
2. Copy the engine folder (`releases.json` and `upstream/design/<release>/`) into a private location that only your
   machines can read (a private repository, an artifact store or a shared disk). Never a public one.
3. On every other machine set `RUNTIME_DESIGN_RELEASES_DIR` to a checkout or mount of that folder.
   `harnessly design engine status` then shows `vendor's, release …`; nothing is fetched there.

Without the permission, leave it out: the built-in engine is complete.

## The rest: Chromium and ffmpeg

Checks, board pictures and PDF/PPTX exports drive a headless Chromium (or Chrome); video exports and recordings use
ffmpeg. Both are optional: designs work without them.

- The container images (`runtime`, `machine`) include both; `docker build --build-arg DESIGN_TOOLS=0` leaves them out.
- The machine installer installs them with profile `dir` and names what is missing otherwise;
  `HARNESSLY_DESIGN_TOOLS=0` skips that.
- Elsewhere install `chromium` (or Google Chrome) and `ffmpeg` with the system's package manager. The runtime finds
  them in the usual places and on `PATH`; `RUNTIME_DESIGN_CHROME` and `RUNTIME_DESIGN_FFMPEG` name them explicitly.
- Their versions are pinned, and `harnessly doctor render` draws a real picture and a 2-second video to prove they work:
  see [the render kit](render-kit.md).

## For a team

A team that wants every machine on the same releases can keep one mirror of releases (a folder with `releases.json`
and the releases' files, laid out as above) and point `RUNTIME_DESIGN_RELEASES_DIR` at a checkout of it on each
machine. Keep such a mirror private: the files are the vendor's, fetched under your account.

## Keeping the built-in engine up to date

The built-in engine does not update by itself; see [moving upstream changes over](design-engine-upstream.md).
