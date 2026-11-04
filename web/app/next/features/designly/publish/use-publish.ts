// «Опубликовать как сайт», the state behind the section of «Поделиться»: the name (from the design's title until the
// person types one), who may open the site, the pages, and the site once the hub has answered. It lives with the design,
// not the dialog, so the address stays after the dialog closes. Pressing waits for the hub and says what it answered.
import { useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import type { IScopePage } from '@/next/features/designly/access/link-scope';
import {
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
  /** The site as the hub last answered, in this session; null before. */
  done: IPublished | null;
  run: () => Promise<void>;
}

export const usePublish = ({ machine, project, title, pages }: IPublishTarget): IPublishState => {
  const [typed, setTyped] = useState<string | null>(null);
  const [access, setAccess] = useState<TSiteAccess>('team');
  const [chosen, setChosen] = useState<string[] | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState<IPublished | null>(null);
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
