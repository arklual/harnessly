// Every «Что нового» entry is whole: both languages, a day, an icon the page can draw. The build refuses a broken entry
// too; here CI names each problem at once.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { whatsNewProblems } from './check';
import { ENTRIES } from './entries';

// The tests run without CSS (vite.config.ts: css: false), so the sheet is read as text.
const icons = readFileSync(join(__dirname, '../../styles/icons.css'), 'utf8');
const FILES = import.meta.glob<string>('./entries/*.json', { eager: true, query: '?raw', import: 'default' });

describe('«Что нового» entries', () => {
  it('are all here, one per file', () => {
    expect(ENTRIES.length).toBe(Object.keys(FILES).length);
    expect(ENTRIES.length).toBeGreaterThan(300);
  });

  it('each have a day, an icon and both languages', () => {
    const broken = Object.entries(FILES)
      .map(([path, raw]) => [path, whatsNewProblems(path.slice('./entries/'.length), raw)] as const)
      .filter(([, problems]) => problems.length > 0);
    expect(broken).toEqual([]);
  });

  it('name icons the page has a glyph for', () => {
    const missing = ENTRIES.filter((entry) => !icons.includes(`data-i='${entry.icon}'`)).map(
      (entry) => `${entry.id}: ${entry.icon}`,
    );
    expect(missing).toEqual([]);
  });
});

describe('whatsNewProblems', () => {
  const entry = (patch: Record<string, unknown>) =>
    JSON.stringify({
      date: '2026-11-21',
      icon: 'sparkles',
      en: { title: 'A title', text: 'A text' },
      ru: { title: 'Заголовок', text: 'Текст' },
      ...patch,
    });

  it('takes a whole entry', () => {
    expect(whatsNewProblems('hrn-713-cards.json', entry({}))).toEqual([]);
  });

  it('catches an entry without one of the languages', () => {
    expect(whatsNewProblems('a.json', entry({ ru: undefined }))).toEqual([
      '"ru" needs a non-empty "title" and "text"',
    ]);
    expect(whatsNewProblems('a.json', entry({ en: { title: 'Only a title', text: ' ' } }))).toEqual([
      '"en" needs a non-empty "title" and "text"',
    ]);
  });

  it('catches a bad day, a missing icon, a bad name and broken JSON', () => {
    expect(whatsNewProblems('a.json', entry({ date: '21.11.2026' }))).toHaveLength(1);
    expect(whatsNewProblems('a.json', entry({ date: '2026-13-45' }))).toHaveLength(1);
    expect(whatsNewProblems('a.json', entry({ icon: '' }))).toHaveLength(1);
    expect(whatsNewProblems('HRN_713.json', entry({}))).toHaveLength(1);
    expect(whatsNewProblems('a.json', '{')).toHaveLength(1);
  });
});
