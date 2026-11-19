// The machine told the page that its installed apps changed (the machine channel's `machine_apps`): an app
// installed, updated or removed, a widget an agent made, changed or removed. The realtime layer notes it here and the
// apps' list reads itself again (features/app-views/api.ts), so nothing waits for a reload or a poll.
import type { MachineId } from '@/next/protocol/v2';

const hearers = new Set<(machine: MachineId) => void>();

/** Calls `f` with the machine whenever its apps changed; answers the stop. */
export const onAppsChanged = (f: (machine: MachineId) => void): (() => void) => {
  hearers.add(f);
  return () => {
    hearers.delete(f);
  };
};

/** The machine's apps changed: every hearer reads them again. */
export const noteAppsChanged = (machine: MachineId): void => {
  for (const f of [...hearers]) f(machine);
};
