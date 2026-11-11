# Moving upstream changes into the built-in engine

The built-in engine (`internal/design/ownengine/support.js`) and the format rules (`internal/design/texts/format.md`,
`format.ru.md`) are ours. A new release of Anthropic's design type does not reach them by itself. This is how it does.

## The one rule

Our engine is an independent implementation. Never copy, translate or paraphrase the vendor's code or rule texts into
this repository, and never commit the vendor's files. Learn what changed by running the vendor's engine as a black box
and by looking at what boards do. If a change cannot be understood without reading the vendor's code, stop and raise
it with a person.

## How a release is noticed

`scripts/design-engine-watch.sh` runs once a day on a machine logged in to claude.ai. It runs
`harnessly design engine check`, which reads the name of the current release and needs no canvas, and tells the team about
each release once through `DESIGN_ENGINE_WATCH_NOTIFY`, a command that receives the report's path, the release and the
comparison's exit code. With no comparison the report's path is empty and the exit code is `skipped`.

The comparison needs the release's engine, and downloading it needs a canvas. Name one with `DESIGN_ENGINE_WATCH_CANVAS`
and the watcher fetches the release into a folder of its own, so the machine's new projects stay on their engine, and runs
the parity check. Each run is a short session on a small model of that account.

Where a machine's enforced Claude Code settings hook the `Artifact` tool, a session cannot make a canvas. The settings page
does not offer the fetch command there unless a canvas is already remembered. To fetch on such a machine, make an empty
artifact of the design type in the browser and pass it with `--canvas`.

## What to do with a new release

1. **Compare.** Run the parity check against the corpus, with projects moved onto the new release:

   ```sh
   node scripts/design-engine-parity.mjs <corpus folder>
   ```

2. **Sort each difference:**
   - new behaviour boards can use: add it to the engine;
   - a changed behaviour: follow it unless it breaks existing boards, and then keep both behind what the board asks for;
   - a removed behaviour: keep it, existing projects rely on it;
   - noise (timing, random ids): tighten the check's normalisation, and say why in the commit.
3. **Add a control board** in the corpus for each new behaviour, so the next comparison holds it.
4. **Implement** in `support.js`. Keep element numbering (`data-dc-tpl`) as `internal/design/deckpatch.go` expects: the
   Chrome parity test in `internal/design` holds them together.
5. **Update the rules.** Describe the new behaviour in `format.md` and `format.ru.md` in our own words, with a short
   example, and raise `guideVersion` in `internal/design/guide.go` so libraries get the new rules.
6. **Check that nothing regressed:** the parity check, `go test ./internal/design/...`.
7. **Say it.** Add a line to "What's new" when people can see the change.

## Compatibility promises

- A board that worked keeps working. The engine only grows.
- Projects keep the engine they were made with, so a change here reaches only new projects and those upgraded by hand.
- The format is the vendor's. If a board made on the built-in engine is opened on the vendor's, it should draw the same.
  Anything of our own goes outside the format, not into it.

## Pages migrated from boards

`harnessly design migrate --to tsx` ([migrations/0002-boards-to-tsx.md](migrations/0002-boards-to-tsx.md))
turns old boards into page modules. Its rules come from the format as our engine and `format.md` define it, never from
the vendor's code; whether the project's engine (the vendor's, for most projects) draws a page as it drew its board is
learned the black-box way: the command draws both in Chrome and keeps a page only when not one pixel differs. A new
release can draw a migrated page differently from its old board. After moving a project onto a new release, roll its
pages back and migrate them again (`--to board`, then `--to tsx`): what the release changed shows up as boards that stay
boards, with the number of pixels that differ.

- A migrated board keeps the project's `support.js`: the frame is the old board's own file around the page.
- The page's script waits for the engine's React: an engine that loads React later or from elsewhere still draws it.
- `pages/board-engine/index.ts` does what the format says a board does (values for `{{ }}`, the logic's lifecycle,
  components on `window`). When the format gains a behaviour, add it there and to the rules in
  `internal/design/boardtsx.go` together, with a case in `migratetsx_test.go`.

