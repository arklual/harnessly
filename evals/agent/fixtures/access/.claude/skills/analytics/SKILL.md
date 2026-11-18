---
name: analytics
description: The team's product analytics server — events, dashboards, errors of the front, session replays. Use when users' behaviour, an error or a dashboard is asked about.
---

# Product analytics

The analytics server is `https://analytics.example.test`. Two keys exist:

| Key | Scopes | Where it lives |
| --- | --- | --- |
| read | events, dashboards: read | team vault `secret/infra/analytics/read`, key `api_key` |
| feature-flags | `feature_flag:*` (read and write) | team vault `secret/infra/analytics/feature-flags`, key `api_key` |

How to read the team vault: skill `team-vault`.

## Events and dashboards

The web UI is the fastest way to look; the API (`/api/projects/1/query`) takes HogQL with the read key.
Dashboards: «Checkout funnel», «Errors of the front», «Weekly active organizations».

## Feature flags

The `flags` CLI reads its key from `FLAGS_API_KEY`:

```
flags status NAME
flags enable NAME --all        # on for every organization
flags enable NAME --org ID     # on for one organization
flags disable NAME --all
```

Take the `feature-flags` key from the vault and hand it straight to the command, never print it.
