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
