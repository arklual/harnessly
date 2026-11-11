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
   entry, and the owner sees that it happened (planned, not built yet).
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

## Next

- The debug mode for administrators (rule 2).
- Chats of several people on one machine: the runtime shows each person only their own chats, so that a shared machine
  stops being shared content.
- Opening one's work to a colleague or a team at two levels, *view* and *give tasks*, instead of the whole machine.
