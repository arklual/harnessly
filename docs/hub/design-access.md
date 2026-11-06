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

## Guest links

A link is `https://<HUB_DESIGN_SHARE_URL host>/s/<token>`. It shows the whole project or chosen pages (matched by page
path) read-only. Revoking a link makes it answer `410` at once; restarting the hub or the machine changes nothing.

The share host is a separate origin so a guest page never runs next to the hub's session. On it the hub serves only
`/s/...`: no hub API, no machine paths. The hub asks the runtime for a short guest token for the linked project
(`hubproto.OpDesignPublicToken`) and fetches the project's public files with it; the hub never signs those tokens and a
token opens only that project.

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

## Settings

| Variable | Where | Meaning |
| --- | --- | --- |
| `HUB_DESIGN_SHARE_URL` | hub | Origin of the guest links, e.g. `https://share.example.com`. A DNS record and certificate for it pointing at the hub. Empty: links are off |
| `HUB_DESIGN_PLATFORM_API` | hub | Platform prefix of the runtimes, default `/api/platform` |
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
