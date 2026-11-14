// The rollout switches of the hub-first plan for this person (GET <hub>/api/me/switches): which pages read from
// the hub instead of from this machine. Asked once per page load; a runtime with no hub, or a hub that does not answer,
// leaves every page on the machine.
import { useEffect, useState } from 'react';
import { appConfig } from '@/platform/config';

export interface IHubSwitches {
  stats: boolean;
  projects: boolean;
}

export const NO_HUB_SWITCHES: IHubSwitches = { stats: false, projects: false };

let pending: Promise<IHubSwitches> | null = null;

/** The person's switches on the hub; the same promise for every caller until the page is loaded again. */
export const loadHubSwitches = (hubUrl: string | null): Promise<IHubSwitches> => {
  if (!hubUrl) return Promise.resolve(NO_HUB_SWITCHES);
  pending ??= fetch(`${hubUrl}/api/me/switches`, { credentials: 'include' })
    .then(async (res) => {
      if (!res.ok) return NO_HUB_SWITCHES;
      const body = (await res.json()) as Partial<IHubSwitches>;
      return { stats: body.stats === true, projects: body.projects === true };
    })
    .catch(() => NO_HUB_SWITCHES);
  return pending;
};

/** Forget the answer (tests, and a person who signed in again). */
export const resetHubSwitches = (): void => {
  pending = null;
};

/** The switches, or null while the hub has not answered yet. */
export const useHubSwitches = (): IHubSwitches | null => {
  const hubUrl = appConfig().hubUrl;
  const [value, setValue] = useState<IHubSwitches | null>(hubUrl ? null : NO_HUB_SWITCHES);
  useEffect(() => {
    let cancelled = false;
    void loadHubSwitches(hubUrl).then((v) => {
      if (!cancelled) setValue(v);
    });
    return () => {
      cancelled = true;
    };
  }, [hubUrl]);
  return value;
};
