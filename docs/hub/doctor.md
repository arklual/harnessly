# `hub doctor` and the codes of the hub's problems

`hub doctor` reads the configuration like the hub does and looks at what the hub depends on. Each line is `ok`, `warn`,
`FAIL` or `skip`; a warning or a failure comes with a code, **what to do**, and a link to its section below. Startup
errors of `hub serve` and `hub check-config` have the same shape:

```
HUB-A001  configuration: HUB_PUBLIC_URL is required, e.g. https://hub.example.com
  what to do: set it to http(s)://host[:port][/path] as people open the hub, e.g. https://hub.example.com/harnessly
  docs:       docs/hub/doctor.md#hub-a001
```

Exit code: `0` nothing failed (warnings do not fail), `1` a check failed, `2` the configuration cannot be read.
`hub doctor --offline` skips what needs the public address to answer (from inside a container it often does not).

| Line | What it looks at |
|------|------------------|
| address | `HUB_PUBLIC_URL` and the path in it |
| database | it connects; the embedded migrations against `schema_migrations` (nothing is written) |
| sites storage | the S3 bucket of the sites, when `HARNESSLY_SITES_S3_ENDPOINT` is set |
| OIDC | `/.well-known/openid-configuration` of each provider |
| proxy | `HUB_TRUSTED_PROXIES` against an https address |
| public address | `GET <HUB_PUBLIC_URL>/healthz` from here, as the proxy passes it |
| clock | the `Date` of that answer against this machine's time |
| extensions | the adapters of `HUB_EXTENSIONS_FILE` ([extensions.md](extensions.md)) |

## HUB-C001
A variable is missing or wrong; the message names it. `hub check-config` shows the result of a fix without starting the hub.

## HUB-A001
`HUB_PUBLIC_URL` or `HUB_AGENT_URL` is not `http(s)://host[:port][/path]`, or the path starts with a path of the hub itself
(`api`, `agent`, `dl`, `m`, `assets`, `sites`, `healthz`, `readyz`, `signin`, `invite`), or has a query, a fragment, escapes,
an empty or a dot segment. See [proxy.md](proxy.md#the-address-and-a-path).

## HUB-A002
The public address does not lead to this hub. The message says how: no answer (DNS, certificate, the proxy is down), a
redirect (a missing trailing slash in the proxy's `location`, or another scheme forced), a status (the proxy does not pass
the path), or a 200 that is not the hub's (another server answers). From a container the public name may not resolve: run
`hub doctor --offline` and check from a machine outside.

## HUB-A003
The address has a path and `HUB_RUNTIME_DOMAIN` is empty. The hub works, but a machine's page (`/m/<id>`) does not under a
path. Set `HUB_RUNTIME_DOMAIN` ([deploy.md](deploy.md#where-machines-open-path-or-subdomain)) or serve the hub at the root.

## HUB-P001
The address is https and `HUB_TRUSTED_PROXIES` is empty: the hub trusts no `X-Forwarded-For`, so every person looks like the
proxy. Set it to the proxy's address ([proxy.md](proxy.md#whom-the-hub-believes)).

## HUB-T001
The clock differs by more than 30 s from the server that answered. Run NTP on the hub and the proxy: codes, tokens and
machine links are signed with the time.

## HUB-D001
The database does not answer. Check `HUB_DATABASE_URL` (host, port, user, password, name) and that the database accepts
connections from this machine; the message is the driver's.

## HUB-D002
Migrations are not applied yet. For `doctor` it is a warning: the hub applies them at start, each in a transaction. When it
is the start that fails with this code, the database user lacks the right to change the schema. Migrations only add
(columns, tables), so the previous version of the hub keeps working on the migrated database.

## HUB-D003
The database has migrations this version does not know: a newer hub wrote them. Do not start this version on it; run the
one that wrote them.

## HUB-S001
The S3 bucket of the sites is not usable: the bucket must exist and the keys must read and write it
(`HARNESSLY_SITES_S3_ENDPOINT`, `_BUCKET`, `_ACCESS_KEY`, `_SECRET_KEY`, `_REGION`).

## HUB-O001
An OpenID provider's configuration cannot be read. The issuer (`HUB_OIDC_<ID>_ISSUER`) must be the address whose
`/.well-known/openid-configuration` answers from this machine.

## HUB-E001
An extension adapter is unreachable or unhealthy; the lines under `extensions` say which ([extensions.md](extensions.md)).

## HUB-M001
A machine's move of its own data into the store stopped on objects with errors (docs/migrations): the line names the
machine, how many of how many objects are in the hub and the error codes. The machine keeps every object, so nothing is
lost; the web shows the same in «Машины» with «Повторить» and «Продолжить с агентом». On the machine,
`harnessly migrate status` lists the objects; each code's cause and fix is in the page of the move,
`docs/migrations/<id>.md`, which `hub doctor` prints under the line.

## HUB-X001
Any other error that stopped the hub; the message is the cause.

## HUB-X002
`HUB_LISTEN` cannot be used: another process holds the port (`ss -ltnp`), or the user may not bind it (ports below 1024).

## HUB-X003
`HUB_DATA_DIR` cannot be made or written by the user the hub runs as.
