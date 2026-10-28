// The person's sidebar, mounted once in the shell: read with the chat list (stores/profile-sidebar.ts), and
// every machine's own projects and favorites merged into it as each machine's list arrives, so a folder or a favorite
// made before the sidebar was the person's is not lost. A machine that is off is merged when it is back: its runtime
// keeps all it had.
import { useEffect, useMemo } from 'react';
import { sameItems, useAll } from '@/next/core/store';
import { mergeMachine } from '@/next/features/sidebar/profile-layout';
import { convsLoadedStore } from '@/next/stores/conversations';
import { favoritesStore } from '@/next/stores/favorites';
import { editProfileSidebar, useProfileSidebar } from '@/next/stores/profile-sidebar';
import { projectsStore } from '@/next/stores/projects';
import { useCentral } from '@/next/stores/central';
import type { MachineId, ProfileSidebar } from '@/next/protocol/v2';

/** The machines whose own list (with its projects and favorites) has come, sorted. */
const useLoadedMachines = () => useAll(convsLoadedStore, (s) => [...s.keys()].sort(), sameItems);
const useAllProjects = () => useAll(projectsStore, (s) => [...s.values()], sameItems);
/** The layouts themselves only say when to look again; the effect reads them by machine. */
const useAllFavorites = () => useAll(favoritesStore, (s) => [...s.values()], sameItems);

/** Every loaded machine of the copy merged in; the same document when nothing is new. */
export const mergeAll = (
  doc: ProfileSidebar,
  machines: readonly MachineId[],
  projects: ReturnType<typeof useAllProjects>,
  favorites: (m: MachineId) => ProfileSidebar['favorites'] | null,
): ProfileSidebar =>
  machines.reduce(
    (next, m) =>
      mergeMachine(
        next,
        m,
        projects.filter((p) => p.machineId === m),
        favorites(m),
      ),
    doc,
  );

export function useProfileSidebarMerge(): void {
  const profile = useProfileSidebar();
  const central = useCentral();
  const loaded = useLoadedMachines();
  const projects = useAllProjects();
  const layouts = useAllFavorites();
  const machines = useMemo(() => loaded.filter((m) => central.on.has(m)), [loaded, central.on]);
  const doc = profile.k === 'ready' ? profile.doc : null;
  useEffect(() => {
    if (doc === null || machines.length === 0) return;
    const favorites = (m: MachineId) => favoritesStore.get(m) ?? null;
    if (mergeAll(doc, machines, projects, favorites) === doc) return;
    // Made again on the document that stands when it is written; a failure is tried again with the next change.
    editProfileSidebar((now) => mergeAll(now, machines, projects, favorites)).catch(() => undefined);
  }, [doc, machines, projects, layouts]);
}
