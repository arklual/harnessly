# Design pages

A project of the design library is a folder `<library>/<id>/` in the design tool's format: boards `project/*.dc.html`,
their engine `project/support.js`, assets. Since the page model, a project also has `meta/`, which is the truth about
its pages:

```
<id>/
  meta/project.json          {"v":1, "title", "source"?, "createdAt"?, "release", "kits":[], "tailwind":false, "canvasKey"}
  meta/pages/<ULID>.json     {"id", "title", "file", "route"?, "widths"?, "variantOf"?, "w", "h", "expand"?, "order"?}
  project/*.dc.html          the boards
  project/canvas.json        built from meta/: the design tool's index (v3), read-only
  artifact.json              built from meta/: the old list of boards (v1), for older tools
```

- One file per page, so that runtimes sharing a library (a folder synced between machines) never write one shared file
  for a page change. A page id is a [ULID](https://github.com/ulid/spec): permanent, made without asking anyone.
- `file` is the board under `project/`: a `*.dc.html`, or a page with its query (`lib/page.html?id=…`).
- `release` is the engine release the project is built on (`legacy` for one made before releases were tracked).
- Pages are listed by `order`, then by id. A board file made by hand that no page names is still listed, without an id,
  until it is registered (`design page create … --file`).
- The runtime rebuilds `project/canvas.json` and `artifact.json` after every page change, atomically and only when the
  bytes differ. Unknown keys already in them are kept (the design tool's `notes`, `designSystems`, a board's own
  settings); the runtime's own data in canvas.json goes under `canvasKey` (`RUNTIME_DESIGN_CANVAS_KEY` when the project
  was made or migrated, default `harnessly`). Boards without a place on the canvas are laid out in a grid.
- A project without `meta/` is read exactly as before (artifact.json, else canvas.json, else the files).

## Commands

Agents change pages through the runtime (which holds the project's lock):

```
harnessly design page list <project>
harnessly design page create <project> --title TITLE [--file NAME.dc.html] [--route /path] [--variant-of PAGE] [--w N --h N] [--fill]
harnessly design page rename <project> <page> [--title TITLE] [--file NAME.dc.html]
harnessly design page remove <project> <page>
```

`<page>` is an id, a file (with or without `.dc.html`) or a title. `create` makes the board file from the project's
`project/_board-template.html` (or the runtime's own template), or from the page named by `--variant-of`; with
`--file` naming a file that is there and no page names, it registers that file as it is. `rename --file` renames the
board file and keeps the id. `remove` moves the board file (unless another page shows it) and the page file to
`<library>/.trash/`. The same operations are `GET /api/cli/design/pages?project=` and `POST /api/cli/design/pages`
`{action: create|rename|remove, project, page?, title?, file?, route?, variantOf?, w?, h?, expand?}`. The panel's own
board edits (rename, move, duplicate, remove, the project title) go through the model too.

## Claims

A claim on a board of a project with `meta/` is keyed by the page id, so a rename keeps it. A claim under the board's
old key (its file name without `.dc.html`, which a runtime without the page model still writes) holds the same page:
this runtime refuses another chat while it lives, and `design release` lets go of both. Answers name the board as
before (`board`) and add the page id (`page`).

## Migration

`harnessly design migrate` gives every project of a library its `meta/`, built from what the library lists today: the
same boards in the same order, with the same titles and sizes. It works on the folder itself, without the runtime.

```
harnessly design migrate --dry-run --library /srv/design     # what it would write; writes nothing
harnessly design migrate --library /srv/design               # writes meta/, project/canvas.json, artifact.json
harnessly design migrate --library /srv/design <project>     # one project
```

It never writes or removes a board or an asset. A page id is derived from the project, the board path and the project's
creation time, so the same library migrated twice or on two machines gets the same ids and the same bytes; a project
that has `meta/` only gets its built files brought up to date; a `meta/` folder without `project.json` is reported and
left alone. Run it on one machine of a shared library and let the sync carry `meta/` to the others.

To prove a migration on a copy first:

```
cp -a /srv/design /tmp/lib-copy
DESIGN_MIGRATION_COPY=/tmp/lib-copy go test ./internal/design -run TestMigrationOfALibraryCopy -v
```

The test checks that the dry run writes nothing, that every file but `meta/`, `artifact.json` and `project/canvas.json`
is byte for byte the same afterwards, that the library lists the same boards, and that a second run writes nothing.

## The canvas.json guard

An agent of the design tool is used to editing `project/canvas.json` by hand; here the edit would be overwritten. The
Claude Code hook refuses it and tells the agent to use `design page`. In the agent's `settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write|Edit|MultiEdit|NotebookEdit|Bash",
        "hooks": [{ "type": "command", "command": "harnessly hook design-guard" }]
      }
    ]
  }
}
```

(or `deploy/hooks/design-canvas-readonly.sh`, which runs the same command). It refuses a write to
`<project>/project/canvas.json` of a folder that has `artifact.json` or `meta/` next to `project/`, and the shell forms
that write it (a redirect, `tee`, `sed -i`, `mv`/`cp`/`rm`, a script that writes or dumps it) when the command names
`project/canvas.json`. Reading is never refused. The shell check is a best effort, not a sandbox.

## New projects

A new project is a copy of `<library>/.template/` when the library has one, else of the template the runtime ships:
`project/_board-template.html` (a minimal board that pulls in `./support.js`), with the board engine this machine has as
`project/support.js`: `RUNTIME_DESIGN_ENGINE_FILE`, else the engine of the release new projects start on. The engine is
not shipped with the runtime; without one a new project is refused (see [the board engine](design-engine.md)). Every
new project gets `meta/` with the release new projects start on (`legacy` without releases); a template's own
`meta/project.json` settings (release, kits, tailwind) are kept, its pages never are.

## Speed

How fast a board loads is measured, not guessed:

```
CHROME_BIN=<chromium> node web/scripts/board-load.mjs <board url> [--runs 3] [--cookie name=value] [--verbose]
```

`<board url>` is a board of the library server, `http://127.0.0.1:<port>/a/<project>/p/<board>`. Each run opens a fresh
headless Chromium (the `@playwright/test` of `web/`; `CHROME_BIN` names a Chromium when the one Playwright expects is not
installed) and loads the board twice in one context: cold (empty cache), then warm (the cache the first load filled).
It prints `cold <ms> warm <ms> requests <n> bytes <n>`: the time until the board's frame, nested iframes included, holds
more than 300 elements; and what the warm load asked the network for (answers from the cache are not counted). After the
runs: the cold load's totals, every address it fetched with a body more than once, and bodies of one size at two
addresses (one bundle downloaded twice).

Measure on a runtime of its own, never on the person's: a standalone runtime (`web/e2e/runtime.ts` shows the
environment) with `RUNTIME_DESIGN_LIBRARY_DIR` on a copy of the project and `RUNTIME_DESIGN_LIBRARY_PORT` on a free port
outside the runtime's own block of ports (it takes nine above its listen port). The library server starts with the first
`GET /api/platform/design/library?lite=1` made with a page session. The wrapper board is measured by its library
address, which skips the preview slot.

Targets: warm load <= 2 s, warm bytes <= 300 KB, no bundle downloaded twice when cold.

Numbers of 25.10.2026, the `acme-admin` copy, the board «Выплаты - Выплата» (a wrapper around the page
`lib/page.html?id=payout-detail`), headless Chromium on a shared machine at load average 8-14 on 12 cores, medians of
10 runs. Before is `main` at f876279, after is this branch:

| | before | after |
|---|---|---|
| cold, ms | 6687 | 6401 |
| warm, ms | 6500 | 5121 (best 4755) |
| warm requests / bytes | 33 / 62 KB | 31 / 64 KB |
| cold requests / bytes | 58 / 3.30 MB | 57 / 2.14 MB |
| `_ds_bundle.js` (1.17 MB) cold | twice, under two addresses | once, `/__h/<hash>/` |
| React, ReactDOM cold | from unpkg.com | from `/__vendor/` |

The bytes and the double download are fixed; the warm time is not: it is still 2.5 times the target. The network is
not what it waits for (the whole warm load is over the wire in about 0.1 s per step): the load goes through three
steps one after another: the wrapper's runtime boots (about 1.2 s before it opens the page's iframe), the page's scripts
start (about 1.1 s later), and `pages.js`, the design system bundle and React render (about 2 s), most of it script
execution (`pages.js` 0.9 s, `_ds_bundle.js` 0.6 s, `react.js` 0.3 s of the profile) and the rest waiting between steps.
What is left to the board's own boot, not to what the library serves.

Public boards had a separate serving path and therefore missed both optimizations: their HTML kept the engine's
React/ReactDOM/Babel URLs on the third-party CDN, and every project asset was `no-store`. The browser consequently
downloaded the same large scripts again on every warm load. Public HTML now applies the same resource map and `tkv`
asset versions as the owner path. The token-protected assets use private immutable caching for a matching version,
private revalidation otherwise, and the same ETag and gzip responder; board HTML and the project index stay
`no-store`.

Numbers of 26.10.2026 for the public route, using an isolated runtime and a copied `acme-admin` fixture, board
«Выплаты - Выплата», medians of 3 runs. Before is `main` at `454db21d`; after is this branch. A local header-injecting
proxy represented the guest gateway without changing response caching.

| | before | after |
|---|---:|---:|
| cold, ms | 6487 | 7248 |
| warm, ms | 5950 | 6252 |
| warm requests / bytes | 36 / 22.45 MB | 18 / 14 KB |
| cold requests / bytes (last run) | 41 / 22.59 MB | 39 / 3.29 MB |
| cold third-party calls | 3 | 1 |

The public-specific fault was the missing transforms and static response path: the warm transfer fell by more than
99.9%, and React/ReactDOM stopped depending on unpkg. It was not the remaining wall-clock bottleneck. On this shared
machine the timings stayed within run-to-run noise and still followed the board's sequential wrapper, engine and page
script execution described above; optimizing that board boot is separate from the public transport fix.
