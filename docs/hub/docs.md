# Documents and boards

The hub keeps what people say about a document: the threads, the comments, who may see and edit it, what each person
has read. The documents live in the **artifacts** service and the boards in the **boards** service; the hub talks to
both with one bearer token. Agents publish with `doc.publish` and change access with `doc.acl`; the web reads
`/api/platform/docs`.

It is off by default (`HUB_DOCS=off`). A hub without it is complete: the document screens say "not set up" and name the
missing setting, and an agent's publish gets the same answer instead of a 404.

## Settings

| Variable | Meaning |
| --- | --- |
| `HUB_DOCS` | `on` turns the feature on. |
| `HUB_ARTIFACTS_URL`, `HUB_ARTIFACTS_TOKEN` | The artifacts service and its editor token. Without them documents answer `docs_not_configured`. |
| `HUB_BOARDS_URL`, `HUB_BOARDS_TOKEN` | The boards service. Optional: without them the hub offers documents only. |

`GET /api/platform/docs/_status` tells what is set up and why not, without signing anything.

## Run it

- **docker compose:** `docker compose -f compose.hub.yaml -f compose.docs.yaml up -d --build` adds the artifacts service
  (built from its repository the first time). Set `DOCS_TOKEN` in `.env`. Boards are the `boards` profile, because no
  boards image is published yet: build one, set `BOARDS_IMAGE`, add `--profile boards`.
- **Helm:** `--set docs.enabled=true` runs artifacts in the release (one replica, files on a volume) and makes the
  token. `docs.boards.bundled=true` plus `docs.boards.image.repository` adds boards. For services you run yourself set
  `docs.artifacts.bundled=false`, `docs.artifacts.url` and `docs.existingSecret` (a Secret with the key `token`).

Why it is **off by default** in the chart: it is two more services with a token to keep, and a hub that only runs agents
does not need them. The chart refuses to render when it is on but cannot work (no artifacts service, boards bundled
without an image, a service of yours without its token).

## Rights

The owner of a document (the person whose machine published it) and the hub's administrators can do everything. Others
get the level the owner gave the team: none, view, comment or edit. A document the hub has no record of is viewable by
everyone signed in. Agent visibility maps to levels: `private` none, `team` or `view` view, `comment`, `edit`, and
`public` view with a public link.

## What it does not do yet

- Editing a block in place (`/source/block`) answers 501 `not_supported`; save the whole source instead.
- Threads of a board stay in the hub; they are not mirrored into the boards service.
- Documents are shared by the whole installation, not separated by space.
- A public link to a board is not supported.
- Several replicas of the bundled services: they keep files on one volume. Use S3 storage for artifacts and run it
  yourself (`docs.artifacts.url`).

## The installer

`harnessly install` (branch `agent/742-installer`) sets up a machine, not a hub, so it needs nothing from this
page. When it grows a hub mode, the hub's compose files are `compose.hub.yaml` plus `compose.docs.yaml`.
