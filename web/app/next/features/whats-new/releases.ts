// «Что нового»: what a release brings, in the person's words — no versions, no file names. The newest release goes
// first; its id is the day it went out (a second one that day takes a letter: 2026-10-27b), so ids sort as strings.
// The texts are next.whatsNew.<id>.<key>.title / .text in app/locales/<lang>/next-whats-new.json.
export interface IReleaseItem {
  key: string;
  icon: string;
}

export interface IRelease {
  id: string;
  items: readonly IReleaseItem[];
}

export const RELEASES = [
  {
    id: '2026-10-28',
    items: [
      { key: 'one', icon: 'messages-square' },
      { key: 'here', icon: 'arrow-right-left' },
      { key: 'off', icon: 'history' },
    ],
  },
  {
    id: '2026-10-27b',
    items: [
      { key: 'safe', icon: 'lock' },
      { key: 'safeSettings', icon: 'key-round' },
    ],
  },
  {
    id: '2026-10-27',
    items: [
      { key: 'telegram', icon: 'send' },
      { key: 'full', icon: 'maximize-2' },
      { key: 'version', icon: 'link' },
    ],
  },
] as const satisfies readonly IRelease[];

/** The release to tell about: the newest one, unless this person has already seen it (or a later one). */
export const unseenRelease = (releases: readonly IRelease[], seen: string | null): IRelease | null => {
  const latest = releases[0];
  if (!latest) return null;
  if (seen !== null && seen >= latest.id) return null;
  return latest;
};
