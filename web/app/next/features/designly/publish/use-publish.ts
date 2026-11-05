// «Опубликовать как сайт», the state behind the section of «Поделиться»: the name (from the design's title until the
// person types one), who may open the site, the pages, and the site once the hub has answered. It lives with the design,
// not the dialog, so the address stays after the dialog closes. Pressing waits for the hub and says what it answered.
import { useEffect, useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import type { IScopePage } from '@/next/features/designly/access/link-scope';
import {
  fetchPublished,
  publishDesign,
  siteNameOf,
  validSiteName,
  type IPublished,
  type TSiteAccess,
} from '@/next/features/designly/publish/publish-api';
import { t } from '@/next/i18n';

export interface IPublishTarget {
  machine: MachineId;
  project: string;
  title: string;
  pages: readonly IScopePage[];
  /** Read the site back only where it may be published (a manager of a site design): the runtime refuses others. */
  enabled?: boolean;
}

export interface IPublishState {
  name: string;
  setName: (name: string) => void;
  valid: boolean;
  access: TSiteAccess;
  setAccess: (access: TSiteAccess) => void;
  chosen: string[] | null;
  setChosen: (chosen: string[] | null) => void;
  pages: readonly IScopePage[];
  busy: boolean;
  error: string | null;
  /** The site as the hub last answered: read back once the design opens, then what publishing answers; null
   *  while there is none. */
  done: IPublished | null;
  run: () => Promise<void>;
}

export const usePublish = ({
  machine,
  project,
  title,
  pages,
  enabled = true,
}: IPublishTarget): IPublishState => {
  const [typed, setTyped] = useState<string | null>(null);
  const [access, setAccess] = useState<TSiteAccess>('team');
  const [chosen, setChosen] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<IPublished | null>(null);
  // The site made from this project before, read back from the hub: its address, name and access stay after a reload.
  // A machine without a hub with sites, or one that does not know the route yet, answers nothing; that is no error.
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    fetchPublished(machine, project).then(
      (site) => {
        if (!live || site === null) return;
        setDone((was) => was ?? site);
        setTyped((was) => was ?? site.name);
        if (site.access) setAccess(site.access);
      },
      () => undefined,
    );
    return () => {
      live = false;
    };
  }, [machine, project, enabled]);
  const name = typed ?? siteNameOf(title);
  const valid = validSiteName(name);

  const run = async () => {
    if (busy || !valid) return;
    setBusy(true);
    setError(null);
    try {
      setDone(await publishDesign(machine, { project, name, access, pages: chosen }));
    } catch (failure) {
      setError(failure instanceof Error && failure.message ? failure.message : t('next.failure.http'));
    } finally {
      setBusy(false);
    }
  };

  return {
    name,
    setName: (next) => setTyped(next.toLowerCase()),
    valid,
    access,
    setAccess,
    chosen,
    setChosen,
    pages,
    busy,
    error,
    done,
    run,
  };
};
