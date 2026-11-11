# Rolling a runtime release out to a fleet

The machine's own update (`selfupdate`, [machine.md](machine.md)) pulls three programs from the hub once an hour. A
fleet that is run from a pipeline wants something else: one release, rolled out to every machine at a chosen moment,
checked on each, and put back when a check fails. That is the **fleet mode** of the same updater. It is off unless
`RUNTIME_UPDATE_MODE=fleet` is set; without it nothing described here is active and the update from the hub is exactly
what it was.

## Layout

```
<root>/releases/<version>-<sha12>/   one release; complete when it holds .harnessly-complete
<root>/current                       symlink to the release the services run from
<root>/deployed.json                 the last activation: version, release, previous, time
```

A release is a tarball with one top-level folder `release/`. `release/.harnessly-release.json` names its commit
(`{"version":"…","commit":"<40 hex>"}`); the version is set to the rollout's number when the release is staged.
Point the services at `<root>/current` and keep tmux and the agents out of the units' kill mode, so a restart does not
take the open chats down.

## Two steps

```
harnessly fleet stage    --root /opt/harnessly --version 1.4.0 --tarball 1.4.0.tgz --sha256 <hex>
harnessly fleet activate --root /opt/harnessly --version 1.4.0 --sha256 <hex>
harnessly fleet rollback --root /opt/harnessly
harnessly fleet status   --root /opt/harnessly [--json]
```

- **stage** checks the tarball against its checksum and unpacks it beside the running release. Paths that leave the
  release, absolute links and device files are refused. Nothing that runs changes, so every machine can stage at once,
  and a machine that cannot take the release stops the rollout while no machine has changed.
- **activate** points `current` at the staged release with one rename, runs `RUNTIME_FLEET_RESTART`, and waits until
  `RUNTIME_FLEET_HEALTH_URL` answers 200 with `{"version":"<this release>"}`. If a step fails the previous release is
  put back, the services are restarted again and the command exits 1. A second rollout on the same machine is refused
  while one runs (a lock in `<root>`).
- **rollback** puts back the release `deployed.json` names as the previous one.

### Several services on one machine

A machine that runs one runtime per person must not start them together: a runtime's start is its heaviest minute, and
all of them at once can take the machine down. Name the services in `RUNTIME_FLEET_SERVICES` and `activate` restarts
them **one at a time**, in that order: `RUNTIME_FLEET_RESTART` runs once per service with its name in `FLEET_SERVICE`,
then the service's own health address must answer with the release's version and with its chats loaded, and the next
service waits until those chats are back (at most `RUNTIME_FLEET_SETTLE_TIMEOUT`; a runtime that takes longer does not
fail the release).

```
RUNTIME_FLEET_SERVICES=runtime-anna=http://127.0.0.1:8122/api/health,runtime-boris=http://127.0.0.1:8123/api/health
RUNTIME_FLEET_RESTART='systemctl restart "$FLEET_SERVICE"'
```

The runtime's `/api/health` carries `"chats"`: `starting` until its conversations are loaded (it listens and serves
nothing yet), `restoring` while they answer and the rest of the start still runs, then `ready`. A health answer without
the field (another program, an older runtime) is judged by its version alone. The first service that fails stops the
rollout; only the services restarted so far go back to the previous release, one at a time, and the others never left
it.

### The units of the services

Several runtimes per machine mean several systemd units for each of them, and writing them with a script that asks
systemd about every pair in turn does not fit a loaded machine (one `systemctl` can take 25-90 s). `activate` does it as
a whole when `RUNTIME_FLEET_UNITS_SPEC` names a spec file: it renders the units, writes the files whose text changed
(each through a temporary file and a rename), asks systemd for **one** `daemon-reload` and **one**
`enable --no-reload` for the new units, and only then restarts the services. If the check of the new release fails,
the units are put back and systemd is reloaded once more, together with the services.

The spec is JSON (`"schema": 1`): `vars` (for every template), `pairs` (one object of `vars` per person's pair, with an
optional `host` that limits it to the machine of that name), `files` and `remove`. A file has a `path` and a `content`,
both Go templates (`text/template`, a missing key is an error) over `{{.version}}`, `{{.release}}` (the folder
`current` points at), `{{.host}}` and the variables; `perPair` renders it once for each pair of the machine, `enable`
enables a new unit. A file that renders to nothing must not exist and is removed. Paths stay inside the unit folder.

`harnessly fleet units --spec FILE --dir DIR --host NAME --version V [--apply] [--json]` shows what the spec comes to
without touching anything (with `--apply` it writes and reloads): compare a new spec with the units a machine has
before turning the step on.

### A window between rollouts

`RUNTIME_FLEET_MIN_GAP=24h` makes the machine refuse an activation that comes sooner than that after the last one:
`activate` exits **4** with nothing changed, and the caller tries again later. `activate --force` is the rollout a
person asked for and goes through. `rollback` is never held back. The hourly update from the hub has the same window
as `RUNTIME_UPDATE_MIN_GAP` ([machine.md](machine.md)).

After a successful activation the newest `RUNTIME_FLEET_KEEP` releases (3) stay, with the active and the previous one;
the rest are removed unless something still uses them.

## Deltas

When only the programs changed, the machine does not need the whole release. `harnessly fleet delta` cuts the parts a
pin change moves (`--parts`, by default `bin` and the release file) out of the whole tarball, and checks that the
tarball was packed from the commit you name (`--head`):

```
harnessly fleet delta --tarball 1.4.0.tgz --out 1.4.0.delta.tgz --head <commit>
harnessly fleet stage --version 1.4.0 --tarball 1.4.0.delta.tgz --sha256 <hex of the delta> --delta-base <commit the machine runs>
```

Staging a delta makes the new release as a **hard-linked copy** of the running one with those parts replaced: the
upload is small and the disk holds one copy of everything that did not change. It applies only on top of a running
release of the commit the delta was cut against. On any other machine `stage` exits **3** with nothing changed, and the
caller sends the whole release. A delta that holds anything but its parts is refused.

## Settings

| Variable | Meaning |
|---|---|
| `RUNTIME_UPDATE_MODE=fleet` | Turn the mode on. The runtime stops comparing its programs with the hub's, "Update now" is not offered (the Updates section of Settings stays hidden for a machine in this mode), and the active release is in the `machine.fleet` part of the runtime's `GET /update` answer and in `harnessly fleet status`. |
| `RUNTIME_FLEET_ROOT` | The folder above. Also read by the `harnessly fleet` command (`--root` wins). |
| `RUNTIME_FLEET_RESTART` | A shell command that restarts the services that run from `<root>/current`. |
| `RUNTIME_FLEET_HEALTH_URL` | The address that reports the version the services serve. |
| `RUNTIME_FLEET_HEALTH_TIMEOUT` | Seconds to wait for it (90). |
| `RUNTIME_FLEET_SERVICES` | `name=health address` pairs, comma-separated: restart one service at a time (above). |
| `RUNTIME_FLEET_SETTLE_TIMEOUT` | Seconds the next service waits for the chats of the one before it (300). |
| `RUNTIME_FLEET_MIN_GAP` | The shortest time between two activations (`24h`, `90m`); unset or `0` is no window. |
| `RUNTIME_FLEET_UNITS_SPEC` | The units spec above; unset keeps the units as they are. |
| `RUNTIME_FLEET_UNIT_DIR` | The unit folder (`/etc/systemd/system`). |
| `RUNTIME_FLEET_HOST` | The machine's name for the spec's `host` (the short host name). |
| `RUNTIME_FLEET_KEEP` | Releases kept (3). |
| `RUNTIME_FLEET_PARTS` | Top-level names a delta may replace (`bin,.harnessly-release.json`). |

What drives the machines (a pipeline that uploads the tarball over ssh, runs `stage` on all machines, then `activate`
on all of them, and falls back to the whole release on exit 3) is the deployment's; the list of machines and its
credentials stay in its own configuration, not here.
