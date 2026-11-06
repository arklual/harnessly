import { describe, expect, it } from 'vitest';
import type { IGuidePage, IGuideSection } from '@/next/features/help/api';
import {
  filterContents,
  isOwnPicture,
  joinPath,
  LINK,
  placeOf,
  resolveLink,
  slugOf,
} from '@/next/features/help/model';

const page = (
  name: string,
  title: string,
  file: string,
  headings: string[] = [],
  read = file,
): IGuidePage => ({
  name,
  title,
  headings,
  file,
  read,
  origin: 'builtin',
});

const apps: IGuideSection = {
  id: 'build-apps',
  title: 'Apps',
  audience: 'all',
  origin: 'builtin',
  pages: [
    page('overview', 'Guide', 'build-apps/README.md', ['Where to start'], 'ru/build-apps/README.md'),
    page('first-plugin', 'A first plugin', 'build-apps/01-first-plugin.md', [
      'The manifest',
      'Первый запуск',
    ]),
    page('apps', 'Reference', 'apps.md'),
  ],
};
const start: IGuideSection = {
  id: 'start',
  title: 'Getting started',
  audience: 'people',
  origin: 'builtin',
  pages: [
    page('overview', 'What it is', 'start/README.md'),
    page('pane', 'The right pane', 'start/02-pane.md'),
  ],
};
const team: IGuideSection = {
  id: 'runbooks',
  title: 'Runbooks',
  audience: 'all',
  origin: 'local',
  pages: [{ ...page('overview', 'Our setup', 'start/README.md'), origin: 'local' }],
};
const sections = [start, apps, team];
const at = (section: IGuideSection, n: number) => ({ section, page: section.pages[n]! });

describe('help model', () => {
  it('makes the anchor as GitHub does', () => {
    expect(slugOf('The manifest, v2!')).toBe('the-manifest-v2');
    expect(slugOf('Первый запуск')).toBe('первый-запуск');
  });

  it('finds the page an address names, older addresses included', () => {
    expect(placeOf(sections, null, null)?.page.file).toBe('start/README.md');
    expect(placeOf(sections, 'start', 'pane')?.page.name).toBe('pane');
    expect(placeOf(sections, 'build-apps', null)?.page.name).toBe('overview');
    expect(placeOf(sections, 'build-apps', 'gone')?.page.name).toBe('overview');
    expect(placeOf(sections, 'first-plugin', null)?.section.id).toBe('build-apps');
    expect(placeOf(sections, 'reference', null)?.page.name).toBe('apps');
    expect(placeOf(sections, 'nothing', null)?.section.id).toBe('start');
    expect(placeOf([], null, null)).toBeNull();
  });

  it('joins a relative path and refuses to climb out', () => {
    expect(joinPath('build-apps/README.md', '../apps.md')).toBe('apps.md');
    expect(joinPath('build-apps/README.md', './01-first-plugin.md')).toBe('build-apps/01-first-plugin.md');
    expect(joinPath('start/README.md', '../../x.md')).toBeNull();
  });

  it('keeps links to pages of any section and to headings inside', () => {
    const from = at(apps, 1);
    expect(resolveLink('../apps.md', from, sections)).toEqual({
      kind: LINK.page,
      section: 'build-apps',
      name: 'apps',
      anchor: null,
    });
    expect(resolveLink('README.md#where-to-start', from, sections)).toMatchObject({
      section: 'build-apps',
      name: 'overview',
      anchor: 'where-to-start',
    });
    expect(resolveLink('../start/02-pane.md', from, sections)).toMatchObject({
      section: 'start',
      name: 'pane',
    });
    // A translation's link is relative to the translation.
    expect(resolveLink('../../apps.md', at(apps, 0), sections)).toMatchObject({ name: 'apps' });
    expect(resolveLink('#the-manifest', from, sections)).toEqual({
      kind: LINK.anchor,
      anchor: 'the-manifest',
    });
    expect(resolveLink('#%D1%83%D1%80%D0%BE%D0%B2%D0%BD%D0%B8', from, sections)).toEqual({
      kind: LINK.anchor,
      anchor: 'уровни',
    });
    expect(resolveLink('#100%', from, sections)).toEqual({ kind: LINK.anchor, anchor: '100%' });
  });

  it('prefers the page of the same origin when two sources have one file', () => {
    expect(resolveLink('README.md', at(team, 0), sections)).toMatchObject({ section: 'runbooks' });
    expect(resolveLink('README.md', at(start, 1), sections)).toMatchObject({ section: 'start' });
  });

  it('leaves an outside link, and shows what leads nowhere as text', () => {
    const from = at(apps, 1);
    expect(resolveLink('https://example.com/x', from, sections).kind).toBe(LINK.outside);
    expect(resolveLink('javascript:alert(1)', from, sections).kind).toBe(LINK.plain);
    expect(resolveLink('../apps/moodboard', from, sections).kind).toBe(LINK.plain);
    expect(resolveLink('99-gone.md', from, sections).kind).toBe(LINK.plain);
  });

  it('loads only the page’s own pictures', () => {
    expect(isOwnPicture('img/pane.png')).toBe(true);
    expect(isOwnPicture('https://tracker.example/pixel.png')).toBe(false);
    expect(isOwnPicture('//cdn/x.png')).toBe(false);
    expect(isOwnPicture('/etc/x.png')).toBe(false);
    expect(isOwnPicture('data:image/png;base64,AAAA')).toBe(false);
  });

  it('filters by section, titles and headings', () => {
    expect(filterContents(sections, '')).toHaveLength(3);
    expect(filterContents(sections, 'plugin').map((g) => g.rows.map((r) => r.page.name))).toEqual([
      ['first-plugin'],
    ]);
    expect(filterContents(sections, 'manifest')[0]?.rows[0]?.headings).toEqual(['The manifest']);
    expect(filterContents(sections, 'runbooks')[0]?.rows).toHaveLength(1);
    expect(filterContents(sections, 'нет такого')).toEqual([]);
  });
});
