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
`harnessly design engine fetch`. When a release is new, it runs the parity check and writes the report to a folder of
that machine, and prints the report's path. Hook it to the team's tracker with `DESIGN_ENGINE_WATCH_NOTIFY`, a command
that receives the report's path.

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
