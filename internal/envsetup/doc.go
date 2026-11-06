// Package envsetup sets the machine up by the hub's environment list (docs/hub/environment.md, «Machine setup»): the
// forges' CLIs logged in, repositories cloned, scripts run in order.
//
// It is the runtime's, not the agent's. The machine agent runs sandboxed by systemd (the home read-only, its own data
// folder the only place to write) and must not run a program of anyone's choosing; the runtime runs as the person, holds
// the person's safe and is where the person's own commands run anyway. The two meet in the link's folder, in files that
// survive a restart of either:
//
//	agent → runtime   environment.json                the list the agent holds (hash, items)
//	                  environment-requests/<id>.json  a run the hub asked for (hubproto.EnvRunRequest)
//	runtime → agent   environment-setup.json          the hash worked on, each item's status, the ledger of «once» scripts
//	                  environment-runs/<id>.json      a run as it stands, rewritten as steps move (hubproto.EnvRun)
package envsetup
