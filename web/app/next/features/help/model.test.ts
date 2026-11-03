import { describe, expect, it } from 'vitest';
import type { IGuidePage } from '@/next/features/help/api';
import { filterContents, LINK, resolveLink, slugOf } from '@/next/features/help/model';

const pages: IGuidePage[] = [
  { name: 'overview', title: 'Guide', headings: ['Where to start'] },
  { name: 'first-plugin', title: 'A first plugin', headings: ['The manifest', 'Первый запуск'] },
  { name: 'reference', title: 'Reference', headings: [] },
];

describe('help model', () => {
  it('makes the anchor as GitHub does', () => {
    expect(slugOf('The manifest, v2!')).toBe('the-manifest-v2');
    expect(slugOf('Первый запуск')).toBe('первый-запуск');
  });

  it('keeps links to pages and headings inside the section', () => {
    expect(resolveLink('01-first-plugin.md', pages)).toEqual({
      kind: LINK.page,
      name: 'first-plugin',
      anchor: null,
    });
    expect(resolveLink('./01-first-plugin.md#the-manifest', pages)).toEqual({
      kind: LINK.page,
      name: 'first-plugin',
      anchor: 'the-manifest',
    });
    expect(resolveLink('README.md', pages)).toMatchObject({ name: 'overview' });
    expect(resolveLink('../apps.md', pages)).toMatchObject({ name: 'reference' });
    expect(resolveLink('#the-manifest', pages)).toEqual({ kind: LINK.anchor, anchor: 'the-manifest' });
    expect(resolveLink('../../apps.md', pages)).toMatchObject({ name: 'reference' });
    expect(resolveLink('#%D1%83%D1%80%D0%BE%D0%B2%D0%BD%D0%B8', pages)).toEqual({
      kind: LINK.anchor,
      anchor: 'уровни',
    });
    expect(resolveLink('01-first-plugin.md#%D1%83%D1%80%D0%BE%D0%B2%D0%BD%D0%B8', pages)).toMatchObject({
      anchor: 'уровни',
    });
    expect(resolveLink('#100%', pages)).toEqual({ kind: LINK.anchor, anchor: '100%' });
  });

  it('leaves an outside link, and shows what leads nowhere as text', () => {
    expect(resolveLink('https://example.com/x', pages).kind).toBe(LINK.outside);
    expect(resolveLink('../apps/moodboard', pages).kind).toBe(LINK.plain);
    expect(resolveLink('99-gone.md', pages).kind).toBe(LINK.plain);
  });

  it('filters by titles and headings', () => {
    expect(filterContents(pages, '')).toHaveLength(3);
    expect(filterContents(pages, 'plugin').map((r) => r.page.name)).toEqual(['first-plugin']);
    const rows = filterContents(pages, 'manifest');
    expect(rows[0]?.headings).toEqual(['The manifest']);
    expect(filterContents(pages, 'нет такого')).toEqual([]);
  });
});
