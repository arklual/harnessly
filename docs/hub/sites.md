# Published sites

A space's people can publish a folder, a zip or a Sinopia project as a **site**: static files at an address of their
own, kept by the hub, open whether or not any machine is on. This page is for people who publish and for whoever runs the
hub; the settings are in [deploy](deploy.md#settings).

## The model

```
space
 └─ site  (name, access, single-page fallback)
     └─ deployments  v1, v2, …   immutable: a manifest of path → sha256, files stored once by hash
         └─ «current» → one of them
```

- A **deployment** is a snapshot. Uploading one never changes another; going back is moving «current» to an earlier one
  («Сделать текущей», `harnessly site rollback`), which takes effect at once on the hub that did it and within two seconds
  anywhere else.
- Files are stored **by the hash of their content**: a second deployment uploads only the files that changed, and the same
  file in two sites is kept once. A sweep removes the files nothing refers to (after an hour, so as not to take an upload
  in flight). The last 20 versions of a site are kept; the current one never goes.
- An upload is a protocol, not a body: the client sends the manifest, the hub answers with the hashes it lacks, the client
  uploads those, and **commit** switches «current» in one step. A reader sees the old version or the new one, never a mix.

## Addresses

One DNS label, so that a single wildcard certificate covers every site:

| Address | Opens |
| --- | --- |
| `shop--acme.<sites domain>` | the current deployment of the site `shop` in the space `acme` |
| `shop--v7--acme.<sites domain>` | version 7, whatever is current now (cached for good) |
| `shop--pr-12--acme.<sites domain>` | reserved for previews of branches and pull requests |

A site's name is 1–30 characters of `a–z 0–9 -`, a space's part of the address up to 20. Two dashes in a row are not allowed
in either (they are the separator), and a few names are taken (`www`, `api`, `admin`, `login`, `harnessly`, `sinopia`, …).
A space whose slug is longer than 20 characters or has `--` in it is asked to be renamed before it publishes.

## Where sites open

`hub check-config` says which of these the hub uses.

| Setting | Address | Notes |
| --- | --- | --- |
| `HARNESSLY_SITES_DOMAIN=example-sites.com` | `shop--acme.example-sites.com` | Every site is an origin of its own. Needs a wildcard DNS record and a certificate for `*.example-sites.com`, routed to the hub |
| not set, self-hosted | `<hub>/sites/shop--acme/` | Static files only, and each page is sandboxed (`Content-Security-Policy: sandbox allow-scripts allow-forms allow-popups allow-modals`, no `allow-same-origin`), so its scripts cannot reach the hub's cookies or API. Build with relative paths |
| not set, cloud edition | — | No sites: a stranger's page does not run on the hub's origin |
| `HARNESSLY_SITES=off` | — | The feature is off |

**Use a domain of its own.** Sites hold pages anyone in a space wrote, so the domain must not be one the hub's cookies
reach. Like `HUB_RUNTIME_DOMAIN`, it is checked against the public suffix list: a name on the hub's registrable domain
(`sites.example.com` next to `hub.example.com`) is a warning (a refusal in the cloud edition unless
`HUB_ALLOW_SAME_SITE_RUNTIME=1`), a parent of the hub's host or the hub's own host is refused, and it must not contain or
sit under `HUB_RUNTIME_DOMAIN`. The only cookie the sites set is `__Host-hl_site`.

### Certificates

- **Wildcard** (best): a certificate for `*.example-sites.com` by DNS-01 (cert-manager, or Caddy built with your DNS
  provider's plugin). Nothing more to do.
- **On demand** (no DNS API): Caddy issues a certificate on the first visit to a site, after asking the hub whether the name
  is a site that exists, so nobody can make it order certificates for names of their choosing:

  ```caddyfile
  {
      on_demand_tls {
          ask http://127.0.0.1:8040/api/site-check
      }
  }
  https://*.example-sites.com, https://example-sites.com {
      tls { on_demand }
      reverse_proxy 127.0.0.1:8040
  }
  ```

  `GET /api/site-check?domain=<host>` answers 200 for the bare domain and for an existing site, 404 for anything else, and
  only to the hub's own host and to `HUB_TRUSTED_PROXIES`. The first visit to a new site takes a second or two longer.
- **Development**: `*.localhost` names resolve to your machine in browsers; set `HUB_PUBLIC_URL=http://hub.localhost:8040`
  and `HARNESSLY_SITES_DOMAIN=sites.localhost`, and a site opens at `http://shop--acme.sites.localhost:8040/`.

## Who can open a site

| Access | Who | How |
| --- | --- | --- |
| **Только команда** (default) | people of the site's space | The visitor is sent to the hub, which knows them; if they are in the space it sends them back with a one-minute, single-use **ticket**, and the site trades it for a host-only cookie (`__Host-hl_site`, `SameSite=Lax`, 12 hours). The cookie is checked against the space on every request: leaving the space ends access at once |
| **Все, у кого есть ссылка** | anyone with the address | No sign-in. The address is not secret and not private, so do not put anything private there |

Every response carries `X-Robots-Tag: noindex, nofollow`. The edge drops every cookie but its own and every
header with a configured prefix (`X-Preview-` by default; a gateway in front adds its own) from the request, and answers only `GET` and `HEAD`. Files are served with
`ETag` = the content hash, `Cache-Control: max-age=0, must-revalidate` (`private` for a team site), and
`max-age=31536000, immutable` for versioned addresses and for names a bundler hashed (`app-3f9a1c2b.js`). A site may
have its own `404.html`; a folder's `index.html` and `name.html` answer for `/folder/` and `/name`; a site marked
**single-page** answers every extensionless path that is not a file with `index.html`. The bare sites domain and an
unknown name get a 404 page with a complaint link (`HARNESSLY_SITES_ABUSE_URL`).

## Publishing

**From the browser.** «Сайты» → «Новый сайт»: drop a folder or a zip. A zip is unpacked by the hub, safely: no paths that
leave the site, no links, no `.git`, a size check on what is read rather than on what the archive claims; a folder zipped
from outside (one folder around everything) is taken from inside it.

**From a command line** (a machine, a CI job):

```sh
harnessly site deploy ./dist --name shop --space acme --url https://hub.example.com --token hsite_…
```

With a **deploy token** (site → «Токены») the name and space are known and need not be given. A token can only upload to
its own site; it cannot change access, make tokens or delete. The command hashes the folder, uploads only what the hub
lacks, and prints the address. It does not upload `.git` or `node_modules`. `harnessly site list | versions | rollback <n>`
do what they say.

From a CI job without the CLI, a zip is one request:

```sh
(cd dist && zip -qr - .) | curl -fsS -X POST --data-binary @- \
  -H "Authorization: Bearer $SITE_TOKEN" -H "Content-Type: application/zip" \
  https://hub.example.com/api/sites/acme/shop/upload
```

**From Sinopia.** «Опубликовать» in a project's header builds a snapshot of the project (the pages you pick, as for a guest
link; hidden files and pages that are not picked never leave the machine) and uploads it the same way; the published site
does not depend on the machine afterwards. Publishing again with the same name makes the next version of that site.
A project without an `index.html` gets one that opens its only page, or lists the pages.

The runtime does not hold a credential. It asks the machine agent on the loopback, and the agent asks the hub with the
signature of the machine's own key (`POST /agent/sites`, a signature made for the dial or an unlink is not accepted
here). The hub answers with a **publishing session**: a token naming the machine, its space and the person it publishes for
(only the person who registered the machine, or a member the hub let into it in the last 12 hours; anyone else, known or
not, gives a session of the machine alone, with the same answer), signed with the sites key, kept nowhere and good for 30 minutes. On every use the hub checks that
the machine is not unlinked and still belongs to the space, and that the person is still a member, so unlinking the machine
ends the session at once. The session is its own kind of caller, not a member: it lists, makes and deploys **only the sites
it made** (marked with the origin they were made for, such as a Sinopia project), and may change the access of those. It
cannot delete a site, touch a site of a person, or mint a persistent token. A name that belongs to another origin or person
is answered as taken, and nothing of that site is changed.

The files of a deployment are kept once, but belong to the space that uploaded them: a hash known from another space does
not let an upload be skipped, the bytes must come from this space, and commit checks every file's size against the manifest.
Only the person who made a site, or a space owner, can delete it; this is decided by the person's id, never by the name shown.

### What is refused

- More than **100 MB** a deployment, **32 MB** a file, **5000** files, **50** sites in a space.
- **Secrets.** A file named `.env`, `*.pem`, `*.key`, `id_rsa`, `.npmrc`, … or holding a private key or a well-known token
  (AWS, GitHub, GitLab, Slack, Stripe, Anthropic, Telegram bot) is refused with the list of files and the rule that matched,
  never the secret. If they are not secrets, upload with `--allow-secrets` (`allowSecrets` in the API).
- Paths with `..`, an absolute path, a backslash, a control character, or anything under `.git`.

## HTTP API

Under `/api/sites`, with the hub's session (the browser) or `Authorization: Bearer <deploy token>`. Errors are the hub's
([api](api.md)).

| Request | |
| --- | --- |
| `GET /api/sites-config` | scheme, domain, limits |
| `GET /api/sites-token` | which space and site a deploy token is for |
| `GET /api/sites/{space}` · `POST` `{name, access, spa}` | list · create |
| `GET /api/sites/{space}/{site}` · `PATCH` `{access, spa}` · `DELETE` | the site with its versions · settings · delete (the space's owner or whoever made it) |
| `POST …/deploys` `{files: {path: {hash, size}}, source, allowSecrets}` | begin → `{id, missing: [hash…]}` |
| `PUT …/deploys/{id}/blobs/{hash}` | the bytes of one file (the body is hashed and checked) |
| `POST …/deploys/{id}/commit` `{current}` | → `{version, url, versionUrl}` |
| `POST …/upload` (`application/zip`) | begin, upload and commit in one request |
| `POST …/current` `{version}` | make a version current (rollback) |
| `GET/POST …/tokens`, `DELETE …/tokens/{id}` | deploy tokens; the secret is in the answer to `POST` only |

`Authorization: Bearer hsm_…` (a publishing session) works on the same routes for the space it was given for.

A browser upload of raw bytes is not JSON, so it declares itself with `X-Harnessly-Client`, as a plain HTML form cannot.

## A sites server on its own

The same code runs without the hub's accounts and spaces, next to a platform that already signs people in:

```sh
hub sites check-config      # prints what it understood, never a secret
hub sites                   # serve
```

| Variable | |
| --- | --- |
| `HARNESSLY_SITES_DOMAIN` | required: the sites live at `<site>--<space>.<domain>` |
| `SITES_GATEWAY_SECRET` | required, ≥ 32 characters: shared with the gateway that signs people in |
| `SITES_ADMIN_TOKEN` | ≥ 24 characters: manages every space's sites over the API (the space is whatever the path says) |
| `SITES_LOGIN_URL` | the gateway's entry for a visitor who is not signed in; gets `?host=<site host>&return=<address>` |
| `SITES_STRIP_HEADER_PREFIXES` | comma-separated prefixes of request headers the edge drops, for the ones the gateway uses to carry identity (default `x-preview-`) |
| `SITES_LISTEN`, `SITES_DATA_DIR`, `SITES_DATABASE_URL` | `0.0.0.0:8050`, `/var/lib/harnessly-sites`, SQLite there or `postgres://…` |
| `HARNESSLY_SITES_DIR`, `HARNESSLY_SITES_S3_*`, `HARNESSLY_SITES_ABUSE_URL` | as in the hub |

The gateway authenticates the visitor, decides whether they may open the site, and sends the browser to
`return` with `?hl_site_ticket=<ticket>`. A ticket is `base64url(payload).base64url(hmac)` where the payload is
`{"u": <user id>, "h": <site host>, "e": <unix expiry, at most 60 s ahead>, "n": <random nonce>}` and the HMAC-SHA256 key is
`SITES_GATEWAY_SECRET`, over `"ticket" 0x00 payload`. A ticket opens the host in `h` once. `hub sites ticket <host> [user]`
makes one for a smoke test. The management API answers on any host that is not a site's address.

## Operating

- Files live in `HARNESSLY_SITES_DIR` (default `<HUB_DATA_DIR>/sites`) or the bucket; back them up with the database.
  Losing the database loses the sites' structure; losing a file breaks the versions that name it (a commit checks that every
  file is there).
- The hub's `/healthz` and `/readyz` cover sites too. Logs carry the route, never a query (a ticket travels in one).
- Deleting a site removes its versions at once and its files at the next sweep (hourly).
