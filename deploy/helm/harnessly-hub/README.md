# harnessly-hub

The harnessly hub on Kubernetes: sign-in, spaces and invitations, and the machines people connect, reached through one
address. The chart runs the `hub` image (Dockerfile target `hub`) as one replica. Everything about running a hub, the
sign-in providers and what path mode trusts is in [docs/hub/deploy.md](../../../docs/hub/deploy.md).

```sh
# The Secret holds what must not be in values: the mail server (with its password), the client secrets, the database.
kubectl create secret generic harnessly-hub \
  --from-literal=HUB_SMTP_URL='smtp://user:password@mail.example.com:587' \
  --from-literal=HUB_GITHUB_CLIENT_SECRET='…'

helm upgrade --install harnessly-hub deploy/helm/harnessly-hub \
  --set publicUrl=https://hub.example.com \
  --set 'adminEmails[0]=you@example.com' \
  --set runtimeDomain=run.example.net \
  --set existingSecret=harnessly-hub \
  --set github.clientId=… \
  --set ingress.enabled=true --set ingress.className=nginx \
  --set trustedProxies=10.42.0.0/16 \
  --set 'ingress.tls[0].secretName=hub-tls' --set 'ingress.tls[0].hosts[0]=hub.example.com'
```

`publicUrl` is the only value needed to render; the ingress host comes from it. With `ingress.enabled` the chart also
refuses to render without `adminEmails` (an unnamed first administrator on the internet is whoever gets there first),
and `edition: cloud` refuses to render without `runtimeDomain`. `ci/minimal-values.yaml` and `ci/full-values.yaml`
render every template (`helm lint . -f ci/full-values.yaml`).

Defaults that differ from the hub's own, on purpose: `signup: invite` (only the administrators and people they invite
can create an account; set `open` to let anyone), and `trustedProxies` empty (see below).

## Safe defaults, and what to set

- **`adminEmails`**: the administrators, who must sign in with an address they have proved (a mailed code, or a
  provider that says it checked the address).
- **`trustedProxies`**: empty trusts nobody, so every person shares the ingress controller's address and its sign-in
  limits. Set it to the controller's pod range, not to a private range: a private range believes every pod in the
  cluster, and any of them could write its own address into `X-Forwarded-For`. The controller sees the person's
  address only when its Service has `externalTrafficPolicy: Local` (or the load balancer speaks the PROXY protocol);
  otherwise the address it forwards is a node's, whatever you trust.
- **`runtimeDomain`**: gives every machine an origin of its own. Make it a domain of its own: for a hub at
  `hub.example.com`, `run.example.net` is another site, `m.example.com` is not. Names under one registrable domain are
  the same site, and a browser sends a person's cookies between them; `SameSite=Strict` does not tell them apart. The hub
  warns about a same-site domain at start, the cloud edition does not start with one unless `extraEnv` sets
  `HUB_ALLOW_SAME_SITE_RUNTIME=1` (an accepted risk, see docs/hub/deploy.md), and a domain above the hub's host is refused.
  Without it (path mode) a machine's pages run on
  the hub's origin: only machines you trust completely belong there, and the hub opens only machines an administrator
  connected (403 `machine_untrusted` for any other).
- **`sshAllowPrivate`**: off. The SSH way refuses private, loopback and link-local servers in every edition; turn it
  on only for a self-hosted hub whose owners may all reach the network the hub sits in.

## What it makes

| Object | Notes |
| --- | --- |
| Deployment | One replica, `strategy: Recreate`: a second hub on one SQLite file would corrupt it, and the agents' links and the open streams live in the process. Probes: `/healthz` (alive), `/readyz` (the database answers). Runs as 10002, read-only root, data on a volume. |
| Service | `ClusterIP`, port 80 → the hub's 8040. |
| Ingress | When enabled: the host of `publicUrl` and, with `runtimeDomain`, `*.<runtimeDomain>` (every machine at its own address). WebSockets stay open for an hour (nginx annotations are the default; other controllers need their own). |
| Secret | From `secrets`, unless `existingSecret` names yours. Read as environment: `HUB_DATABASE_URL`, `HUB_SMTP_URL`, `HUB_GITHUB_CLIENT_SECRET`, `HUB_YANDEX_CLIENT_SECRET`, `HUB_OIDC_<ID>_CLIENT_SECRET`, `HUB_SECRET_KEY` (at least 32 characters, what the spaces' SSH keys are sealed with; the chart does not invent it, because a key made at render time would change on every Argo CD sync; without it the hub keeps a key file in the data folder). |
| PersistentVolumeClaim | The data folder: the SQLite file and, unless `HUB_SECRET_KEY` is set, the key file that seals the spaces' SSH keys. With Postgres and `ssh: false` (or `HUB_SECRET_KEY` set) it holds nothing and `persistence.enabled=false` is fine. |
| Postgres | Only with `database.postgres.enabled`: one StatefulSet pod with a volume of its own and a generated password. For a try-out or a small team; for anything else use a database you back up (`HUB_DATABASE_URL` in the Secret). |

## Sign-in

A provider shows up on the sign-in page when its settings are complete. Register the app with the provider first, with
the callback `<publicUrl>/api/auth/oauth/<github|yandex|id>/callback` (deploy.md has the steps for each), then set the
client id in values and the client secret in the Secret.

## Not here

A second replica, a rolling update and autoscaling are not supported (docs/hub/architecture.md, "Not in this version").
Upgrades replace the pod: the machines' links drop and come back by themselves within seconds.
