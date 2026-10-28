# The board engine

The design library (prototypes, sites, decks; named by `RUNTIME_DESIGN_NAME`) keeps every design as plain folders on
your machine. Its boards are written in the board format of Claude Design, and a board draws itself with that format's
engine, which the board loads as `./support.js`.

**The engine is not part of harnessly.** It belongs to the format's vendor and is published as an artifact type on
claude.ai, so harnessly neither ships it in the repository, the binaries, the container images nor serves it from a
hub. Each machine fetches it once with its owner's own Claude account.

## Fetching it

You need Claude Code installed on the machine and logged in with your claude.ai account (`claude` in a terminal of
the machine, then `/login`). Then, in a terminal of the machine:

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
(`~/.harnessly/design-engine`). Run the command again to pick up a newer release: new projects start on the newest
one, existing projects stay on theirs until you upgrade them from the panel.

`harnessly design engine status` shows what the machine has.

## Without the engine

The library still lists and opens the designs it has (each project carries the `support.js` it was made with). Making
a new design is refused with the command above, and the design page says what is missing.

## The rest: Chromium and ffmpeg

Checks, board pictures and PDF/PPTX exports drive a headless Chromium (or Chrome); video exports and recordings use
ffmpeg. Both are optional: designs work without them.

- The container images (`runtime`, `machine`) include both; `docker build --build-arg DESIGN_TOOLS=0` leaves them out.
- The machine installer installs them with profile `dir` and names what is missing otherwise;
  `HARNESSLY_DESIGN_TOOLS=0` skips that.
- Elsewhere install `chromium` (or Google Chrome) and `ffmpeg` with the system's package manager. The runtime finds
  them in the usual places and on `PATH`; `RUNTIME_DESIGN_CHROME` and `RUNTIME_DESIGN_FFMPEG` name them explicitly.

## For a team

A team that wants every machine on the same releases can keep one mirror of releases (a folder with `releases.json`
and the releases' files, laid out as above) and point `RUNTIME_DESIGN_RELEASES_DIR` at a checkout of it on each
machine. Keep such a mirror private: the files are the vendor's, fetched under your account.
