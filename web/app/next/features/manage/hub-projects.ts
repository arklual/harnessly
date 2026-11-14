// The registry of projects the hub keeps (GET <hub>/api/me/projects): the projects of every machine the person
// may open, a machine that sleeps among them with its label. Asked only while the person's `hub-projects` switch is on;
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

/** The sidebar's projects with the registry's added: a project of a sleeping machine, named with the hub's label. The
 *  projects the page already has stay as they are. */
export const withHubProjects = (
  own: readonly ISidebarProject[],
  hub: readonly IHubProject[],
): readonly ISidebarProject[] => {
  if (hub.length === 0) return own;
  const known = new Set(own.map((p) => p.id));
  const added: ISidebarProject[] = [];
  for (const p of hub) {
    if (known.has(p.id) || !p.asleep) continue;
    known.add(p.id);
    added.push({ id: p.id, name: p.label ? `${p.name} · ${p.label}` : p.name });
  }
  if (added.length === 0) return own;
  return [...own, ...added].sort((a, b) => a.name.localeCompare(b.name));
};
