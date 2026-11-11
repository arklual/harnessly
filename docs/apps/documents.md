# Documents

**Documents** are versioned pages that people and agents publish, read, comment on and share: a plan, a review, a
report. They live in the documents service, [artifacts](https://github.com/ly-solutions/artifacts), a program of its own
with its own storage. The hub keeps what people say about a document — threads, comments, who may see and edit it —
and the machines get the «Документы» app when the hub knows the service's address.

The service is optional. A hub without it starts and works as usual; it simply has no documents app.

## Install

### docker compose

Next to a hub from `compose.hub.yaml`, the overlay `compose.docs.yaml` starts the service from its published image
(`ghcr.io/ly-solutions/artifacts`), turns on `HUB_DOCS` and gives the hub the address:

```sh
docker compose -f compose.hub.yaml -f compose.docs.yaml up -d
# with boards as well
docker compose -f compose.hub.yaml -f compose.docs.yaml -f compose.boards.yaml up -d
```

In `.env`:

| Variable | Meaning |
| --- | --- |
| `DOCS_TOKEN` | The token the hub sends the service, 16 characters or more (`openssl rand -hex 32`). The default in the file is for a trial on one host only. |
| `ARTIFACTS_PUBLIC_URL` | The service's public address (below). Default `http://docs.localhost:8082`. |
| `ARTIFACTS_PORT` | The port, on the host's loopback and inside. Default `8082`. |
| `ARTIFACTS_IMAGE` | The image; pin a release, `ghcr.io/ly-solutions/artifacts:X.Y.Z`. Default `latest`. |
| `ARTIFACTS_STORAGE` | `fs` (default): files on the `artifacts-data` volume, one replica. `s3`: a bucket, with `ARTIFACTS_S3_BUCKET`, `ARTIFACTS_S3_ACCESS_KEY_ID`, `ARTIFACTS_S3_SECRET_ACCESS_KEY` and, for a store other than AWS, `ARTIFACTS_S3_ENDPOINT`, `ARTIFACTS_S3_REGION`. |
| `ARTIFACTS_AUTH` | How people sign in to the service: `token` (default; its own tokens and links) or `harnessly` (they come with the hub's platform token, and the service asks the hub for each person's rights). |
| `DOCS_ACCESS_TOKEN` | With `ARTIFACTS_AUTH=harnessly`: the service's secret for asking the hub about rights, 16 characters or more. The overlay gives it to both. |
| `DOCS_FRAME_EXTRA` | Origins besides the hub that may frame documents, comma-separated: the machines' `https://*.run.example.net` with `HUB_RUNTIME_DOMAIN`. |

Nothing is built: the image is pulled the first time. To run your own build, build it from the repository and name it in
`ARTIFACTS_IMAGE`.

### Helm

The `harnessly-hub` chart runs the service in the release with `docs.*`:

```sh
helm upgrade --install hub deploy/helm/harnessly-hub \
  --set publicUrl=https://hub.example.com \
  --set docs.enabled=true \
  --set docs.url=https://docs.example.com \
  --set docs.storage.type=fs
```

| Value | Meaning |
| --- | --- |
| `docs.enabled` | On: the hub gets `HUB_DOCS`, both addresses and the token. Off by default. |
| `docs.url` | The service's public address (below). **Required.** |
| `docs.bundled` | `true` (default): the chart runs the service. `false`: `url` is a service you run yourself, and `docs.existingSecret` holds its token. |
| `docs.image.repository`, `docs.image.tag` | The image; pin a release in `tag`. |
| `docs.storage.type` | `fs` (a volume of its own, `size`, `storageClass`; one replica) or `s3` (a Secret with the `ARTIFACTS_S3_*` variables in `storage.s3.existingSecret`). **Required** when bundled. |
| `docs.auth` | `token` or `harnessly`, as above. |
| `docs.existingSecret` | A Secret with `token` and, for `harnessly`, `accessToken`. Empty: the chart makes one and keeps it across upgrades. |
| `docs.ingress` | Route the host of `url` to the service on the hub's Ingress (`ingress.enabled`); its certificate goes in `ingress.tls`. |
| `docs.frameAncestors` | Origins besides the hub that may frame documents: with `runtimeDomain`, the machines' `https://*.<runtimeDomain>` when their pages embed it (any page of those origins can then frame it). |

The chart refuses to render when documents are on but cannot work: no `url`, no storage for a service it runs, `s3`
without its Secret, or a service of yours without `existingSecret`. The keys `docs.artifacts.*` of earlier charts are
gone; the chart says so when it meets them.

## The address

The service has two addresses:

- **Public** — what machines and browsers open, and what the apps put in their frames: `HUB_ARTIFACTS_PUBLIC_URL` on the
  hub, from `ARTIFACTS_PUBLIC_URL` (compose) or `docs.url` (Helm). The apps frame only `https` addresses.
- **Internal** — what the hub calls: `HUB_ARTIFACTS_URL`. The overlay sets it to the service on the compose network, the
  chart to its Service in the cluster; for a service of your own (`docs.bundled=false`) it is `docs.url` too. Without
  `HUB_ARTIFACTS_PUBLIC_URL` the hub hands the machines `HUB_ARTIFACTS_URL`.

Where:

- **One host, a trial:** `http://docs.localhost:8082`. Browsers send `*.localhost` to the host's loopback, where the
  port is published, and the overlay tells the machine's container that the name is the service. Plain http is enough
  to try the service, not for the app's frames.
- **A server:** a name of its own with TLS in front, `https://docs.example.com`. The service allows the hub's address
  to frame it; add the machines' origins yourself when their pages embed it.

## Check that the app is there

1. The service answers: `curl -fsS <address>/healthz` (the process) and `<address>/readyz` (its storage).
2. The hub sees it: `curl -fsS <hub>/api/platform/docs/_status` says documents are set up, or names the setting that is
   missing (`HUB_DOCS`, `HUB_ARTIFACTS_URL` or `HUB_ARTIFACTS_TOKEN`).
3. On a machine of the hub, the «Документы» app is in the apps list, and an agent can publish: ask it to publish a
   short note as a document, then open the link.

## Without the service

The hub starts and runs without it; documents are simply not offered. There is no «Документы» app on the machines, the
document screens say "not set up" and name the missing setting, and an agent's publish gets the same answer instead of
an error page. Add the overlay or the values later and the app appears; nothing in the hub has to be migrated.

See also: [Boards](boards.md), [the hub's side of documents](../hub/docs.md).
