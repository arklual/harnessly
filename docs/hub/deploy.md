# Running a hub

The hub is the one address your people sign in at. It keeps accounts, spaces and invitations, lets people connect
machines (a laptop, a server, a container) and opens any of them in the browser through itself, so a machine needs no
inbound port. How it is built is in [architecture](architecture.md); this page is for running one.

The hub is one static Go binary (`hub`) with an embedded web page. State is a SQLite file by default, or Postgres.

| Way | For | What you get |
| --- | --- | --- |
| [docker compose](#one-host-with-docker-compose) | trying it, a home server, a team on one box | the hub and one machine next to it («Эта машина»), one command |
| [A server with Caddy](#a-server-with-caddy) | a VPS with a domain | the same compose behind automatic TLS |
| [systemd](#systemd-without-docker) | a host without Docker | the binary as a hardened system service |
| [Kubernetes](#kubernetes-with-postgres) | a cluster | a Helm chart, one replica, Postgres in or out of the cluster |

Whatever the way, only `HUB_PUBLIC_URL` is required. Everything else has a default that works on one machine, and
anything you leave unconfigured is simply not offered: a sign-in way without its settings is not on the sign-in page.

## One host with docker compose

```sh
git clone https://github.com/ly-solutions/harnessly && cd harnessly
docker compose -f compose.hub.yaml up -d --build      # or: make hub-compose-up
open http://hub.localhost:8040
```

Nothing is published as an image yet, so the first start builds two: the **hub** (Dockerfile target `hub`) and a
**machine** (target `machine`: the runtime, the agent, Claude Code and Codex). Both come up and the machine registers
itself with the hub.

1. Open the page and enter your email. With no mail server set, the code is in the hub's log, marked `DEV`:
   `docker compose -f compose.hub.yaml logs hub | grep "DEV email code"`.
2. The first account to sign in is the **administrator** of the installation, provided it has proved its email (the
   code from the mail, or GitHub's verified address): a provider that only claims an address does not count. Make a space.
3. In the space, «Эта машина» connects the machine from the compose file: only the administrator can, because that
   machine runs on the same host as the hub. Everyone else connects their own machines with a command or over SSH.

The hub's port is bound to `127.0.0.1`. The machine is a **separate container on a network of its own** with the hub
(`172.29.40.0/24`; `HUB_NET` changes the first three bytes when the host already uses it): it does not share the hub's
network namespace, so the programs in it cannot reach the hub's loopback, nor pose as the reverse proxy in front of the
hub. The address the hub announces to machines, `HUB_PUBLIC_URL`, has to work for the browser and for the machine alike.
The try-out name is `hub.localhost`: browsers send `*.localhost` to the host's loopback, where the port is published,
and the machine's container is told (`extra_hosts`) that the name is the hub's address on their network. The port inside
the container is `HUB_PORT` too, for the same reason. Moving the port means setting `HUB_PORT` (and `HUB_PUBLIC_URL` if
you set it) in `.env`, copied from [`.env.hub.example`](../../.env.hub.example).

The hub believes `X-Forwarded-For` from one address only: the gateway of that network (`HUB_TRUSTED_PROXIES`, exactly
`<HUB_NET>.1/32`), which is where a connection to the published port, the reverse proxy on the host, comes from. The
machine is another address and is not believed.

The machine finds its one-time registration code in a file (`HUB_LOCAL_CODE_FILE`) the hub keeps on a volume the two
share, and the hub writes a new one whenever it is used or about to run out. The file is private: mode `0600` and its
folder `0700`, or, when `HUB_LOCAL_CODE_GROUP` names the machine's group (compose sets `10001`, the `agent` of the
machine image), `0640` and `0750` for that one group. Nobody else on the host can read it. The machine's home (logins,
workspaces, its key) is a volume of its own; `docker compose -f compose.hub.yaml down` keeps the volumes, `down -v`
deletes them. (The compose file was written without a Docker daemon at hand: if the machine does not appear, check
`docker compose -f compose.hub.yaml logs machine` for the code file and the name `hub.localhost`.)

## A server with Caddy

One VPS, one domain, [Caddy](https://caddyserver.com) for TLS.

**1. DNS.** An `A`/`AAAA` record for the hub's name, e.g. `hub.example.com`. For [machines on their own
addresses](#where-machines-open-path-or-subdomain) also a wildcard, `*.m.example.com`.

**2. Firewall: 22, 80 and 443 only.** compose binds the hub to `127.0.0.1`, so nothing else is reachable from outside
(mind that Docker publishes a port that is *not* bound to `127.0.0.1` straight through `ufw`).

**3. `.env`** next to `compose.hub.yaml`:

```sh
HUB_PUBLIC_URL=https://hub.example.com
HUB_ADMIN_EMAILS=you@example.com
HUB_SMTP_URL=smtps://no-reply%40example.com:app-password@smtp.example.com:465
HUB_MAIL_FROM=harnessly <no-reply@example.com>
# HUB_SIGNUP=invite             # only administrators and invited people may create an account
# HUB_RUNTIME_DOMAIN=run.example.net   # see "Where machines open": a domain of its own
```

(An `@` in the SMTP user name is written `%40`.) Run `docker compose -f compose.hub.yaml up -d --build`.

**4. Caddy**, in `/etc/caddy/Caddyfile`, then `systemctl reload caddy`:

```caddyfile
hub.example.com {
	reverse_proxy 127.0.0.1:8040
}
```

`reverse_proxy` carries WebSockets (agent links, terminals) and event streams as they are and sends
`X-Forwarded-For` and `X-Forwarded-Proto`. The hub trusts that header from the addresses in `HUB_TRUSTED_PROXIES`
(compose.hub.yaml: exactly the gateway of the hub's network, which is where Caddy on the host comes from): that is where
it takes the client address for rate limits and the sign-in log. If your proxy is elsewhere, list its address, and no
wider: whatever address is trusted can pick the address every limit counts it by.

The local machine of the compose file registers with `https://hub.example.com` like any other machine, from inside the
host, so the server has to reach its own public address; most do. If yours cannot, remove the `machine` service and
connect machines with a command, or leave `HUB_PUBLIC_URL` as `http://hub.localhost:8040` for a try-out.

**With a wildcard for machines** the certificate for `*.m.example.com` has to be obtained with a DNS challenge, which
needs a Caddy build with your DNS provider's module (see Caddy's documentation for `tls { dns … }`):

```caddyfile
hub.example.com {
	reverse_proxy 127.0.0.1:8040
}

*.m.example.com {
	tls {
		dns <provider> <credentials>
	}
	reverse_proxy 127.0.0.1:8040
}
```

Both names go to the same port: the hub tells them apart by the `Host` header.

## systemd without Docker

For a host without Docker, [`deploy/systemd/harnessly-hub.service`](../../deploy/systemd/harnessly-hub.service) runs
the binary as its own unprivileged account with a locked-down sandbox (read-only system, private `/tmp`, no home, no
capabilities, only the network and its data folder). The steps are in the unit's header:

```sh
make hub hub-dist                                   # bin/hub with the page; bin/hub-dist with what machines download
useradd --system --no-create-home --home-dir /var/lib/harnessly-hub --shell /usr/sbin/nologin harnessly-hub
install -Dm755 bin/hub /usr/local/bin/harnessly-hub
install -d /opt/harnessly && cp -r bin/hub-dist /opt/harnessly/dist
install -Dm644 deploy/systemd/harnessly-hub.service /etc/systemd/system/harnessly-hub.service
install -Dm600 deploy/systemd/hub.env.example /etc/harnessly/hub.env     # edit HUB_PUBLIC_URL at least
systemctl daemon-reload && systemctl enable --now harnessly-hub
```

The unit runs `harnessly-hub check-config` before every start: a bad setting stops it with the reason in
`journalctl -u harnessly-hub`. Put Caddy (or nginx) in front, as above.

## Kubernetes with Postgres

[`deploy/helm/harnessly-hub`](../../deploy/helm/harnessly-hub/README.md) runs the hub image as one replica with
`strategy: Recreate`: a second hub on one SQLite file would corrupt it, and the agents' links and open streams live in
the process, so there is no rolling update and no second replica. Upgrading replaces the pod; machines reconnect within
seconds.

```sh
kubectl create secret generic harnessly-hub \
  --from-literal=HUB_DATABASE_URL='postgres://harnessly:…@db.internal:5432/harnessly?sslmode=require' \
  --from-literal=HUB_SMTP_URL='smtp://user:password@mail.example.com:587'

helm upgrade --install harnessly-hub deploy/helm/harnessly-hub \
  --set publicUrl=https://hub.example.com --set existingSecret=harnessly-hub \
  --set 'adminEmails[0]=you@example.com' --set runtimeDomain=run.example.net \
  --set trustedProxies=10.42.0.0/16 \
  --set ingress.enabled=true --set ingress.className=nginx \
  --set 'ingress.tls[0].secretName=hub-tls' --set 'ingress.tls[0].hosts[0]=hub.example.com'
```

The chart's defaults are the careful ones, and some differ from the hub's own:

- `signup: invite`, not `open`: only the administrators and people they invite can create an account. Set `open` to let
  anyone.
- `adminEmails` is **required while the ingress is on**: the chart refuses to render without it, because with no named
  administrator the first account to arrive, from anywhere on the internet, would be the administrator. An address
  counts only when its owner has proved it.
- `edition: cloud` refuses to render without `runtimeDomain`.
- `trustedProxies` is **empty**: nobody's `X-Forwarded-For` is believed, so everyone shares the ingress controller's
  address and the sign-in limits that go with it. That is safe and clumsy: set it to the controller's pod range (not to
  the private ranges: every pod in the cluster would be believed and could pick its own address). The controller sees the
  person's address only with `externalTrafficPolicy: Local` on its Service (or a load balancer that speaks the PROXY
  protocol); with the default `Cluster` policy the address in `X-Forwarded-For` is a node's, whatever is trusted.
- `sshAllowPrivate: false` ([the SSH way](#the-ssh-way) refuses private addresses).

Keep secrets (`HUB_DATABASE_URL`, `HUB_SMTP_URL`, client secrets) in the Secret, not in values; `existingSecret` is the
way to use External Secrets or Sealed Secrets. `database.postgres.enabled=true` instead runs a Postgres of the chart's own
in the cluster: fine for a try-out or a small team, not a substitute for a database you back up. With Postgres the data
volume holds only the key file that seals the spaces' SSH keys (for [the SSH way](#the-ssh-way)): put `HUB_SECRET_KEY`
in the Secret instead (`openssl rand -base64 32`; the chart does not make one, because a key made while rendering would
change on every Argo CD sync) and, or with `ssh=false`, the volume can be turned off.

The Ingress sends the hub's host and, with `runtimeDomain`, `*.<runtimeDomain>` to the hub, and raises the proxy's
timeouts for the long-lived WebSockets (nginx annotations by default; other controllers have their own).

## Settings

The hub reads environment variables. `hub check-config` prints what it understood and what it switched on, and never a
secret; it exits with 2 and every problem it found when the settings are wrong. All of these are in
[`.env.hub.example`](../../.env.hub.example).

| Variable | Default | Meaning |
| --- | --- | --- |
| `HUB_PUBLIC_URL` | — (required) | The address people use, `http(s)://host[:port]`, no path. What providers call back, what machines dial, what cookies are bound to. `https` makes cookies `Secure` and `__Host-` prefixed |
| `HUB_LISTEN` | `0.0.0.0:8040` | Where the hub listens |
| `HUB_DATA_DIR` | `/var/lib/harnessly-hub` | The SQLite file and, unless `HUB_SECRET_KEY` is set, `secret_key`: what the spaces' SSH keys are sealed with |
| `HUB_DATABASE_URL` | SQLite in the data folder | `sqlite:///path` or `postgres://user:pass@host/db` |
| `HUB_SIGNUP` | `open` (the Helm chart and `compose.hub.yaml`: `invite`) | `open`, or `invite`: administrators and people with an invitation |
| `HUB_ADMIN_EMAILS` | first account | Comma-separated administrators of the installation. Each has to sign in with an address they have [proved](#who-is-the-administrator); without the list the first account, if it has proved its address, is the administrator |
| `HUB_SMTP_URL`, `HUB_MAIL_FROM` | log the code (plain http only) | `smtp://[user:pass@]host[:587]` (STARTTLS) or `smtps://…:465`. **Required on an https hub and in the cloud edition**: the hub does not start without it (and `check-config` exits non-zero) unless `HUB_MAIL_LOG=allow` |
| `HUB_MAIL_LOG` | not set | `allow`: accept that, with no SMTP server, sign-in codes and invitation links are written to the hub's log although the hub is on https or in the cloud edition. Only for a hub whose log is as private as its database |
| `HUB_PASSKEY`, `HUB_RP_ID`, `HUB_RP_NAME` | `on`, host of the public URL, `harnessly` | Passkeys; need a domain name, not an IP address |
| `HUB_GITHUB_CLIENT_ID`, `_SECRET`, `HUB_GITHUB_URL` | off | GitHub sign-in; the URL is for GitHub Enterprise |
| `HUB_YANDEX_CLIENT_ID`, `_SECRET` | off | Yandex ID sign-in |
| `HUB_OIDC_PROVIDERS`, `HUB_OIDC_<ID>_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET`, `_NAME`, `_SCOPES` | off | Any number of OpenID Connect providers |
| `HUB_RUNTIME_DOMAIN` | path mode | [Machines at `<id>.<domain>`](#where-machines-open-path-or-subdomain): a domain of its own (not on the hub's registrable domain). Required by `HUB_EDITION=cloud` (the hub does not start without it); without it the hub logs a warning at start and `check-config` says so |
| `HUB_ALLOW_SAME_SITE_RUNTIME` | `0` | `1`: let the cloud edition start with a `HUB_RUNTIME_DOMAIN` on the hub's own registrable domain (`m.example.com` for `hub.example.com`), which is one site, not two. The hub warns about it at every start. Elsewhere it is a warning with or without this |
| `HUB_SSH` | `on` | [The SSH way](#the-ssh-way) of connecting a server |
| `HUB_SSH_ALLOW_PRIVATE` | `off` | Let the SSH way reach private, loopback and link-local addresses. Off in every edition by default; the cloud edition ignores it |
| `HUB_SECRET_KEY` | a key file in the data folder | At least 32 characters (`openssl rand -base64 32`): what the spaces' SSH keys are sealed with. `_FILE` is read as a file name. Losing it makes the stored keys useless (they are made again) |
| `HUB_CONTAINER_IMAGE` | hidden | The image the «В контейнере» way runs; must be pullable by the machines |
| `HUB_LOCAL_CODE_FILE` | off | Keep a registration code for the machine next to the hub (compose.hub.yaml); the file is `0600`, in a `0700` folder |
| `HUB_LOCAL_CODE_GROUP` | none | A numeric group id that may read that file instead (`0640`, folder `0750`): the machine's container group |
| `HUB_DIST_DIR` | `/opt/harnessly/dist` | What machines download: [`install.sh`, `runtime-<os>-<arch>`, `agent-<os>-<arch>`](#what-machines-download) |
| `HUB_MIN_AGENT_VERSION` | any | Refuse agents older than this version |
| `HUB_TRUSTED_PROXIES` | `127.0.0.1/32,::1/128` (compose: the gateway of the hub's network; Helm: none) | Addresses believed about `X-Forwarded-For`, as exact as the proxy's own: a wide range lets whatever sits in it pick the address every limit counts it by |
| `HUB_EDITION` | `self-hosted` | `cloud` is the hosted service: no OIDC, no local machine, no SSH to private addresses, and `HUB_RUNTIME_DOMAIN` is required |
| `HUB_TERMS_URL` | none | A link shown at sign-in, when set |
| `HUB_WEB_DIR` | built in | A built `dist-hub` folder to serve instead of the embedded page (development) |
| `HUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

`hub healthcheck` asks the hub on this host for `/healthz` (the container's health check). `/healthz` says the process
is up; `/readyz` also asks the database.

## Sign-in ways

Email code is always available. Every other way is **off until its settings are complete**, and a way that is off is not
shown. The sign-in page asks `GET /api/config` which are on. One person can have any number of them; the last one cannot
be removed.

### Who is the administrator

The administrators are the addresses in `HUB_ADMIN_EMAILS`, or, with none listed, the first account. Either way the
address has to be **proved** by that person: a mailed code, GitHub's primary verified address,
an OpenID Connect provider that says `email_verified: true`. A provider that only reports an address (Yandex ID, an OIDC
provider without `email_verified`) never makes anyone the administrator, never lets them in on `HUB_SIGNUP=invite`
through an invitation addressed to an email, and never joins an existing account: someone must not become the
administrator by registering the administrator's address somewhere that does not check it. An invitation by link works
for any signed-in way; one addressed to an email works once the address is proved. A first account that has not proved its
address is refused (`first_account_unverified`) when no administrator is named. With administrators named and
`HUB_SIGNUP=invite`, nobody else can create an account before one of them has signed in: the empty user table is a door
only when no administrator is named. Name the administrators on any server others can reach.

### Email code

With `HUB_SMTP_URL` the hub mails a six-digit code (valid for ten minutes, five tries, counted atomically and also limited per address across codes) and invitations. Without it
the hub writes the code to its log at warn level, marked `DEV`, so a single-host installation on plain http needs no mail
server. A hub on **https**, and every hub of the **cloud** edition, does not start that way: the log of a hub like that is
kept and read by people other than the one asking for a code (a log shipper, a shared index), and a code or an invitation
link in it is a sign-in as that person. `HUB_MAIL_LOG=allow` overrides the refusal for a hub whose log is as private as its
database; the hub then says so at every start.

```
level=WARN msg="DEV email code (no HUB_SMTP_URL is set, so the code is not mailed)" to=you@example.com code=123456
```

That line (and the one for an invitation link) is the only place the hub logs a code, and it exists only in this mode.
Anyone who can read the log can sign in as anyone: set SMTP before the hub is reachable by people you do not trust, and
never take a code from a shared log. `smtp://` uses STARTTLS (usually port 587),
`smtps://` implicit TLS (465); URL-encode special characters in the user and password.

### Passkeys

On by default, no registration needed. A passkey is bound to `HUB_RP_ID`, by default the host of `HUB_PUBLIC_URL`, so they
work only on a domain name (a hub reached by an IP address logs "passkeys are off" and hides them) and **changing the
domain loses them**: sign in by email and add a new one. Set `HUB_RP_ID` to a parent domain (`example.com` for
`hub.example.com`) to share passkeys between hosts under it. `HUB_PASSKEY=off` hides them.

### GitHub

1. GitHub → *Settings* → *Developer settings* → *OAuth Apps* → *New OAuth App* (for an organisation, the same under the
   organisation's settings).
2. *Homepage URL*: `HUB_PUBLIC_URL`. *Authorization callback URL*: `<HUB_PUBLIC_URL>/api/auth/oauth/github/callback`.
3. Generate a client secret. Set `HUB_GITHUB_CLIENT_ID` and `HUB_GITHUB_CLIENT_SECRET`.

The hub asks for `read:user` and `user:email` and uses the primary **verified** email: if an account with that email
exists, GitHub joins it; otherwise one is created. For GitHub Enterprise Server set `HUB_GITHUB_URL=https://github.example.com`.

### Yandex ID

1. [oauth.yandex.ru](https://oauth.yandex.ru) → *Создать приложение* → platform *Веб-сервисы*.
2. *Callback URI*: `<HUB_PUBLIC_URL>/api/auth/oauth/yandex/callback`.
3. Permissions: *Доступ к адресу электронной почты* (`login:email`) and *Доступ к имени пользователя, фамилии и полу*
   (`login:info`).
4. Set `HUB_YANDEX_CLIENT_ID` and `HUB_YANDEX_CLIENT_SECRET` (the app's *ID* and *Пароль*).

Yandex does not say whether an email is verified, so Yandex **never joins an existing account by email**: when the email
belongs to someone, the sign-in is refused with "account exists" and that person signs in by email code and links Yandex in
their settings. A new email creates a new account.

### OpenID Connect (Keycloak, Authentik, GitLab, Google, …)

List an id (lower-case letters and digits) in `HUB_OIDC_PROVIDERS` and give it four settings:

```sh
HUB_OIDC_PROVIDERS=keycloak
HUB_OIDC_KEYCLOAK_ISSUER=https://sso.example.com/realms/team
HUB_OIDC_KEYCLOAK_CLIENT_ID=harnessly
HUB_OIDC_KEYCLOAK_CLIENT_SECRET=…
HUB_OIDC_KEYCLOAK_NAME=Corporate sign-in         # the button; default is the id
# HUB_OIDC_KEYCLOAK_SCOPES=openid email profile  # the default
```

At the provider create a *confidential* client with the **authorization code** flow and the redirect URI
`<HUB_PUBLIC_URL>/api/auth/oauth/keycloak/callback` (the id is part of it). The hub uses discovery
(`<issuer>/.well-known/openid-configuration`), PKCE, `state` and `nonce`. An account joins an existing one by email only
when the provider says `email_verified: true`; some providers need that switched on (Keycloak: *Email verified* on the
user, or a verification step). The cloud edition runs no generic OIDC. Several providers can run side by side.

## Where machines open: path or subdomain

A machine's page is the machine's own JavaScript. What differs is the origin it runs on:

- **Path mode** (no `HUB_RUNTIME_DOMAIN`): `https://hub.example.com/m/<machine>/`, on the hub's origin. Simple, one name, no
  wildcard. But the page of a machine runs on the same origin as the hub's web page, so script on a machine could call the
  hub's API **with the signed-in person's cookie**. It cannot be fenced in: a sandbox that took away the page's origin
  would take its cookies with it, and the page would not work. So in path mode **a machine is trusted as much as the
  hub**, with its own software too: fine when the machines are yours (one host, a small team that trusts each other),
  not when people connect machines you do not control, and not for the cloud edition (it refuses to start in path mode).
  The hub enforces what it can of that: **in path mode it opens only machines an administrator of the hub connected**
  (`/m/<id>/` and `GET /api/machines/<id>/open` answer 403 `machine_untrusted` for any other, even to a member of its
  space and even with `HUB_SIGNUP=open`), so a stranger who signs up, makes a space and a machine and invites the
  administrator in cannot run script as them. To let members connect machines, set `HUB_RUNTIME_DOMAIN`. The hub says so
  in its log at start and `hub check-config` prints it.
- **Subdomain mode** (`HUB_RUNTIME_DOMAIN=run.example.net`): `https://<machine>.run.example.net`, one origin per machine. The
  hub opens a machine by handing a one-minute, single-use ticket to its origin, which trades it for a cookie of its own
  (valid for that host only, twelve hours, `SameSite=Strict`, named `__Host-hl_run` on https) and goes on to the clean
  address. A machine can neither reach the hub's cookie nor use the other machines' sessions. It needs a wildcard DNS record and a wildcard certificate, and the
  proxy in front must send both names to the hub. **Needed for anything shared.**

**Make `HUB_RUNTIME_DOMAIN` a domain of its own**, not a subdomain of the hub's: for a hub at `hub.example.com`,
`run.example.net` is another *site*, while `m.example.com` is the same site. "Site" is what the browser's public suffix
list calls the registrable domain (`example.com`, `example.co.uk`): a browser sends a person's cookies to every host of
one site, and a script of one host may set cookies for the whole domain. `SameSite=Strict` does **not** separate them:
two hosts under one registrable domain are same-site, whatever the cookie says. The hub narrows the same-site case but
cannot remove it:

- the cookies the hub sets on https carry the `__Host-` prefix (`__Host-hl_session`, `__Host-hl_oauth`,
  `__Host-hl_run`): Secure, `Path=/`, no `Domain`. A browser refuses to store such a cookie unless the host itself set
  it, so a machine's script cannot plant a sign-in state or a machine session in a person's browser, and the hub never
  reads a cookie of the plain name on https;
- a request to a machine's page that does not come from the page itself (`Sec-Fetch-Site` other than
  `same-origin`/`none`, or an `Origin` that is not the machine's) is refused (`cross_site`, 403) unless it is the
  navigation of a whole page (a person following a link or the hub's button), and no machine's page may be shown inside
  a frame of another origin (`frame-ancestors 'self'` is added to every answer of a machine). The browsers that do not
  send Fetch Metadata are covered by `Origin` and the cookie only.

What is left is a script on one machine that is allowed to **send** requests, from its own page, to the hub's API and to
other machines' pages with the cookies the browser attaches. So for machines of people who do not trust each other, use a
domain of its own. The hub checks it with the public suffix list when it starts: a same-site `HUB_RUNTIME_DOMAIN` is a
warning in the log and in `hub check-config`, and in the cloud edition the hub **does not start** with one, unless
`HUB_ALLOW_SAME_SITE_RUNTIME=1` says that the operator accepts that. A `HUB_RUNTIME_DOMAIN` that is a parent of the hub's
own host (`example.com` for `hub.example.com`) is refused in every edition. Using a domain of its own costs a domain, a
wildcard DNS record and a wildcard certificate.

In both modes the hub checks the person's membership of the machine's space on every request and drops their open
connections when they leave the space or the machine is removed.

## What machines download

A machine connects outward to the hub (a WebSocket on the public URL) and downloads two programs from it, so the hub keeps
them in `HUB_DIST_DIR`:

```
install.sh
runtime-linux-amd64   runtime-linux-arm64   runtime-darwin-amd64   runtime-darwin-arm64
agent-linux-amd64     agent-linux-arm64     agent-darwin-amd64     agent-darwin-arm64
```

The hub image carries all of them; for a hub started from a binary, `make hub-dist` builds the folder into `bin/hub-dist`
(`HUB_TARGETS=linux/amd64` for one system). `GET /install` serves `install.sh` with the hub's address filled in,
`GET /dl/manifest.json` lists the files with their SHA-256 sums, and `GET /dl/<name>` serves a file. None of these needs a
login, and none carries a secret: the install command a person copies from the page carries a one-time registration code that
expires in thirty minutes.

## The SSH way

Instead of pasting a command, a person can give the hub an address and a user, and the hub installs the machine once over
SSH. Three things keep one person's server out of reach of another:

- **A key per space.** The hub makes an Ed25519 key for each space on first use. `GET /api/spaces/{id}/machines/ssh`
  answers the public half to the space's **owners** only; they add it to the server's `authorized_keys`. The private half
  is kept in the database sealed with AES-256-GCM (bound to the space) under `HUB_SECRET_KEY`, or under a `secret_key` file
  in the data folder made on first start (`0600`) when it is not set. An owner of another space has a different key, so
  what one owner authorizes on their server gives the others nothing. A stored key that cannot be opened (the secret
  changed) is replaced by a new one, with a warning in the log, and the servers have to be told it.
- **The owner confirms the host key.** The first request has no `hostKeyFingerprint`: the hub only shakes hands, shows the
  server's fingerprint (`SHA256:…`, and its type) in the failed job (`host_key_unconfirmed`) and stops: nothing is run and
  the key is not offered. The owner compares it with what the server says
  (`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`) and sends the request again with `hostKeyFingerprint`; if the server
  now shows another key the job fails with `host_key_mismatch`. Nothing is remembered between requests.
- **Public addresses only.** The hub refuses a server that is not on the public internet, in **every** edition: private,
  loopback, link-local, shared (CGNAT), documentation and reserved ranges, and IPv6 forms that carry an IPv4 address
  (IPv4-mapped, NAT64, 6to4, Teredo), which are judged by the address inside. The name is resolved once, every answer
  must be public, and the connection goes to the address that was checked, so a name that answers differently the second
  time gains nothing (`ssh_blocked`; a refused port, a timeout and an unknown name read alike, `ssh_unreachable`, so the
  hub cannot be used to map a network). A self-hosted hub whose owners may all reach a LAN sets `HUB_SSH_ALLOW_PRIVATE=on`;
  the cloud edition ignores it.

After the install the hub keeps no session and the machine connects outward like any other. The `dir` and `container`
profiles need root or passwordless `sudo` on the server; the check says so when they do not. `HUB_SSH=off` switches the
way off.

**Keep `HUB_SECRET_KEY` (or the data folder with `secret_key`) with the database's backup, but apart from it**: a copy of
the database alone holds only sealed keys. A hub restored without the key makes new keys, and the servers have to be told
again. The earlier `ssh_ed25519` file of a hub that had one key for everybody is no longer used and can be deleted; the
servers that trusted it have to be given their space's key.

## Operating

- **Backups.** SQLite: the file `hub.db` in the data folder (stop the hub first, or `sqlite3 hub.db ".backup out.db"`).
  Postgres: `pg_dump`. The key that seals the SSH keys (`HUB_SECRET_KEY`, or `secret_key` in the data folder) is the only
  other state, and belongs in another place than the database copy. Machines hold their own keys and are not in
  the backup; a restored hub knows them by the public keys it stored.
- **Upgrades.** Replace the binary or the image and restart. Migrations run at start and only go forward: keep a backup
  before an upgrade. Machines reconnect by themselves.
- **Logs.** One line per request (method, route, status, time, client address): the route is the pattern that matched
  (`/api/invites/{token}`), never the path itself, the query string, the headers or the bodies, so a token in an address
  does not reach the log: at `debug` when it went well, at `info` when it was refused (4xx), at `warn` when the hub failed (5xx). Keys,
  environment values and proxy credentials that pass through the hub on their way to a machine are never logged and never
  stored; the only sign-in code in a log is the `DEV` line of a hub without a mail server.
- **Clock.** A machine signs each connection with its time; the hub accepts two minutes of difference. A machine whose clock
  is off cannot connect and says so.
- **Limits.** Sign-in codes, invitations, registration codes, installs and the like are rate-limited per address and per
  account; `HUB_TRUSTED_PROXIES` decides whose `X-Forwarded-For` is believed.

## Troubleshooting

| Symptom | Look at |
| --- | --- |
| The hub does not start | `hub check-config`: every problem with a setting is listed (an https or cloud hub with no `HUB_SMTP_URL` is one) |
| No sign-in code arrives | Plain-http hub with no `HUB_SMTP_URL`: the code is in the log (`DEV`). With SMTP: the hub logs the mail error at warn level |
| The sign-in page lacks GitHub, Yandex or the passkey | The way is off: both halves of its settings must be set; passkeys need a domain name |
| After the callback "state mismatch" or a loop | `HUB_PUBLIC_URL` is not the address in the browser (scheme, host or port differ) or the callback URL at the provider is not exactly the one above |
| A machine cannot connect | Its clock; `HUB_PUBLIC_URL` as the machine sees it; a proxy that drops WebSockets or cuts them after a minute (raise the read timeout to an hour) |
| Terminals work for a minute and drop | The proxy's idle timeout |
| A machine is refused as too old (426) | `HUB_MIN_AGENT_VERSION`: re-run the install command on the machine |
| The SSH way stops with `host_key_unconfirmed` | By design: compare the fingerprint with the server's and send the request again with it |
| The SSH way says the address is not available (`ssh_blocked`) | The server is on a private or reserved address: `HUB_SSH_ALLOW_PRIVATE=on` on a self-hosted hub, or give the server a public one |
| The hub does not start in the cloud edition | `HUB_RUNTIME_DOMAIN` is required there, and it may not be on the hub's registrable domain unless `HUB_ALLOW_SAME_SITE_RUNTIME=1`; `HUB_SMTP_URL` is required too |
| A machine's page answers 403 `cross_site` | A page of another origin asked for it (a fetch, a frame): open it from the hub's button or by its own address |
| Every visitor shares one rate limit | The hub sees only the proxy's address: add the proxy to `HUB_TRUSTED_PROXIES` |
