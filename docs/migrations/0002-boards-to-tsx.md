# 0002-boards-to-tsx: old boards into page modules

A board written in the old format (a template inside `<x-dc>` with `{{ }}`, `<sc-if>`, `<sc-for>`, `<x-import>` and a
logic class) can become a page module: `pages/<name>.tsx`, the form a page is written in since HRN-1012 and the form
that goes into the product's code (`harnessly design code`). `harnessly design migrate --to tsx` does it by rules,
without a model, and takes a board only when its page draws the same picture, pixel for pixel. `--to board` takes it
back, byte for byte. Projects that are not migrated open and work as they are: nothing changes for them.

## Run it

The command works on the library folder itself, like `design migrate`. It needs Chrome on the machine
(`RUNTIME_DESIGN_CHROME` names it when it is not in a usual place).

```sh
harnessly design migrate --to tsx --dry-run --library /srv/design shop     # what would happen, nothing is written
harnessly design migrate --to tsx --library /srv/design shop               # every board of the project
harnessly design migrate --to tsx --board "Orders" --library /srv/design shop   # one board
```

The report has a line for each board:

| Status | What it means |
|---|---|
| `migrated` | the board is now built from its page |
| `would-migrate` | a dry run: it would be |
| `kept` | the rules do not cover something the board does; the reason says what. The board is as it was |
| `differs` | the page was made, but draws N pixels differently; the board is as it was |
| `unstable` | the board itself draws a different picture on each load, so there is nothing to compare with; the board is as it was |
| `already` | the board is already built from a page |

Run the dry run first, then the run. A second run leaves the migrated boards alone.

## What it writes

For the board `<name>.dc.html` of a project:

| File | What it is |
|---|---|
| `project/pages/<name>.tsx` | the page: the template as JSX, the board's logic class as it was written, the values of `renderVals()` read in the component's body |
| `project/pages/<name>.fixtures.ts` | the defaults of the board's props (its `data-props`), the page's data |
| `project/pages/<name>.board.html` | the frame: the board file without its template and logic. It keeps the `<head>` and the `<helmet>`, and has one `<x-import component-from-global-scope="__tkPage.default">` where the page goes |
| `project/pages/board-engine/index.ts` | once per project: what drawing a board takes (the values a template reads, the logic's `setState` and lifecycle, components found on `window`) |
| `project/<name>.dc.html` | the board built from the page: the frame with the page's script in it |
| `meta/tsx-migration/<name>.dc.html` | the board as it was |
| `meta/tsx-migration/<name>.json` | the checksums of what was written, to see a later change |

The board keeps its file name, so pages, links, the canvas and comments that name it stay as they are. After the
migration the board is built: edit `pages/<name>.tsx`, and the library builds the board again (`harnessly design page
build`). The page's name is the board's name, spaces and letters of any script included.

## How a board is proven

For each board the command builds the page and draws both boards in Chrome, from the project's folder, with the
project's own engine (`project/support.js`), at 1280 × 800, with the same clock and the same random numbers, the
animations stopped at their start. The whole page is compared pixel by pixel. A board is written only when not one
pixel differs. Whitespace between elements is tried tidy first (`{" "}`), as a person would write it; when that changes
a pixel, it is kept as the board had it.

## What stays a board

The rules follow the format (`internal/design/texts/format.md`). These stay boards, named in the report:

- `<dc-import>`: a board drawn inside another one;
- `<x-import from="…">`: a file of components loaded at run time;
- `style-hover` and the other styles of a state;
- a `<helmet>` that is not at the top of the template;
- a page whose logic does not build as a module (strict mode), or whose picture differs.

## Roll back

```sh
harnessly design migrate --to board --dry-run --library /srv/design shop
harnessly design migrate --to board --library /srv/design shop
```

Each migrated board gets its file back byte for byte, and its page files are removed (`pages/board-engine/` too, when no
page uses it). A page edited after the migration is not rolled back: the report names the changed files, and `--force`
rolls back anyway, losing the edit. `--board NAME` rolls back one board.

## The engine

A migrated board is drawn by the engine its project has, as before: the frame keeps the `<script src="./support.js">` the
board had, and the page waits for the engine's React before it draws. Updating the engine of Claude Design is unchanged
([design-engine-upstream.md](../design-engine-upstream.md)): fetch the release (`harnessly design engine fetch`),
compare it, and move projects onto it from the panel. After a project moves to another engine, prove its pages again:

```sh
harnessly design migrate --to board --library /srv/design shop
harnessly design migrate --to tsx --library /srv/design shop
```

## Check it on a copy

Before a library is migrated, run the check on a copy of it. A copy made with hard links costs no space: the migration
writes every file through a new file and a rename, so the originals are never changed.

```sh
cp -al /srv/design /data/design-copy
DESIGN_TSX_MIGRATION_COPY=/data/design-copy DESIGN_TSX_MIGRATION_JOBS=2 \
  go test ./internal/design -run TestTSXMigrationOfALibraryCopy -v -timeout 6h
```

It migrates each project, checks that only the migrated boards, their pages and `meta/tsx-migration` changed, rolls back,
and checks that every file is byte for byte as it was. It ends with the count of migrated boards and the reasons for the
rest.
