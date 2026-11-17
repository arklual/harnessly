# A machine's Teleport identity, approved in the hub

A new machine of a person needs a Teleport machine identity (tbot) to reach the cluster: `kubectl`, the MCP servers
behind Teleport. The bot itself (`agent-<login>`, its roles and its `owner` trait) is the cluster's and lives in git.
What every new machine needs is a join token for its own key. Before HRN-1615 each one was a merge request to the
infrastructure repository; now the person approves it in the hub with one press.

## How it goes

1. The operator's setup of the machine makes a key pair for tbot (the private key never leaves the machine) and writes
   `~/.tbot/join.json`: `{"bot":"agent-<login>","token":"agent-<login>-<machine>","machine":"<host>","publicKey":"ssh-ed25519 …"}`.
   tbot does not start yet: a systemd path unit waits for `~/.tbot/join-approved`.
2. The machine's agent sends `tbot.join` over its link (on link up and every hour after) when `join.json` is there and
   `join-approved` is not, or is the one the agent wrote itself. An observe link carries it only with
   `AGENT_HUB_TBOT_JOIN=on`.
3. The hub checks the request against the machine the link proves: the token is `<bot>-<machine>`, where `<machine>`
   is the machine's host name or its last parts, and the key is one Ed25519 key. It reads the bot's `owner` trait in the
   cluster (`TeleportBotV1`), finds that person's account, checks the switch `tbot-join` for them and the machine, and
   that they may open the machine. Then it keeps the request (`tbot_joins`) and rings the person's bell.
   Anything else is answered `refused` with a code: `invalid_request`, `off`, `no_bot`, `no_owner`, `not_owner`,
   `unavailable` (the machine asks again in an hour).
4. The person sees «Новая машина X просит доступ как agent-…» with the key's fingerprint and the time at the top of the
   screen «Машины» and presses «Разрешить» or «Отказать». Approving checks again that the bot is theirs and creates the
   `TeleportProvisionToken` named after the token, in the namespace of the Teleport operator:
   `roles: [Bot]`, `bot_name`, `join_method: bound_keypair` with the machine's key as `initial_public_key`, standard
   recovery with a limit of 20, label `app.kubernetes.io/managed-by: hub`. Another approved key of the same token is
   revoked: the token answers one key.
5. The hub tells the machine `tbot.joined` `approved` (or the machine hears it the next time it asks); the agent writes
   `join-approved`, the path unit starts tbot.
6. «Отозвать» deletes the token and tells the machine `revoked`; the agent removes the `join-approved` it wrote. A
   declined or revoked key asks no more: a new request needs a new key.

The hub never replaces or deletes a token it did not make (no `managed-by: hub` label): a token from git stays git's,
and approving a request of that name answers 409 `token_exists`.

## What the hub may do in the cluster

The hub's service account has a Role in the operator's namespace on `teleportprovisiontokens` (create, get, list,
delete) and get on `teleportbotsv1`, nothing else. A ValidatingAdmissionPolicy on that account allows only a token whose
name is `agent-<login>-<machine>`, whose roles are exactly `[Bot]`, whose join method is `bound_keypair`, whose
`bot_name` is the name's `agent-<login>` part, and which carries no other join method. So the worst a hub can do is give a
machine an existing bot's identity, which is what the person approves.

## Settings

| Variable | Default | |
| --- | --- | --- |
| `HUB_TBOT_JOIN` | `off` | `on`: the hub takes requests; off, it refuses them and `/api/me/tbot-joins` answers 404 |
| `HUB_TBOT_JOIN_NAMESPACE` | `teleport-operator` | the namespace the Teleport operator watches |
| `HUB_TBOT_JOIN_KUBE_URL` | `https://kubernetes.default.svc` | the API server |
| `HUB_TBOT_JOIN_KUBE_TOKEN_FILE` | the pod's service account token | read on every request |
| `HUB_TBOT_JOIN_KUBE_CA_FILE` | the pod's service account CA | |

The rollout switch `tbot-join` ([rollout-switches.md](rollout-switches.md)) turns it on per person and machine.

## API

All under the person's session; a request is visible only to the person the bot belongs to.

- `GET /api/me/tbot-joins` → `{"requests": [{id, machineId, machineName, host, bot, token, fingerprint, state, createdAt, updatedAt, decidedAt}]}`: waiting and approved ones.
- `POST /api/me/tbot-joins/{id}/approve` → the request; 409 `decided`, `not_owner`, `token_exists`; 502 `cluster_unavailable`.
- `POST /api/me/tbot-joins/{id}/decline`
- `POST /api/me/tbot-joins/{id}/revoke`

Protocol: event `tbot.join` (`hubproto.TbotJoin`), op `tbot.joined` (`hubproto.TbotJoinState`).
