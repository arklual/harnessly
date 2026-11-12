# Preview slots on an origin of their own

A machine's preview slot (`/m/<id>/__pv/<slot>-<key>/…`) normally runs on the hub's origin, sandboxed: the app can
script itself, but it has no origin, so no cookies, no storage and no service worker of its own. With
`HUB_PREVIEW_SLOT_DOMAIN` each person's slot `n` (1–6) also gets a host `<handle>-<n>.<HUB_PREVIEW_SLOT_DOMAIN>` (and the
hub's port, when it has one), and the app runs there as itself.

It is the rollout switch **`preview-slot-host`** ([rollout-switches.md](rollout-switches.md)), per person and machine. No
row means off, and off answers 404 on `POST /api/preview/slot-open` and closes every slot host request.

## Setup

- `HUB_PREVIEW_SLOT_DOMAIN`: a wildcard domain pointing at the hub (`*.slots.example.net`) with its certificate.
  **It must be a site of its own**, not the hub's registrable domain: the slot runs unsandboxed, so a host that is
  same-site with the hub could reach the hub's cookies. The hub refuses to start in the cloud edition when it is
  (`HUB_ALLOW_SAME_SITE_RUNTIME` lifts that, as it does for machine hosts), and always when it contains or is contained
  by the hub, runtime or sites domains.
- `HUB_PREVIEW_SLOT_PREFIX` (optional): put in front of the handle, `<prefix><handle>-<n>.<domain>` (`ai-`), so slot
  hosts cannot meet other names under a domain that others share (feature stands, say). 1-20 lower-case letters,
  digits and hyphens, starting with a letter. A host without the prefix is not a slot host.
- The cookie key is derived from the hub's master key; without it the slot hosts stay shut.
- A certificate that is issued per host (no wildcard) needs the hosts written out. The handle is a keyed hash, so nobody can
  work it out; an instance administrator reads the six hosts of one account with
  `GET /api/admin/preview-slot-hosts?email=<address>` (`{email, hosts}`, 403 for anyone else, 404 for an unknown
  address, and the route is not there at all while the domain or the key is not set). Reading a host opens nothing: it
  still needs the switch, a ticket and the cookie.

## Flow

1. The page calls `POST /api/preview/slot-open {machine, path}` signed in; `path` is the slot's address as the runtime
   gave it. The answer is `{url, host}`: an entry address on **the caller's own** slot host. The handle is a keyed hash of
   the account id; it is never read from the request.
2. The browser opens `url` (`/__tk/enter?t=…`). The ticket is single use, lives 60 s and is bound to the host, the machine
   and the slot key. The host sets its cookie (`__Host-hl_slot`: no `Domain`, `HttpOnly`, `Secure`, `SameSite=None;
   Partitioned` for the hub's frame, 12 h) and redirects into the slot.
3. Every later request carries the cookie, which names the person, the host, the machine and the slot. It is checked
   for signature, expiry, that it was made for this very host, that the host's handle is the person's and the slot is the
   host's. The person must still be let into the machine, not disabled, and the switch on.

## What a slot host answers

Only paths under the cookie's own prefix `/m/<id>/__pv/<slot>-<key>/`; `/` goes there; anything else, any `/__tk/…` other
than the entry, dot segments, `//`, backslashes and encoded slashes are refused. Service worker script requests are
refused. A request must come from the slot itself, the same person's other slots or the hub's pages (the frame); a
whole-page navigation may come from anywhere; other cross-site requests (images, fetches) and an `Origin` of another
person's slot or `null` are refused. Only the target's own `__t_*` cookies go upstream.

Answers lose the path mode's sandbox, the target's CORS headers and origin-wide headers (HSTS, `Clear-Site-Data`), and gain
`frame-ancestors` for the hub's origins, `X-Robots-Tag: noindex` and a private cache policy.

## Threat model

| who | what they try | why it fails |
|---|---|---|
| another person | open my slot host | the handle is a keyed hash of my account; their ticket is bound to their own host; their cookie names their host and fails on mine |
| holder of a ticket | use it elsewhere or twice | scope = host + machine + key; redeemed once, shared in the database |
| holder of a cookie | use it on another slot or host | the cookie carries the host and slot; both are checked against the request |
| anyone | forge or extend a cookie | HMAC with a server key; 12 h expiry in the payload |
| removed member | keep using an open slot | membership, disabled state and the switch are read on every request |
| a page on another origin | read or drive my slot | `__Host-` cookie is host-only; the request checks `Origin`/`Sec-Fetch-*`; no CORS headers from the target |
| my app's page | reach the hub or the machine's API | only the slot's prefix is served; the origin is a different site from the hub |
| an attacker with their own slot | hand me their cookie (login CSRF) | exposes only the attacker's own app, on the attacker's origin; entry needs a ticket for that person's own host |

Not included: the carried-session mode for app views, neighbour ports and external-address slots (7–8), and any
instance-specific forwarding; those are deployment extensions.
