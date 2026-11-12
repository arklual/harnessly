# Own work

What runs on a machine (chats, prompts, terminals, the agent's files) is the work of a person. The hub's rights are two
halves that do not mix:

| Half | Who decides | What it gives |
|---|---|---|
| **Content** | only its owner | the owner sees it; anyone else sees it only after the owner opened it to them (a machine's access list today; per chat later) |
| **Structure** | an administrator of a node of the tree (organization, tenant, team; the instance administrator is the administrator of the root) | people, machines, money, limits and the numbers of usage of the node and everything under it, never the content |

Three rules follow:

1. Work is seen by its owner and by those the owner opened it to.
2. An administrator manages but does not read. The one way across is the debug mode: a reason, a time limit, a journal
   entry, and the owner sees that it happened.
3. An agent acts with the rights of its person, never more.

## In the hub now

The machine is still the unit of content: opening a machine's runtime shows every chat on it. The owner's rule
(rollout switch `own-work`, `internal/hub/machaccess.FeatureOwnWork`) applies the first rule to opening:

- with the switch on for the person who opens, only the person who connected the machine and the people and teams its
  access list names let them in;
- an instance administrator, a manager of the tenant or the team, and an empty list ("the whole tenant") no longer do;
- seeing the machine in lists and managing it are not affected (`machaccess.Checker.Reach`);
- the refusal is 403 `own_work` where a role or an empty list would have let the person in before.

It needs `HUB_ACCESS_ENFORCE=on`; it is turned on person by person (`PUT /api/admin/switches` with
`{"feature": "own-work", "person": "<email>", "enabled": true}`), then for everyone with a `(*, *)` row. Before the row
for everyone, a machine shared with its tenant by an empty list needs its people listed, or they lose it.

## Debug mode

When the owner's rule refuses a person only because their role (instance administrator, manager of the tenant or the
team) is not enough, the refusal `own_work` offers them the debug mode instead of a dead end:

- `POST /api/machines/{machineId}/debug` with `{"reason": "…"}` (10 to 500 characters) opens the machine to that person
  for 30 minutes (`machaccess.DebugFor`); `machaccess.Checker.MayDebug` decides who may ask, at most 10 openings an
  hour per person;
- the opening is a row of `machine_debug_openings` (who, which machine, why, until when) and an entry
  `machine.debug_open` of the organization's journal;
- the person who connected the machine gets a letter with the reason and a link to it;
- `GET /api/machines/{machineId}/debug` lists the openings of the last 90 days; the machine card shows them to its
  owner and to the administrators under «Открывали для отладки».

A person the access list does not name and who has no role gets the plain 403 `access` and no debug mode: only roles
that already manage the machine may ask to read it.

## Next

- Chats of several people on one machine: the runtime shows each person only their own chats, so that a shared machine
  stops being shared content.
- Opening one's work to a colleague or a team at two levels, *view* and *give tasks*, instead of the whole machine.
