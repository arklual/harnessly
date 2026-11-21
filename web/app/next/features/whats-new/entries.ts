// The entries of «Что нового»: one file per feature in ./entries (see ./entries/README.md), so two branches that each
// add one never touch the same file. Only the day and the icon come into the page's code from here; the texts go into
// the dictionary of each language (`dictionaries` in vite.config.ts), so the phone gets its own language alone.
import { byNewest } from './news';
import type { IEntry } from './seen';

const DATES = import.meta.glob<string>('./entries/*.json', { eager: true, import: 'date' });
const ICONS = import.meta.glob<string>('./entries/*.json', { eager: true, import: 'icon' });

const idOf = (path: string) => path.slice('./entries/'.length, -'.json'.length);

export const ENTRIES: readonly IEntry[] = Object.keys(DATES)
  .map((path) => ({ id: idOf(path), date: DATES[path] ?? '', icon: ICONS[path] ?? 'sparkles' }))
  .sort(byNewest);
