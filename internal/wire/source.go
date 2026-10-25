package wire

import "context"

// MachineSource serves the machine channel: the machine's record and its changes. It is not convo's — reach comes
// from the cluster's heartbeat (standalone always online), runtime and agents from the runtime itself, load from
// internal/machines — so the hub asks it, not the conversations.
type MachineSource interface {
	// Snapshot is the data of a machine snapshot; Load is set only when load is true.
	Snapshot(ctx context.Context, load bool) (MachineSnapshot, error)

	// Subscribe calls fn with every MachineStatusEv and, while load is true, a MachineLoadEv every 2 s, in order,
	// until cancel is called.
	Subscribe(load bool, fn func(Event)) (cancel func())
}
