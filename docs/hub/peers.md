# Messages between machines

Agents of one machine already write to each other's chats (`harnessly peers`, `harnessly peers send REF TEXT`; the
runtime's side is in [the CLI's API guide](../../cmd/runtime/api-guide.md)). A hub carries the same messages between
its machines: an agent on one machine lists the chats of the other machines of its space and writes to them, and the
receiving machine decides by its own settings whether to take the message. Nobody types a token and no agent sees one.

- [How a message travels](#how-a-message-travels)
- [Who may write to whom](#who-may-write-to-whom)
- [Turning it on](#turning-it-on)
- [The endpoints](#the-endpoints)

## How a message travels

```
runtime A ──relay token──▶ agent A ──machine key──▶ hub ──link: peers.deliver──▶ agent B ──inbox token──▶ runtime B
 (POST <relay>/send)        (POST /agent/peers/send)                               (POST <base>/api/peers/inbox)
```

1. The sending runtime signs the request with its pair's secret (audience `harnessly-peer-relay`) and posts it to its
   relay, the agent of the same machine on the loopback (`RUNTIME_PEER_RELAY_URL=http://127.0.0.1:<agent port>/peers`).
2. The agent checks the token with the same secret and posts the message to the hub, signed with the machine's
   Ed25519 key under its own domain (`harnessly-agent-peers-v1`: a dial's or a site's signature does not do, nor this
   one for them).
3. The hub names the sender itself (below), finds the other machine and hands the message over that machine's link
   (`peers.deliver`).
4. The other agent signs the sender the hub vouched for with its own pair's secret (audience `harnessly-peer`, 60 s)
   and gives the message to its runtime's inbox. The runtime applies its settings (Settings → Machines → «Связь между
   агентами»: off, everyone, or a list), puts the message into the chat and keeps both sides in its audit.

The directory (`harnessly peers`) goes the same way: the hub asks each reachable machine which of its chats this sender
may write to (`peers.directory`, 5 s each) and gives them back in its own refs.

A chat of another machine is `m-<machine id>/<conversation id>`: the hub rewrites every chat it passes on, so a machine
never learns another's hostname or fleet id, and an answer is written back to the ref it came with.

## Who may write to whom

- **Machines.** The machines of the sender's space, and of the other spaces of its organization. Never itself, never a
  revoked or switched-off machine. An unknown machine, one out of reach and one offline all answer `unreachable`: the
  sender learns nothing of which machines exist.
- **The sender** is the machine the signature proves and a person of its space. The person the runtime names
  (`RUNTIME_OWNER_EMAIL`) is believed as the hub believes a page session: they registered the machine or opened it
  through the hub lately, and still have a role in its space. Otherwise the message is from the person who registered
  the machine; a machine with neither cannot send (`unverified`).
- **The receiver decides.** The hub only carries; the receiving runtime's settings say who may write, and its audit
  keeps every message.
- **Limits.** 30 requests a minute per machine for sends and directory lookups together (`rate-limited`), 1 MB a
  request; the runtime has its own limits (16000 characters, 20 messages a minute from one machine).

## Asking the owner first

With **Ask me first** on (Settings → Machines, `approve: "others"` in the settings), a message from anyone but the
machine's owner does not reach the agent at once. The sender gets `held`, and the message waits in the owner's inbox on
the machine's page, with who sent it, from which machine and chat, to which chat, and until when.

- **Accept** delivers it exactly as it came; **Reject** refuses it with `rejected`. Only the page session decides
  (`POST <platform>/peers/held`): no agent of the machine can accept a message sent to it.
- The sender takes a waiting message back with `harnessly peers revoke REF ID` (`revoked`). Only the machine and person
  that sent it can do that.
- A message waits 24 hours, or as long as the sender asks with `harnessly peers send --ttl 2h …` (7 days at most). Then
  it ends as `expired`.
- The same id again (`harnessly peers send --id ID …`) never runs twice. It answers what came of the first message:
  still held, delivered, rejected, revoked or expired. That is also the protection against a replayed request.
- The audit keeps every step: the requester, the chat that ran the message, and the owner who decided (`by`).
- The inbox lives in the runtime's memory. A restart empties it, and a message sent again waits again.

The owner's own machines are not held: the setting is about other people.

### Where the boundary is

These rules are the application's: they decide who among the people of the fleet may reach an agent, and when. They do
not protect a machine from whoever runs its infrastructure. The hub's operator and anyone with root on the machine can
read what reaches the agents and act as the machine. A task that must not run with the owner's own credentials belongs
on a machine set up for it: its own owner (a service account), and no personal tokens in its safe.

## Turning it on

| Where | Setting | Default |
| --- | --- | --- |
| hub | `HUB_PEERS=on` | `off`: the endpoints answer 404 and the agents tell their runtimes `no-relay` |
| machine, policy | `peers = true` in [policy.toml](machine.md#the-local-policy) | on without a file; a file without the key means off |
| machine, observe link | `AGENT_HUB_PEERS=on` in the agent's environment | off: a closed cluster's machine takes part only when its operator says so |
| machine, runtime | `RUNTIME_PEER_RELAY_URL=http://127.0.0.1:<AGENT_PORT>/peers` | unset: the runtime has no way to other machines |
| machine, agent | `AGENT_RUNTIME_PEERS` | `<runtime base>/api/peers`: where the runtime's inbox and directory are |

The kill switch turns it off with everything else. A refused delivery because of the policy answers the sender `off`.

## The endpoints

| Method, path | Request | Response |
| --- | --- | --- |
| `POST /agent/peers/send` | `Authorization` made like the dial's, under `harnessly-agent-peers-v1`; body `{ person, message: { id, to, fromConversationId, fromTitle, text, via, revoke?, ttl? } }` | 200 `{ id, state: 'delivered' \| 'refused' \| 'held', reason?, detail?, to?: PeerChat }` either way; 401 bad signature, 403 revoked or switched off |
| `POST /agent/peers/directory` | the same header; body `{ person }` | 200 `{ chats: PeerChat[] }`; 429 past the limit |

Operations on the link (hub → agent):

| `op` | Params | `result` |
| --- | --- | --- |
| `peers.deliver` | `{ sender: { person, personName, machine, machineName }, message: { id, conversationId, fromConversationId, fromTitle, text, via } }` | `{ id, state, reason?, detail?, to? }`; `policy_denied` when the machine takes no messages |
| `peers.directory` | `{ sender }` | `{ chats: PeerChat[] }` |

`PeerChat` is `{ ref, conversationId, title, machine, machineName, owner, state: 'busy' | 'idle' | 'gone' }`. The agent's
own loopback routes are `POST /peers/send` (body: the message) and `GET /peers/directory`, both with the relay token.
