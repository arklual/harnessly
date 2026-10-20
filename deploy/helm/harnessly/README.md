# harnessly Helm chart

Runs [harnessly](https://github.com/ly-solutions/harnessly) in Kubernetes: one pod per person with the runtime, its
tmux server and the agents, the home folder on a PersistentVolumeClaim, the web page behind a Service and an optional
Ingress. Boards and documents panes appear when the release points at a
[boards](https://github.com/ly-solutions/boards) server and an [artifacts](https://github.com/ly-solutions/artifacts)
catalog (deploy them with their own charts; [deploy/argocd](../../argocd) has an app of apps for all three).

```sh
helm install ada deploy/helm/harnessly \
  --set ownerName=Ada \
  --set ingress.enabled=true --set 'ingress.hosts[0].host=ada.harnessly.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/'
```

The first visit sets the page's password. The default image has no agent CLIs: build the Dockerfile's `with-agents`
target (Claude Code and Codex from npm) and set `image.repository`, or install them into the home volume from a
terminal tab (`npm install -g --prefix ~/.local …`, or Claude Code's own installer).

## Boards and artifacts

```yaml
integrations:
  boards:
    url: https://boards.example.com
  artifacts:
    url: https://artifacts.example.com
existingSecret: ada-harnessly   # RUNTIME_BOARDS_TOKEN, RUNTIME_BOARDS_AGENT_TOKEN, RUNTIME_ARTIFACTS_TOKEN, RUNTIME_ARTIFACTS_READ_TOKEN
```

On their side, allow this page to embed them: `integration.frameAncestors` / `integration.corsOrigins` in the boards
chart and `embed.frameAncestors` in the artifacts chart, set to the page's origin. Host all three under one registrable
domain (`*.example.com`) so the panes keep their sign-in cookies.

Agents get `BOARDS_URL`, `BOARDS_TOKEN`, `ARTIFACTS_URL`, `ARTIFACTS_TOKEN`, `ARTIFACTS_SESSION` and `ARTIFACTS_AUTHOR`
in every tab; the [agent skills](https://github.com/ly-solutions/agent-skills) use them.

## Values

| Key | Default | Description |
| --- | --- | --- |
| `image.repository` | `ghcr.io/ly-solutions/harnessly` | image; build the `with-agents` target for one with Claude Code and Codex |
| `image.tag` | chart `appVersion` | image tag |
| `image.pullPolicy` | `IfNotPresent` | |
| `imagePullSecrets` | `[]` | |
| `nameOverride`, `fullnameOverride` | `""` | |
| `basePath` | `/` | path the page and its API are served under (`RUNTIME_APP_BASE_PATH`) |
| `ownerName` | `""` | the person's display name: boards shows it, agents publish as "`<name>` (agent)" |
| `ownerEmail` | `""` | the person's e-mail for board sign-ins |
| `integrations.boards.url` | `""` | origin of a boards server as browsers reach it; empty: no board pane |
| `integrations.artifacts.url` | `""` | origin of an artifacts catalog as browsers reach it; empty: no document pane |
| `existingSecret` | `""` | Secret read as environment: the runtime's tokens and anything else (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, …); empty: the chart creates one from `secrets` |
| `secrets` | `{}` | keys of the Secret the chart creates when `existingSecret` is empty |
| `extraEnv` | `[]` | more environment (`RUNTIME_*`, see [configuration](../../../docs/configuration.md)) |
| `extraEnvFrom` | `[]` | more environment from ConfigMaps or Secrets |
| `persistence.enabled` | `true` | keep the home folder on a PVC (a volumeClaimTemplate); off: an emptyDir, lost with the pod |
| `persistence.size` | `20Gi` | |
| `persistence.storageClass` | `""` | empty: the cluster's default |
| `persistence.accessModes` | `[ReadWriteOnce]` | |
| `persistence.annotations` | `{}` | annotations of the claim template |
| `tmpSizeLimit` | `1Gi` | size limit of the `/tmp` emptyDir (tmux's sockets) |
| `service.type` | `ClusterIP` | |
| `service.port` | `80` | |
| `service.annotations` | `{}` | |
| `ingress.enabled` | `false` | |
| `ingress.className` | `""` | |
| `ingress.annotations` | `{}` | raise the proxy's read timeout for the WebSockets (terminals, conversations) |
| `ingress.hosts` | `harnessly.example.com`, `/` | |
| `ingress.tls` | `[]` | |
| `serviceAccount.create` | `true` | |
| `serviceAccount.name` | `""` | |
| `serviceAccount.annotations` | `{}` | |
| `serviceAccount.automount` | `false` | mount the API token into the pod (the agents could use it) |
| `podAnnotations`, `podLabels` | `{}` | |
| `podSecurityContext` | uid/gid 10001, non-root, `fsGroup` 10001, RuntimeDefault seccomp | |
| `securityContext` | no privilege escalation, no capabilities, read-only root | the home volume and `/tmp` are the writable mounts |
| `resources` | requests 250m / 512Mi, limit 4Gi memory | agents and their tools run inside this limit |
| `startupProbe`, `livenessProbe`, `readinessProbe` | `GET <basePath>/api/health` | the path follows `basePath` |
| `terminationGracePeriodSeconds` | `30` | |
| `nodeSelector`, `tolerations`, `affinity` | empty | |

`helm test <release>` checks the health route through the Service.
