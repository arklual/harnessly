// «Mocks ↔ live» is one setting. Until the backend contracts are in (HRN-531.9) the dev server defaults to mock and a production build to live; `?hubdata=live`
// (remembered) or VITE_HUB_DATA=live switches every screen at once, no screen knows which one it talks to.
import type { Role } from './types';

export type DataMode = 'mock' | 'live';

const MODE_KEY = 'hl-hub-data';
const ROLE_KEY = 'hl-hub-mock-role';

const read = (key: string): string | null => {
  try {
    return sessionStorage.getItem(key) ?? localStorage.getItem(key);
  } catch {
    return null;
  }
};
const write = (key: string, value: string) => {
  try {
    localStorage.setItem(key, value);
  } catch {
    // not remembered
  }
};

let devGallery = false;

export const dataMode = (search = location.search): DataMode => {
  // A production build always asks the live hub: an address must not be able to show a person made-up numbers.
  if (!import.meta.env.DEV) return 'live';
  const asked = new URLSearchParams(search).get('hubdata');
  if (asked === 'mock' || asked === 'live') {
    write(MODE_KEY, asked);
    return asked;
  }
  // The gallery (`?dev`) is mock by definition; a production build is live unless told otherwise.
  if (new URLSearchParams(search).has('dev')) devGallery = true;
  if (devGallery) return 'mock';
  const env = import.meta.env.VITE_HUB_DATA as string | undefined;
  const v = read(MODE_KEY) ?? env;
  if (v === 'mock' || v === 'live') return v;
  return import.meta.env.DEV ? 'mock' : 'live';
};

/** Mock only: which of the three fixture people looks (`?as=admin|lead|member`, remembered). */
export const mockRole = (search = location.search): Role => {
  const asked = new URLSearchParams(search).get('as');
  if (asked === 'admin' || asked === 'lead' || asked === 'member') {
    write(ROLE_KEY, asked);
    return asked;
  }
  const stored = read(ROLE_KEY);
  return stored === 'lead' || stored === 'member' ? stored : 'admin';
};
