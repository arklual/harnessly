// A choice the person made that every device should have (the theme, Beta, the feed's cards…): the store that keeps
// it in this browser says so here after its write, and next/stores/synced-prefs.ts carries it to the person's
// settings. No import of the stores: platform/theme.tsx, shared with the legacy pages, says it too.

export const PREF_WRITE_EVENT = 'hx:pref-write';

/** The browser's storage key of the choice just written (or `voice.lang`). */
export const notePrefWrite = (key: string): void => {
  if (typeof window === 'undefined') return;
  window.dispatchEvent(new CustomEvent<string>(PREF_WRITE_EVENT, { detail: key }));
};
