# Boards

**Boards** are Excalidraw whiteboards that people draw and agents read and edit: an architecture, a data flow, a
user's path. They live in the boards service, [boards](https://github.com/ly-solutions/boards), a program of its own
with its own storage. The machines get the «Доски» app when the hub knows the service's address.

The service is optional. A hub without it starts and works as usual; it simply has no boards app.

## Install

### docker compose

Next to a hub from `compose.hub.yaml`, the overlay `compose.boards.yaml` starts the service from its published image
(`ghcr.io/ly-solutions/boards`) and gives the hub its address and token:

```sh
docker compose -f compose.hub.yaml -f compose.boards.yaml up -d
# with documents as well
docker compose -f compose.hub.yaml -f compose.docs.yaml -f compose.boards.yaml up -d
```

In `.env`:

| Variable | Meaning |
| --- | --- |
| `BOARDS_TOKEN` | The token the hub sends the service, 16 characters or more (`openssl rand -hex 32`). The default in the file is for a trial on one host only. |
| `BOARDS_SESSION_SECRET` | 32 characters or more: keeps people signed in to the editor across restarts. |
| `BOARDS_PUBLIC_URL` | The service's public address (below). Default `http://boards.localhost:8081`. |
| `BOARDS_PORT` | The port, on the host's loopback and inside. Default `8081`. |
| `BOARDS_IMAGE` | The image; pin a release, `ghcr.io/ly-solutions/boards:X.Y.Z`. Default `latest`. |
| `BOARDS_STORAGE` | `fs` (default): files on the `boards-data` volume, one replica. `s3`: a bucket, with `BOARDS_S3_BUCKET`, `BOARDS_S3_ACCESS_KEY_ID`, `BOARDS_S3_SECRET_ACCESS_KEY` and, for a store other than AWS, `BOARDS_S3_ENDPOINT`, `BOARDS_S3_REGION`. |
| `BOARDS_AUTH` | How people sign in to the service: `token` (default) or `harnessly` (they come with the hub's platform token). `harnessly` needs a boards release that has it. |
| `BOARDS_FRAME_EXTRA` | Origins besides the hub that may frame the editor, space-separated: the machines' `https://*.run.example.net` with `HUB_RUNTIME_DOMAIN`. |

Nothing is built: the image is pulled the first time. To run your own build, build it from the repository and name it in
`BOARDS_IMAGE`.

### Helm

The `harnessly-hub` chart runs the service in the release with `boards.*`:

```sh
helm upgrade --install hub deploy/helm/harnessly-hub \
  --set publicUrl=https://hub.example.com \
  --set boards.enabled=true \
  --set boards.url=https://boards.example.com \
  --set boards.storage.type=fs
```

| Value | Meaning |
| --- | --- |
| `boards.enabled` | On: the hub gets `HUB_BOARDS_URL`, `HUB_BOARDS_PUBLIC_URL` and the token. Off by default. |
| `boards.url` | The service's public address (below). **Required.** |
| `boards.bundled` | `true` (default): the chart runs the service. `false`: `url` is a service you run yourself, and `boards.existingSecret` holds its token. |
| `boards.image.repository`, `boards.image.tag` | The image; pin a release in `tag`. |
| `boards.storage.type` | `fs` (a volume of its own, `size`, `storageClass`; one replica) or `s3` (a Secret with the `BOARDS_S3_*` variables in `storage.s3.existingSecret`). **Required** when bundled. |
| `boards.auth` | `token` or `harnessly`, as above. |
| `boards.existingSecret` | A Secret with `token` and `sessionSecret`. Empty: the chart makes one and keeps it across upgrades. |
| `boards.ingress` | Route the host of `url` to the service on the hub's Ingress (`ingress.enabled`); its certificate goes in `ingress.tls`. |
| `boards.frameAncestors` | Origins besides the hub that may frame the editor: with `runtimeDomain`, the machines' `https://*.<runtimeDomain>` when their pages embed it (any page of those origins can then frame it). |

The chart refuses to render when boards are on but cannot work: no `url`, no storage for a service it runs, `s3`
without its Secret, or a service of yours without `existingSecret`. The keys `docs.boards.*` of earlier charts are gone;
the chart says so when it meets them.

## The address

The service has two addresses:

- **Public** — what machines and browsers open, and what the apps put in their frames: `HUB_BOARDS_PUBLIC_URL` on the
  hub, from `BOARDS_PUBLIC_URL` (compose) or `boards.url` (Helm). The apps frame only `https` addresses.
- **Internal** — what the hub calls: `HUB_BOARDS_URL`. The overlay sets it to the service on the compose network, the
  chart to its Service in the cluster; for a service of your own (`boards.bundled=false`) it is `boards.url` too. Without
  `HUB_BOARDS_PUBLIC_URL` the hub hands the machines `HUB_BOARDS_URL`.

Where:

- **One host, a trial:** `http://boards.localhost:8081`. Browsers send `*.localhost` to the host's loopback, where the
  port is published, and the overlay tells the machine's container that the name is the service. Plain http is enough
  to try the service, not for the app's frames.
- **A server:** a name of its own with TLS in front, `https://boards.example.com`. The service allows the hub's address
  to frame it; add the machines' origins yourself when their pages embed it.

## Check that the app is there

1. The service answers: `curl -fsS <address>/healthz` (the process) and `<address>/readyz` (its storage).
2. The hub has the address: `HUB_BOARDS_URL` is in the hub's environment (`docker compose exec hub env | grep BOARDS`).
3. On a machine of the hub, the «Доски» app is in the apps list, and an agent can draw: ask it to sketch a two-box
   diagram on a new board, then open the link.

## Without the service

The hub starts and runs without it; boards are simply not offered. There is no «Доски» app on the machines and nothing
fails. Add the overlay or the values later and the app appears; nothing in the hub has to be migrated. Threads on a
board are kept by the hub's documents feature, so they need [Documents](documents.md) as well.

See also: [Documents](documents.md), [the hub's side of documents and boards](../hub/docs.md).
