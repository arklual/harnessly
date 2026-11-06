# Capabilities

`GET /api/capabilities` is one answer to «what does this server do», on the hub and on the runtime, in the same shape:

```json
{ "server": "runtime", "capabilities": [
  { "id": "tracker", "status": "needs_config", "reason": "no tracker is named", "configure": "RUNTIME_TRACKER_URL" },
  { "id": "gitlab", "status": "on" },
  { "id": "vault", "status": "off", "reason": "served by a platform gateway in front of the runtime; the public runtime has no such part" }
] }
```

`status` is `on` (served), `needs_config` (served once `configure` is set) or `off` (this server has no such part).
The answer is open, like `/api/config`: it names no person, secret or address. It only reads settings that already exist
(the hub's feature flags, `RUNTIME_WEB_FEATURES` and the runtime's service settings); none of them changed.

The web reads it with `web/app/platform/capabilities.ts`. A server that does not answer is an older one: the client then
knows nothing, which is not «off».

## The check that keeps the page honest

`node tools/web-routes-check.mjs` (CI job `web-routes`) lists every `/api/…` path the web code calls and looks each up
in the routes the hub and the runtime register. A call nothing serves fails the check unless
`tools/web-routes-known.json` lists it with the capability it hangs on and the task that closes the gap. A listed entry
that is served after all, no longer called, or hangs on a capability the runtime does not name also fails, so the list
stays true. `--report` prints the table of the gaps.
