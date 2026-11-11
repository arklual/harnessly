# Access to a resource

One model of access for the things a hub holds: a document, a design, an app. It is off by default.

| Variable | Meaning |
|---|---|
| `HUB_RESOURCE_ACCESS` | `on` puts access to documents on this model. Off: `doc_meta.team_level` decides, as before. |

## The model

A resource is `(kind, id)`: `doc` with the document's slug, `design` with `<machine id>/<project id>`, `app`.

- **Subjects**: a person (a role with an optional end date), a team (a role, and `can_share`: edit and share is manage), a guest
  (a link), and the team level (what everybody of the owner's team has: none, view, comment, edit).
- **Rights**: view, comment, edit; manage and owner on top. An administrator of the installation manages everything.
- **Requests**: a person who has no access asks once; an open request is shown, not doubled; a refusal blocks a new one for 24 hours.
- **Log**: every change is written to `resource_access_log`.
- **Guest links**: the token is shown once (the hub keeps its hash), rights are view or comment, optional expiry and pinned version.

A resource without a row in `resource_access` is open as the kind's own records say (`doc_meta`, `design_*`). The first change by
a manager writes the row and copies those records (`ensure`), so nothing is lost and old records keep being read.

A role given to a person is held by their account (`resource_access_people.user_id`). A role given to an address is an
invitation (a row without `user_id`, shown with `pending`): it gives nothing to an account that merely signs in under the
address, and becomes the role of the person who has proved the address (an identity whose provider vouches for it) the first
time they open the resource. A request is answered to the account that asked.

Every row of the model names the space of its resource (`space_id`, empty for a resource no space is known for), and every
query reads and writes within it: the rows of one space never answer for a resource of another. A guest link's token is the
one key that finds a row without its space, and the link then names it.

## Documents

With the flag on, the document routes ask this model for the person's rank, and these appear under `<platform api>/docs`:

- `/{slug}/acl[/…]`: the access API (view, people, teams, settings, requests, owner, revoke-all, log, guest comments, shares).
- `-guest/{token}` (GET), `-guest/{token}/content`, `-guest/{token}/comments` (POST): a guest, no sign-in. Commenting needs the
  `comment` right; a hidden `website` field catches bots; ten comments per ten minutes per link. A guest's text is kept in the
  thread (`author_kind = guest`) and is never handed to the agent of the conversation.
- `GET /d/{token}` on the hub's root: the address a share hands out (`PUBLIC_URL/d/<token>`). It answers the document itself as a
  page of its own: the link's version when it was pinned, the sandbox CSP of the viewer, `no-store`, `noindex`, and the open is
  counted. An unknown token is a 404 page, a revoked or expired one a 410 page. Commenting goes through the guest API above.
- Mentions: `@[Name](person:email)` in a comment, five people at most, only people the hub knows. Each is mailed once
  (`doc_comment_mentions.notified_at`); the mail names the document and the author, not the text.

Replies in a thread and resolve are part of documents already.

## Storage

Migration `0042_resource_access.sql` adds only tables: `resource_access`, `resource_access_people`, `resource_access_teams`,
`resource_access_requests`, `resource_access_log`, `resource_guest_links`, `resource_guest_comments`, `doc_comment_mentions`.
An older revision of the hub ignores them.

## UI kits

A kit is a resource of kind `kit`, `<space>/<kit id>`, in the same tables (`resource_access*`), so there is no second
model and no migration. A kit belongs to its space: its rows carry `space_id`, and claiming, listing and the checks of an
old kit read only the rows of that space.

- **Maker.** The hub makes the maker of a kit the person of the machine that commits the id first (under the table key);
  later commits from other people are refused. `KitSettings.Owner` is read once, to carry an old kit over.
- **Ceilings.** A team or the company gets *view* at most; a group too. Only a named person gets *edit*; "edit and may
  share" cannot give a group edit. The ceiling is applied when the role is computed and again when a grant is written.
- **Old kits.** A kit that was on the maker's machine before kits were shared (its seal says `legacy`) is open to the
  team for viewing, once. A new kit is private.
- **Store.** The `kits` collection is filtered per machine: a path is seen with *view*, written with *edit* or as maker.
  A machine with several people holds a kit if one of them may view it. Without the hub nothing changes.
- **Dialog.** `GET/PUT /access/kit/<id>/...` answers like a design's. No links, shares or guest comments for kits.
- **Limits.** The view of a machine is cached for 15 s. The runtime does not yet get the kit grants in its access
  snapshot: until it does, "edit" on a machine is held by the store, not by the runtime's own checks.
