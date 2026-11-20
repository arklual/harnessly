# The person's apps on every VM

A VM is not a place the person has to think about: the apps installed on one of their VMs are on all of them.

- **Where.** The person's own room of the hub's store, `appset-<user>` (collection `appset`). Only machines made by that
  person open it, whatever space they are in (`internal/hub/storehub/appset.go`).
- **What.** `apps/<id>.json` is one app: version, the sum of its package, on/off, the settings' values, the rights the
  person agreed to, or a tombstone for an app removed. `archives/<sum>.tgz` is the package packed (`apps.Pack`) and
  addressed by its sha256, so an app installed from a folder of one VM needs that folder nowhere else.
- **How.** `apps.AppSet` (`internal/apps/appset.go`) runs on every VM linked to a hub (`RUNTIME_APPS_SYNC=off` stops it): a
  pass at start, after each change of the registry or of an app's settings, when another VM writes into the room, and
  every ten minutes. Per app it compares this VM's record, the room's record and what both were at the last pass
  (`<apps>/appset-state.json`): only this VM changed, it publishes; only the room changed, it applies (install or update
  from the archive, switch, settings, removal); both, the later change wins.
- **Not in the set.** An app installed for development (`harnessly app dev`, the author's live folder) stays on its VM;
  the team's and the environment's apps come by their own ways; secrets are shared by the hub's safe, not here;
  the app's stored data is not here; a trusted-tier app runs code on the machine and is installed by hand on each VM.
  Claude Code accounts and credentials are never synced.
- **Rights.** An app comes in with no right beyond what the package reaches and the person agreed to: the grant of
  the other VM is applied only to the keys the package reaches (`Registry.Consent` refuses more), and only with
  `RUNTIME_APPS_SYNC_CONSENT` not off. Off, an app that reaches anything waits for a click on each VM.
- **Left.** A right taken back (`Revoke`) on one VM is not taken back on the others; an app with install values (an
  address of the installation) is not synced; where the deployment requires signed packages an app installed from a
  signed file is not installed by the sync (the signature does not travel); no screen in the page yet lists what the
  sync did or could not do (the runtime's log has it).
