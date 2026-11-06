# The hub behind a reverse proxy, and under a path

The hub speaks plain http. Whatever gives it an `https` address (Caddy, nginx, an ingress) is a reverse proxy, and the hub
has to know four things about it: the address people open, whom to believe about the client's address, that WebSockets
pass, and how long a connection may live. `hub doctor` checks what it can ([doctor.md](doctor.md)).

## The address, and a path

`HUB_PUBLIC_URL` is `http(s)://host[:port][/path]`. With a path (`https://example.com/harnessly`) the hub lives under it:

- every link the hub builds (invitations, sign-in callbacks, the installer, the link a machine dials) starts with the whole
  address;
- cookies are set for `Path=/harnessly`, and under `https` their names start with `__Secure-` instead of `__Host-` (a
  `__Host-` cookie must have `Path=/`; the browser still keeps the cookie to https and to this host);
- the page is served with its files named under the path, and the web client prefixes its requests and its routes;
- a passkey stays bound to the host (`HUB_RP_ID`), not to the path.

The hub answers a request **with or without** the prefix, so both ways of configuring the proxy work: pass `/harnessly/…`
on as it is, or cut `/harnessly` off. `https://example.com/harnessly` (no slash) goes to `/harnessly/`.

The path is plain segments (`/harnessly`, `/tools/hub`). It may not start with a path the hub itself answers at its root
(`api`, `agent`, `dl`, `m`, `assets`, `sites`, `healthz`, `readyz`, `signin`, `invite`), and takes no query, fragment or
escapes; the hub does not start otherwise (`HUB-A001`). With no path nothing changes from before.

`HUB_AGENT_URL` may carry a path the same way; a machine dials `wss://host/harnessly/agent/v1`.

**Machine pages.** The page a machine serves (`/m/<id>/…`) works under a path only in the subdomain mode, with
`HUB_RUNTIME_DOMAIN` ([deploy.md](deploy.md#where-machines-open-path-or-subdomain)). In the path mode it needs the root
of the host; `hub doctor` warns (`HUB-A003`).

## nginx under `/harnessly/`

```nginx
map $http_upgrade $connection_upgrade { default upgrade; '' close; }

server {
  listen 443 ssl;
  server_name example.com;

  location /harnessly/ {
    proxy_pass http://127.0.0.1:8040/;          # the trailing slash cuts /harnessly off; without it the hub gets the whole path
    proxy_http_version 1.1;                      # WebSocket and event streams need 1.1
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;      # WebSocket: machines and terminals
    proxy_set_header Connection $connection_upgrade;
    proxy_read_timeout 3600s;                    # a machine link and an event stream stay open for hours
    proxy_send_timeout 3600s;
    proxy_buffering off;                         # events reach the browser as they happen
    client_max_body_size 200m;                   # uploads of files into a conversation
  }
}
```

The same config at the root of a host is `location / { proxy_pass http://127.0.0.1:8040; … }`. Caddy:
`handle_path /harnessly/* { reverse_proxy 127.0.0.1:8040 }` (or `handle /harnessly/*` without cutting).

## What the proxy has to pass

| What | Why | If missing |
|------|-----|------------|
| `Host` as the browser sent it | machines sign their dial for the host; cookies and sign-in are bound to it | machine links and sign-in fail with "host mismatch" |
| `X-Forwarded-For` | the real address of a person, for limits and the audit log | everyone looks like the proxy (`HUB-P001`) |
| `X-Forwarded-Proto` | the hub knows the request came over https | redirects go to `http://` |
| `Upgrade` and `Connection` | WebSocket: the link of a machine (`/agent/v1`), terminals, tunnelled pages | machines stay "unreachable", terminals do not open |
| long `proxy_read_timeout` | the hub sends a heartbeat, but not every 60 s | links drop every minute |
| no response buffering for `/api/events` | server-sent events | the screen updates in bursts |

## Whom the hub believes

`HUB_TRUSTED_PROXIES` lists the addresses whose `X-Forwarded-For` is believed, as exact as the proxy's own address (the
default is the local machine). A wide range lets whatever sits in it choose the address every limit counts by. Behind a
proxy on another machine or in another container network, set it to that address; leave it empty only without a proxy.

## Checking by hand

```sh
curl -i https://example.com/harnessly/healthz        # 200 {"ok":true,...}
curl -I https://example.com/harnessly                # 301/302 to /harnessly/ (from nginx or the hub)
hub doctor                                           # the same, and the database, S3, OIDC, the clock
```
