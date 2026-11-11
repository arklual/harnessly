# Update channels

An installation follows one of two update channels.

| Channel | What it takes | For |
| --- | --- | --- |
| `stable` (the default) | the release the project has named stable: one that has been out for at least a week and was not withdrawn | everybody who runs harnessly for work |
| `latest` | every published release, several a day at times | trying a fix the day it is released |

The channel decides which release the "new version" notice offers and which release the installer takes. It never
moves an installation back: on `stable`, an installation that already runs something newer stays on it until a newer
release becomes stable.

## Where the channel is set

| Installed with | Setting | Default |
| --- | --- | --- |
| `install.sh` (one machine) | `--channel stable\|latest` (or `HARNESSLY_CHANNEL`). The installer keeps it as `RUNTIME_UPDATE_CHANNEL` in `~/.config/harnessly/env`, where the runtime and the next run of the installer read it | `stable` |
| a release binary, `make install`, Docker, Helm | `RUNTIME_UPDATE_CHANNEL=stable\|latest` in the runtime's environment | `stable` |
| a machine connected to a hub | none: the machine takes the programs its hub serves, so its version is the hub's. Choose the hub's version where the hub is installed (its image tag or binary) | follows the hub |
| a machine in [fleet mode](../hub/fleet-rollout.md) | none: its rollout brings whole releases | follows the rollout |

To change the channel of a machine installed with `install.sh`, run the installer again with the other value:

```
curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh -s -- --channel latest
```

For the other ways of running it, set `RUNTIME_UPDATE_CHANNEL` and restart the runtime. A value that is neither
`stable` nor `latest` is read as `stable`, with a warning in the log.

## Seeing the channel

```
harnessly doctor update
```

prints the running version, where this runtime's updates come from, the channel, the newest release of that channel,
and the reason when the channel has no answer. `--json` gives the runtime's own answer, the one the page gets too:
`channel`, `source` (`releases`, `hub`, `fleet` or `none`), `latest`, `updateAvailable` and `note`.

## How a release becomes stable

Every merge that changes the programs is released, so `latest` moves often. `stable` moves only when a maintainer
promotes a release, by hand:

1. GitHub → Actions → **promote-stable** → Run workflow, with the version (`0.16.180`).
2. The workflow asks GitHub for that release and runs `tools/relchannel promote`, which refuses the promotion when
   the release
   - is a draft or a pre-release,
   - is marked withdrawn: `[withdrawn]` in its title or its notes,
   - has been out for less than the window (7 days; the `window_days` input changes it for one run),
   - is older than the release that is stable now (the `allow_older` input lifts this, to take a bad stable release
     away).
3. It writes the pointer and uploads it as the asset `stable.json` of the release named `channels`.

Nothing promotes by itself: there is no schedule. The workflow uses the run's own token, as the release workflow does.

To withdraw a release, add `[withdrawn]` to its title on the releases page. It then can never be promoted. If it is
the stable one already, promote the previous good release with `allow_older`: new installations take that one, and
running installations are not moved back, so they need the fixed release promoted when it is ready.

The same check runs anywhere, without GitHub, on a saved description of the release:

```
go run ./tools/relchannel promote -release release.json -current stable.json -window 168h -out stable.json
```

## The stable pointer

```json
{"schema": 1, "channel": "stable", "version": "0.16.180",
 "url": "https://github.com/ly-solutions/harnessly/releases/tag/v0.16.180",
 "released_at": "2026-10-20T10:00:00Z", "promoted_at": "2026-10-28T09:00:00Z"}
```

It is served at `https://github.com/ly-solutions/harnessly/releases/download/channels/stable.json`. The runtime reads
it at start and every 12 hours, as it read the latest release before; it sends nothing else. `RUNTIME_UPDATE_CHECK=0`
turns the check off for both channels.

A mirror of the releases (`--mirror URL`, see [closed networks](closed-network.md)) keeps the pointer as
`URL/stable.json`, beside `URL/latest.txt`; point the runtime at it with `RUNTIME_UPDATE_STABLE_URL`.

## Older installations and sources without a pointer

- An installation from before the channels knows nothing of them. It keeps reading the latest release and updates as
  it always did, which is the `latest` channel. It moves to `stable` when it is updated to a version with channels and
  `RUNTIME_UPDATE_CHANNEL` is not set to `latest`.
- A source of releases without `stable.json` (a mirror made before the channels, or the time before the first
  promotion): an installation on `stable` is offered nothing and says why, in `harnessly doctor update`, in the
  runtime's update answer (`note`) and from the installer. It does not take the latest release instead. The one exception
  is a first installation with nothing to stay on: the installer says so and installs the latest release, and the
  channel stays `stable`.
- A mirror set with `RUNTIME_UPDATE_CHECK_URL` alone names the latest release only. On `stable` the runtime asks
  nothing of it until `RUNTIME_UPDATE_STABLE_URL` names the mirror's pointer.
