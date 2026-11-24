// The registry of projects the hub keeps (GET <hub>/api/me/projects): the projects of every machine the person
// may open, a machine that sleeps among them. Asked only while the person's `hub-projects` switch is on;
// a runtime with no hub, a hub that says no or one that does not answer leaves the sidebar on the machine's own projects.
import { useEffect, useState } from 'react';
import { appConfig } from '@/platform/config';
import type { ISidebarProject } from '@/next/features/sidebar/types';
import { useHubSwitches } from './hub-switches';

export interface IHubProject {
  id: string;
  name: string;
  machineId: string;
  machineName: string;
  asleep: boolean;
  /** «машина спит» when the machine is not on, said by the hub. */
  label?: string;
}

const EVERY_MS = 60_000;
const NONE: readonly IHubProject[] = [];

/** The registry's projects, or [] while the switch is off, the hub has not answered or answers no. */
export const useHubProjects = (): readonly IHubProject[] => {
  const hubUrl = appConfig().hubUrl;
  const on = useHubSwitches()?.projects === true;
  const [rows, setRows] = useState<readonly IHubProject[]>([]);
  useEffect(() => {
    if (!hubUrl || !on) return undefined;
    let cancelled = false;
    const load = () => {
      fetch(`${hubUrl}/api/me/projects`, { credentials: 'include' })
        .then(async (res) =>
          res.ok ? (((await res.json()) as { projects?: IHubProject[] }).projects ?? []) : [],
        )
        .catch(() => [] as IHubProject[])
        .then((list) => {
          if (!cancelled) setRows(list);
        });
    };
    load();
    const timer = setInterval(load, EVERY_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [hubUrl, on]);
  // Off, the rows kept from before are not shown: no setState in the effect for it (react-hooks/set-state-in-effect).
  return hubUrl && on ? rows : NONE;
};

/** The sidebar's projects with the registry's added. A project is the person's, not a machine's: the same name on
 *  several machines is one project, shown once and without the machine. A project the list already has (by id, by name,
 *  or, in the person's own sidebar, as a machine folder of one of its projects) is not repeated; a project only another
 *  machine has joins by its name alone. Such a row takes no chat from this machine (`elsewhere`). */
export const withHubProjects = (
  own: readonly ISidebarProject[],
  hub: readonly IHubProject[],
  inProfile: (machineId: string, projectId: string) => boolean = () => false,
): readonly ISidebarProject[] => {
  if (hub.length === 0) return own;
  const key = (name: string) => name.trim().toLocaleLowerCase();
  const known = new Set(own.map((p) => p.id));
  const names = new Set(own.map((p) => key(p.name)));
  const added: ISidebarProject[] = [];
  for (const p of hub) {
    if (known.has(p.id) || names.has(key(p.name)) || inProfile(p.machineId, p.id)) continue;
    known.add(p.id);
    names.add(key(p.name));
    added.push({ id: p.id, name: p.name, elsewhere: true });
  }
  if (added.length === 0) return own;
  return [...own, ...added].sort((a, b) => a.name.localeCompare(b.name));
};
