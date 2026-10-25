// The projects of every machine, from the convs channel: its snapshot carries them with the folder a new chat starts in
// by default, a new one comes as project_upsert. A project is a folder; the sidebar groups the chats under it.

import { createKeyedStore, sameItems, useAll, useKey } from '@/next/core/store';
import type { MachineId, Project } from '@/next/protocol/v2';

export const projectsStore = createKeyedStore<string, Project>();
/** The folder a machine starts a chat in when none is picked; absent until its convs snapshot arrived. */
export const defaultCwdStore = createKeyedStore<MachineId, string>();

export const useDefaultCwd = (machine: MachineId | null) => useKey(defaultCwdStore, machine);

/** A machine's projects by name; empty until its snapshot (the conversations' loading says that). */
export const useProjects = (machine: MachineId | null): Project[] =>
  useAll(
    projectsStore,
    (s) =>
      [...s.values()]
        .filter((p) => machine !== null && p.machineId === machine)
        .sort((a, b) => a.name.localeCompare(b.name)),
    sameItems,
  );
