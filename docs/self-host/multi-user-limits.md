# Known limits of shared and team installations

harnessly started as a tool for one person on one machine. Several people on one machine, and many machines behind one
hub, work, with the limits below. They are listed so that whoever runs an installation can plan around them; none of
them needs an action on a single-person machine.

## Several people on one machine

- **One runtime per account.** All the chats of an account on a machine live in one runtime process. When that process
  restarts (an update, a crash, a restart of the service), every chat of the account loses its page for the time of
  the start. The agents keep working in tmux and the page catches up afterwards.
- **A restart is not instant on a busy machine.** After a start the runtime reads the chats' histories again. With
  hundreds of chats and little spare CPU, the page may need minutes before every chat shows its current state. Updates
  are therefore spaced out (`RUNTIME_UPDATE_MIN_GAP`, a day by default), and the [stable channel](update-channels.md)
  keeps their number low.
- **Every chat keeps a terminal session.** A stopped chat still costs a few megabytes of memory. Archive or delete the
  chats nobody needs on a machine that holds many.
- **No quotas between people.** The accounts of a machine share its CPU, memory and disk. One person's heavy build or
  test run slows down everybody's chats, and a full disk stops them all. Give a team machine room to spare, or one
  machine per person where the work is heavy.
- **The agent's state is read from its terminal.** Whether a message reached the agent, and whether its turn is over,
  is concluded from what the agent's own program prints. With a version of an agent program newer than the ones this
  release was tested with, a status may lag behind, and in rare cases a message is delivered twice or needs sending
  again. Keep the agent programs at the versions the release notes name when this matters.
- **A rollout restarts the accounts in turn.** In [fleet mode](../hub/fleet-rollout.md) the runtimes of a machine with
  several accounts are restarted one after another, each waiting for the chats of the one before it; a rollout of a
  machine takes as long as its accounts together.

## A team behind a hub

- **One replica unless the hub is on Postgres.** The connections of the machines and the open streams of the pages
  live in the hub's process. With the SQLite file there is one replica and no rolling update: upgrading the hub
  interrupts every page and every machine for about half a minute, and they reconnect by themselves. On Postgres the
  chart can run two replicas and replace them in turn ([Several replicas](../hub/replicas.md)); the machines of the replica
  being stopped call again within seconds. Upgrade outside working hours all the same, and not more often than you
  need.
- **The hub is a single point.** While it is down, nobody reaches a machine through it; the agents on the machines keep
  working. Back up its database and its secret key ([Running a hub](../hub/deploy.md)), and prefer Postgres to the
  SQLite file for a team.
- **Everything to a machine goes through its one link.** All the people of a machine share the connections of that
  machine to the hub (512 at a time). A machine that is overloaded answers slowly to everybody, and requests may wait
  until the page gives up. A very large answer (tens of megabytes) to a slow client may be cut; large files travel
  better as uploads than as answers.
- **The hub's copy of a conversation may lag.** The machine holds the conversation; the hub keeps a copy to show it
  when the machine is off. After a break of the link the copy catches up, and for a short time it may miss the end of
  the last turn.
- **Monitoring is opt-in.** The hub answers a health check and writes logs; its numbers (links of the machines, queues,
  the lag of the chats' copy) are served only when `HUB_METRICS_LISTEN` is set ([Running a hub](../hub/deploy.md)).
  Collect them, and keep the logs somewhere that outlives the container.
- **One version for everybody.** The machines take the programs their hub serves, so upgrading the hub upgrades every
  machine within the hour (each not more often than `RUNTIME_UPDATE_MIN_GAP`). There is no per-team or per-machine
  version behind one hub.

## What helps

- Follow the [stable channel](update-channels.md) and upgrade the hub deliberately, not on every release.
- Size a shared machine for its busiest hour, not the average one.
- Read the release notes before upgrading a hub that many people use, and keep the previous image or binaries at hand
  ([Upgrading as one release](../hub/upgrade.md)).
