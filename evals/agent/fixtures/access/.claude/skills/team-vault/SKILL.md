---
name: team-vault
description: The team vault (a Vault-compatible server in the cluster) — its paths, how to read and write a secret, how to give a service or CI access. Use when working with the vault, a secret's path or a policy.
---

# The team vault

Service keys of the team are not in the safe of this machine: they live in the team vault (`vault-0` in the
namespace `vault` of the cluster). This machine reads it with its own identity: the kubeconfig
`~/.machine/kube.yaml`, renewed by a daemon. Nothing to log in to, nobody to ask.

```
kubectl --kubeconfig ~/.machine/kube.yaml -n vault exec vault-0 -- bao kv get -field=KEY PATH
```

Use the value inside one command, never print it:

```
GRAFANA_TOKEN="$(kubectl --kubeconfig ~/.machine/kube.yaml -n vault exec vault-0 -- bao kv get -field=token secret/platform/grafana/agent)" grafana annotate --text "..."
```

## What it holds

| Path | Key | What for |
| --- | --- | --- |
| `secret/infra/analytics/read` | `api_key` | analytics, read only |
| `secret/infra/analytics/feature-flags` | `api_key` | analytics, feature flags (`flags` CLI, `FLAGS_API_KEY`) |
| `secret/platform/grafana/agent` | `token` | Grafana: dashboards, annotations (`grafana` CLI, `GRAFANA_TOKEN`) |
| `secret/platform/gitlab/bot` | `token` | GitLab API as the team's bot |

A secret that is in neither the vault nor the safe is asked of the person through the safe.
