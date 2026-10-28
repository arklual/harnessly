// A newer release of harnessly itself (the runtime's GET <platform>/update), as the page tells the person about it.
// Putting the notice away hides that version only; the next release shows it again. What was put away is remembered in
// this browser; with storage blocked the notice shows again on the next page.
import { getPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
const DISMISSED_KEY = 'hx:release-dismissed';

/** Where "How to update" leads: the deployment guide's section for every way of running harnessly. */
export const UPDATE_GUIDE_URL = 'https://github.com/ly-solutions/harnessly/blob/main/docs/deploy.md#updates';

export interface IRelease {
  version: string;
  /** The release page ("What's new"); an https address, or empty when the runtime knows none. */
  url: string;
}

const isHttps = (v: unknown): v is string => typeof v === 'string' && v.startsWith('https://');

/** The newer release the runtime found, or null: none, or an answer this page does not understand. */
export const readRelease = (v: unknown): IRelease | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (o.updateAvailable !== true || typeof o.latest !== 'string' || o.latest === '') return null;
  return { version: o.latest, url: isHttps(o.url) ? o.url : '' };
};

/** Asks the machine's runtime; a failed request is "nothing new". */
export const loadRelease = async (machine: MachineId): Promise<IRelease | null> => {
  const page = await getPage(runtimePath(platformPath('/update'), machine), readRelease, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

export const readDismissed = (): string | null => {
  try {
    return window.localStorage.getItem(DISMISSED_KEY);
  } catch {
    return null;
  }
};

export const writeDismissed = (version: string) => {
  try {
    window.localStorage.setItem(DISMISSED_KEY, version);
  } catch {
    // Storage blocked: the notice is gone for this page only.
  }
};
