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

### Leases on boards

The same `design-guard` hook takes the chat's lease on every board (`*.dc.html`) the call writes, and the Stop hook lets
go of them when the turn ends:

```json
{
  "hooks": {
    "Stop": [{ "hooks": [{ "type": "command", "command": "harnessly hook design-release" }] }]
  }
}
```

A board is held by one chat at a time. Another chat's edit is refused with «Доску «<name>» правит <who> в чате «<title>»
на <machine>, последняя правка <N> мин назад. Сделай вариант или займись другой доской.» A lease runs out two minutes
after its last sign of life (the runtime renews it while the chat's turn runs) and is let go of at the end of the turn.
Where the leases are kept is `RUNTIME_DESIGN_LEASES` (see [configuration](configuration.md)): `folder` (default) or the
design-sync `center`, which is what the machines of a synced library need. A lease that was taken off (the person pressed
«Снять — это зависло» in another chat, or it ran out) is told to the old chat on its next edit once: it refuses that
edit, asks to read the file again, and the retry goes through. The web page reaches the leases at
`/api/design/library/leases` (see `internal/design/lease_routes.go`).

## New projects

A new project is a copy of `<library>/.template/` when the library has one, else of the template the runtime ships:
`project/_board-template.html` (a minimal board that pulls in `./support.js`), with the board engine this machine has as
`project/support.js`: `RUNTIME_DESIGN_ENGINE_FILE`, else the engine of the release new projects start on. The engine is
not shipped with the runtime; without one a new project is refused (see [the board engine](design-engine.md)). Every
new project gets `meta/` with the release new projects start on (`legacy` without releases); a template's own
`meta/project.json` settings (release, kits, tailwind) are kept, its pages never are.

## Pages written as modules

A page can be written as an ordinary React module and have the library build its board. The module is the source; the
board `<name>.dc.html` is the result, drawn by the same engine as any board. The same `.tsx` file is the one that goes
into the product, so a page is never translated by hand between the mockup and the code.

```
project/
  pages/home.tsx             the page: imports from the kit, default export is the page's component
  pages/home.fixtures.ts     its data (any module next to it; the page imports it)
  components/title.tsx       views shared by pages (any folder of the project)
  home.dc.html               BUILT from pages/home.tsx: do not edit
```

```tsx
import { Root, Button } from "@acme/ui";
import { events } from "./home.fixtures";

export default function Home() {
  return (
    <Root>
      {events.map((e) => <Button key={e.id}>{e.name}</Button>)}
    </Root>
  );
}
```

**What a page may import.** Everything else is a build error that names the import and says what is allowed.

- The project's kit package (`kit.package` in the kit's `design-system.json`, e.g. `@acme/ui`), and any subpath that a
  component's `code.import` names (`@acme/ui/chip`). A name is imported as the component's `code.export`; the build
  maps it to the kit's browser bundle (`window.<Namespace>.<Component id>`), which the board loads from the kit version the
  project is pinned to (`meta/project.json` `kit`). A name the kit does not export fails with the list of those it does.
- `react` (the engine's React). JSX needs no import.
- Relative files of the project: `.ts .tsx .js .jsx .json`, with or without the extension, or a folder's `index`. Not
  the kit's files under `ds/`, not styles or images (the kit's CSS is loaded by the board), nothing outside the project.
- No other package. A page that needs an icon or a chart takes it from the kit.

**Fixtures** are plain modules (`<name>.fixtures.ts`): the page imports its data from there, and in the product the same
import points at the real data source. Keep the data out of the component.

**The board.** `harnessly design page build <project> [page]` builds `pages/<name>.tsx` into `<name>.dc.html` (all the
pages of the project without a name). The runtime also rebuilds a board a moment after any code file of the project
changes. The build is a function of the page's files, the kit version and the builder: the same inputs give the same
bytes, and the result is kept by the files' versions, so a change that touches no input rebuilds nothing. A board made
by hand under the same name is never overwritten: the build says so and stops. The board starts with
`<!-- tk-page: generated from pages/<name>.tsx (in:<hash>) -->`; that line is what marks it as built.

**Read-only board.** A built board is closed to edits in the board itself (an agent's write, the Document API): the
refusal names the source. Change the `.tsx` (or its fixtures) and the board follows. Editing a built board on the canvas
is not carried back into the source yet.

**Check.** `harnessly design page check <project>` says, without writing, whether every module has a board that is
current (exit code 1 when not), and `design check` (the board check) lists the same under `sources`.
`harnessly design check <project> --strict` reads the page, its local components and the hand-written boards against the
kit: only its components, the props they declare and its tokens (see «Strict mode» in
[design-kits.md](agents/design-kits.md#strict-mode)). A project or a kit can turn it on for good (`"strict": true`).

**A page from an old board.** `harnessly design migrate --to tsx <project>` turns boards of the old format
into pages, by rules, and keeps each only when its page draws the board's picture pixel for pixel
([migrations/0002-boards-to-tsx.md](migrations/0002-boards-to-tsx.md)). Such a page has two more files:
`pages/<name>.board.html`, the frame (the old board without its template and logic: its `<head>`, its `<helmet>`, one
`<x-import component-from-global-scope="__tkPage.default">` where the page goes), and `pages/board-engine/index.ts`,
shared by the project's migrated pages. A page with a frame is built into the frame, not into the plain board above,
and draws its whole board, root included. A page's name is the board's: letters of any script, digits, spaces and
`- _ . , · : ( ) « » — –`.

**The page's script waits for React.** The engine loads React after the board's `<head>` is read, so a page's script
runs once `window.React` is there; the engine waits for `__tkPage.default` on `window` by itself.

**Going back.** Delete `pages/<name>.tsx`: the board stays as it is and is an ordinary board again, open to edits (its
first line may stay). A migrated board goes back to its old file, byte for byte, with `harnessly design migrate --to
board <project>`. Boards written in the old format (`from=`, JSX component files) keep working beside modules; nothing
here changes how they load, and the engine is the same one, so rolling the engine back needs no change to a page.

## Into the product's code

A page module is already the product's view; `harnessly design code` puts it, and what it needs, into the
application's repository. It works on the library's folder (no runtime) and is rules over files: the same project, kit
and profile give the same bytes.

```
harnessly design code <project> --to <repository> [--dry-run] [--json] [--library DIR] [--kits DIR]
```

The repository says once where things go, in `sinopia.export.json`:

```json
{ "v": 1, "root": "src/design", "router": "react-router", "stories": true, "tests": "vitest" }
```

`root` is the folder the export writes under; `router` is `react-router` (a route table, `<root>/routes.tsx` unless
`routes` names another file), `next-app` (`app/<route>/page.tsx`, `routes` names the app folder) or `none`.

| File | What | Who owns it |
| --- | --- | --- |
| `<root>/pages/<name>.view.tsx` | the page: kit imports from `code`, presets expanded, the library's variables, the fixtures' names as props (`export type <Name>Data`) | the export |
| `<root>/pages/<name>.fixtures.ts` | the page's fixtures, as they are | the export |
| `<root>/components/…`, other files the pages import | the local components, with the same rules, at the same paths | the export |
| `<root>/pages/<name>.stories.tsx` | a story with the fixtures (`stories: true`) | the export |
| `<root>/pages/<name>.view.test.tsx` | the view renders its fixtures (`tests: "vitest"`) | the export |
| routes | by the page's `route` in `meta/pages` | the export |
| `<root>/pages/<name>.container.tsx` | data, state and handlers: written once with the fixtures, never again | the developer |

What a page must be to export: strict (the export runs the strict check first and stops on a finding), a module whose
default export is `export default function Name()` when it imports data from its fixtures, and fixtures imported by
name. A board in the old format is listed under `not exported`: `harnessly design migrate --to tsx` turns it into a
module first. Business logic, requests and rights are not in a mockup: they are the container's.

**Exporting again.** Every file the export writes starts with a line naming its source and the hash of what was
written (`rev:`). An export rewrites a file only when that hash still matches: a view edited by hand, or a file at an
export path the export did not write, is a **conflict**, and an export with a conflict writes nothing and names the
files. A container that is there is kept. Without a change of the mockup, an export changes nothing.

**The proof** of the five checks is the sample in `internal/design/testdata/code` (a neutral kit and the package it
stands for, a project, an application):

```
go test ./internal/design -run TestDesignCode                    # the strict gate, the files, the second export, conflicts
DESIGN_CODE_PROOF=/tmp/p DESIGN_CODE_NODE_MODULES=$PWD/web/node_modules go test ./internal/design -run TestDesignCodeProof
node web/scripts/design-code-proof.mjs /tmp/p --modules web/node_modules   # tsc on the package, the pixels
```

The first leaves the mockup's board (built from the module, drawn by the built-in engine), the exported application
and its build; the script type-checks the application against the package and compares the two pages at 1280×800
pixel for pixel.

## Component files are translated by the library

A board's component file (`<x-import … from="lib/card.jsx">`: `.jsx`, `.tsx`, `.ts`, or a `.js` with exports) is
translated by the library server, not in the browser. The built-in engine asks for `<file>?tkc=1`; the server answers
with the code translated by esbuild (JSX into `React.createElement`, types dropped, exports as CommonJS), its first
line saying how to run it (`/*tk-compiled:script*/` or `/*tk-compiled:cjs*/`). The translation is kept by the file's
version, so an edit is the next request's new code and nothing else is redone. The page no longer fetches Babel
standalone (3 MB) or runs it on every load.

Nothing else changes for a board. A server that does not know the query (an older runtime, a plain static host), an
engine that does not send it (the vendor's), a file with nothing to translate, and a file that does not compile all end
in the source being sent as before and translated in the browser, where a syntax error is reported the way it always
was. Guest links get the same answer through the public path.

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

### Turning a deck's slides

A deck is a board per slide, and the editor turns a slide by putting the next board in a frame. Two scripts measure it,
both on a runtime of their own (see above) whose library is a copy of the deck's project:

```
node web/scripts/board-load.mjs --deck <slide 1 url> <slide 2 url> [--edit <slide 2 file>] [--runs 7]
node web/scripts/deck-turn.mjs <runtime url> <project id> [--runs 6] [--edit <slide 2 file>] [--settle 3000] [--tunnel]
```

`board-load.mjs --deck` is the library alone: a slide is a page of its own address in one browser context (what a frame
does), seen when its board is drawn as the stage reads it (`#dc-root` filled, fonts and pictures in, two frames on):
`open` (nothing cached), `turn` (slide 2 after slide 1: its own board cold, the kit's files cached), `turn-warm` (slide 2
again), `turn-edit` (slide 2's file changed). `deck-turn.mjs` is the real editor in the runtime's page: the time, in the
page's clock, from the arrow key to the new slide's frame on screen (`data-top` and `data-on`), after the slide on screen
has rested `--settle` ms (the time the neighbours have to load); `--tunnel` throttles to the hub tunnel's ~1.3 MB/s and
40 ms.

Numbers of 09.11.2026, the `kit-deck-tbank-light` copy (18 slides, the Т-Банк kit's 44 MB theme), headless Chrome on a
shared machine at load average 3-4, medians of 6-7 runs, `main` at `8cc88698` (before any change of the turn):

| ms | open | turn (cold) | turn-warm | turn-edit |
|---|---:|---:|---:|---:|
| library alone, `board-load.mjs --deck` | 692 | 506 | 268 | 267 |
| editor, `deck-turn.mjs` | 2225 | 188 | 183 | 180 |
| editor, `deck-turn.mjs --tunnel` | 2816 | 675 | 195 | 197 |
