import type { IArtifactAgent, ILibraryArtifact, ILibraryResponse } from '@/lib/hl-design-library/types';

/** A conversation about the project on one of the machines that hold a copy. */
export interface IProjectChat {
  machineId: string | undefined;
  agent: IArtifactAgent;
}

/**
 * A project of the design library together with the machine it lives on. The library is a folder on a
 * machine, so its boards, pictures, history and chats are all asked of that machine, not of the page's own.
 */
export interface ILibraryProject extends ILibraryArtifact {
  /** `?machine=` of the runtime that listed it; undefined when the page talks to one runtime without a fleet. */
  machineId: string | undefined;
  machineName: string | null;
  /** Loopback port of that machine's library server. */
  port: number;
  /** Every machine that holds a copy: a conversation may start on any of them. */
  machineIds: (string | undefined)[];
  /** Conversations about it on every machine, newest first. */
  chats: IProjectChat[];
}

/** What one machine answered. */
export interface IMachineLibrary {
  machineId: string | undefined;
  machineName: string | null;
  data: ILibraryResponse;
}

/**
 * Of two copies of one project, the one to show: the copy on the page's own machine (its boards open without a hop),
 * otherwise the newer one.
 */
const preferred = (
  candidate: ILibraryProject,
  kept: ILibraryProject,
  pageMachine: string | undefined,
): boolean => {
  if (candidate.machineId === pageMachine && kept.machineId !== pageMachine) return true;
  if (kept.machineId === pageMachine && candidate.machineId !== pageMachine) return false;
  return candidate.updatedAt > kept.updatedAt;
};

/**
 * Projects of every machine in one list, the newest first. The library is synced between machines
 * (design-sync), so a project with the same id on two machines is one project, shown once.
 */
export const mergeLibraries = (
  libraries: readonly IMachineLibrary[],
  pageMachine?: string,
): ILibraryProject[] => {
  const byId = new Map<string, ILibraryProject>();
  for (const { machineId, machineName, data } of libraries) {
    for (const artifact of data.artifacts) {
      const kept = byId.get(artifact.id);
      const machineIds = [...(kept?.machineIds ?? []), machineId];
      const chats = [...(kept?.chats ?? []), ...artifact.agents.map((agent) => ({ machineId, agent }))].sort(
        (a, b) => (b.agent.lastEventAt ?? 0) - (a.agent.lastEventAt ?? 0),
      );
      const project: ILibraryProject = {
        ...artifact,
        machineId,
        machineName,
        port: data.port,
        machineIds,
        chats,
      };
      if (!kept || preferred(project, kept, pageMachine)) byId.set(project.id, project);
      else Object.assign(kept, { machineIds, chats });
    }
  }
  return [...byId.values()].sort((a, b) => b.updatedAt - a.updatedAt);
};

/**
 * The project a place names. A place with a machine takes the project there; one without (an older link), or whose
 * machine no longer has it, takes the newest project with that id on any machine.
 */
export const findProject = (
  projects: readonly ILibraryProject[],
  id: string,
  machine: string | undefined,
): ILibraryProject | null =>
  (machine !== undefined
    ? projects.find((project) => project.id === id && project.machineId === machine)
    : undefined) ??
  projects.find((project) => project.id === id) ??
  null;

/** Machine names are worth showing only when the projects come from more than one machine. */
export const spansMachines = (projects: readonly ILibraryProject[]): boolean =>
  new Set(projects.map((project) => project.machineId)).size > 1;
