# Design access and share links

Sinopia projects on a machine have owners, members and team levels, and a guest link can show a project (or some of its
pages) to someone without an account. The hub keeps these records; the machine's runtime enforces them.

## Who may do what

- The person who makes a project owns it. A project not stored yet answers `edit` with no right to manage it.
- A stored project is open to the space at team level `edit` until its owner changes that (`view`, `comment`, `edit`,
  `none`). Editors may share; only the owner and managers change the team level and members.
- Every member of the machine's space appears in the snapshot the hub pushes to the machine
  (`hubproto.OpDesignAccess`); a person missing there gets nothing on a known project. The snapshot is pushed when the
  machine connects, after every change (debounced), when grants run out, and every five minutes.
- The machine reports projects made, claimed and deleted with a `design` event; the hub records them under the person
  the owner file names when that person may open the machine, otherwise under the machine's person.

The page's API stays where it was: `/m/<id><platform api>/design/access/...` answered by the hub, with the same request
and response shapes as before. The hub checks that the person may open the machine first, as for every machine path.

## A library in the hub's store

With the hub's store on (`HUB_STORE`, [store](store.md)) a project is the space's, not one machine's: its rows are
found under whichever machine of the space recorded it, the access dialog of any machine of the space changes the same
rows, and every machine of the space gets the snapshot again after a change. The store gives a machine only the projects
its person has a role in and takes its writes only to those they may edit.

## Guest links

A link is `https://<HUB_DESIGN_SHARE_URL host>/s/<token>`. It shows the whole project or chosen pages (matched by page
path) read-only. Revoking a link makes it answer `410` at once; restarting the hub or the machine changes nothing.

The share host is a separate origin so a guest page never runs next to the hub's session. On it the hub serves only
`/s/...`: no hub API, no machine paths. The hub asks the runtime for a short guest token for the linked project
(`hubproto.OpDesignPublicToken`) and fetches the project's public files with it; the hub never signs those tokens and a
token opens only that project.

A project in the hub's store (docs/hub/store.md) is read from the store instead, and no machine takes part: the hub
puts the revision into a folder of its own (`<data>/design-view`: each blob read from the store once and linked into
every revision that has it) and answers with the runtime's own code for a guest read. A live link shows the project's
newest revision, a pinned one the store revision it was made at (`0000…<rev>` in hex); the owner's machine is asked
only whether the apps allow a link, when it is on. `/s/<token>/version` is the version the link shows, and
`/s/<token>/events` streams `event: rev` each time a live link's project gets a newer revision (the guest page loads
the board again); a pinned link answers `204` there. The machine is still read for a project the store has nothing of
yet, and for a link pinned to a version of git made before the move.
A file of a link pinned to a store revision never changes, so the guest's browser keeps it for good
(`Cache-Control: private, max-age=31536000, immutable`); the guest page's own assets are hashed and kept the same way.

### The team's read and edit through the hub

Under any machine of the space the person may open (it only names the space; it may be off, and so may the
author's), `<platform api>/design/store/<project>/` reads and writes the project in the store with the person's role
from the hub's tables: `head` (`{version, rev}`), `events` (the same stream as a live link), `index.json` and
`f/<path>` (the boards at the newest revision, as a guest link reads them), and `PUT`/`DELETE file/<path>` for a file
of the project's folder (an editor; `.owner.json` only by the owner) with `X-Store-Base`, the file's revision the edit
was made on (`0` for a new file). A stale base answers `409 {"code":"conflict","rev":<the file's revision now>}`. The
write is a revision of the store under the person's e-mail and the machine `hub`, and every machine of the space takes
it as it takes any other's.

### The editor's saves and the deck's PDF

The deck and video editor's saves of a recorded project in the store go to the hub, whether the machine the page runs
over is on or not: `deck` settings, notes, patch, comments and the PDF switch, `video` settings, scene, overlay and
comments (`designview.EditRoutes`). The hub checks the role in its tables, runs the runtime's own handler on a working
copy of the project as the store has it (`<data>/design-view/edit`), and commits what changed on top of the revisions it
read; a file changed meanwhile answers `409 {"code":"conflict"}`. Reading the deck's or video's state stays the
machine's while it is on (it knows the recordings and exports running there) and is the hub's when it is off. A
recording, an export, the model's tidy of notes and a slide from a kit need a live process and stay the machine's.

A deck's PDF for guests is built by any machine of the space whose person may edit the deck, one at a time: the one
holding the store's lease `pdf/<project>` (two minutes, refreshed while it builds). Whether the PDF is behind is the
store's newest revision of what it is made of against the revision written beside it (`.media/published/deck.pdf.rev`),
not the files' times. The published files sync, so the hub serves the PDF to guests with every machine off.

## A machine of a closed cluster

An observe link (`AGENT_HUB_OBSERVE_FILE`) changes nothing of its machine, so by default it refuses the design
operations and opens no tunnel. The machine's operator hands the designs' access to the hub with `AGENT_HUB_DESIGN=on`
in the agent's environment. The link then:

- relays `design.access` and `design.public.token` to the runtime and passes its design events to the hub;
- opens tunnels that end at a filter of their own, which lets through only `GET` and `HEAD` of
  `/m/<id>/api/<name>/design-public/<project>[/<file>]` with no query and no cookie, and hands them to
  `<AGENT_RUNTIME_PLATFORM_API>/design-public/...`; anything else is `403 machine_policy`;
- keeps every other refusal: no runtime session, keys, logins, installs or limits, and the cluster's tunnels go where
  they went.

The snapshot the hub keeps replaces the one in `design-access.json`, so the cluster must stop writing it for that machine
before the switch is turned on.

## Moving the access from a cluster's gateway

A cluster whose gateway kept the designs' access (the platform's: `resources`, `resource_members`, the invites and the guest
links) hands it to the hub in one step, for everyone: `hub design import-gateway`, or the same at the hub's start with
`HUB_DESIGN_IMPORT_GATEWAY=dry-run|apply` as a release step. It reads the gateway's database and:

- copies every project into the rows of the space whose library holds them (`HUB_DESIGN_IMPORT_SPACE`, else
  `HUB_STORE_LEGACY_SPACE`), under its owner's machine of the space (the oldest; else `HUB_DESIGN_IMPORT_DEFAULT_MACHINE`
  or the space's oldest machine): its people, its teams' grants (a gateway team is the hub team named in
  `HUB_DESIGN_IMPORT_TEAMS`, else the `general` team of the space whose slug is the team's or `<team>-team`; a team the
  hub lacks makes the project personal), the requests, the invites, the guest links with their views and comments. A
  link's sealed address is the gateway's: the link keeps opening and cannot be copied again;
- moves each project once: a `gateway_import` line of its log marks it, so a later start changes nothing and what was
  changed in the hub since stays. A project the hub knows keeps its own row and gets only the rows it lacks, and one
  whose owner was handed to the hub before is kept as it is;
- finds the hub machine of every person × machine of the gateway (`runtimes`): the one machine of the space the person
  made whose name is `<prefix><machine>` or starts with `<prefix><machine>-` or `<prefix><machine>.` (the prefix is
  `HUB_DESIGN_IMPORT_MACHINE_PREFIX`, `--machine-prefix`; none by default), or the one named in
  `HUB_DESIGN_IMPORT_MACHINES` (`person@example.com@runner-2=<id>`). A pair with none or with several is reported and
  left with the gateway;
- with `apply` and no project failed, hands the found pairs over: a row of `hub_design_machines` and the pair's
  `hub-access` switch, after which the gateway stops writing the pair's `design-access.json`. A row or a switch that is
  there already, one turned off on purpose included, is left as it is.

A dry run does all of it in transactions it rolls back and writes nothing into the gateway. Both print the report:
each project with what moved, each pair with its machine or why not. The agents of the handed pairs then need
`AGENT_HUB_DESIGN=on`: until they get it they keep the last snapshot the gateway sent.

## Settings

| Variable | Where | Meaning |
| --- | --- | --- |
| `HUB_DESIGN_SHARE_URL` | hub | Origin of the guest links, e.g. `https://share.example.com`. A DNS record and certificate for it pointing at the hub. Empty: links are off |
| `HUB_DESIGN_PLATFORM_API` | hub | Platform prefix of the runtimes, default `/api/platform` |
| `HUB_DESIGN_IMPORT_GATEWAY` | hub | `dry-run` or `apply`: the move from the gateway at start; with `HUB_DESIGN_IMPORT_GATEWAY_URL` (its Postgres), `_SPACE`, `_TEAMS`, `_MACHINES`, `_DEFAULT_MACHINE` |
| `AGENT_RUNTIME_PLATFORM_API` | machine | Where the agent reaches the runtime's platform API (default `<runtime>/api/platform`) |

The hub keeps only a hash of each link's token: a stolen database does not open the links.

## The access dialog

The newer «Share» dialog speaks `/m/<id><platform api>/access/design/<project>` (and `/access/me`,
`/access/directory?q=`); the hub answers it from the same records and rules as the older routes:

- `GET` is the view: the person's role, whether they run the access, the owner, the home team and its level, and for
  those who run the access the grants to people and teams, the open requests and the links.
- `PUT team`, `PUT grants/user/<email>`, `PUT grants/team/<id>` (and `DELETE`), `PUT settings` (`editorsCanShare`,
  owner only), `POST owner` (hand the project over), `GET log`.
- `POST requests` asks for `comment` or `edit`; `POST requests/<id>` allows (with a role and a term) or denies.
- `GET`/`POST links`, `PATCH links/<id>` (`disabled` switches a link off and on), `GET links/<id>/address` (the
  address again), `POST links/<id>/address` (a new address; the old one stops opening at once).

The directory lists the people and teams of the machine's space only, and nothing to someone outside every team. Other
kinds than `design` are not answered, so the page keeps its older dialog for them.

## Notifications

With mail set up (`HUB_SMTP_URL`), a request for access mails the owner and those who may share the project, with a link
to the project's requests; a decision mails the person who asked. Nobody is mailed about their own action.

## Kept addresses

A link's address is kept sealed (AES-256-GCM, a key derived from `HUB_SECRET_KEY` or the `secret_key` file of the data
folder), so it can be copied again from the dialog. A hub whose key changed cannot open the older addresses: those
links keep opening, and a new address is issued from the dialog when one is needed.
