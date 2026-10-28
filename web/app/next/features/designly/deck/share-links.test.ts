import { describe, expect, it } from 'vitest';
import type { IGuestLink } from '@/next/features/designly/access/api';
import {
  deckLinkOf,
  liveLinks,
  otherLinks,
  pdfLineOf,
  revokeAll,
} from '@/next/features/designly/deck/share-links';
import type { IDeckPublished } from '@/next/features/designly/deck/types';

const link = (id: string, extra: Partial<IGuestLink> = {}): IGuestLink => ({
  id,
  title: id,
  rights: 'view',
  expiresAt: null,
  revokedAt: null,
  opens: 0,
  createdAt: '2026-01-01T00:00:00Z',
  url: `https://example.org/s/${id}`,
  comments: 0,
  ...extra,
});

describe('share links', () => {
  const now = Date.parse('2026-06-01T00:00:00Z');
  const links = [
    link('deck', { view: { deck: 'slides', pdf: true, stats: true } }),
    link('site'),
    link('gone', { revokedAt: '2026-02-01T00:00:00Z' }),
    link('old', { expiresAt: '2026-03-01T00:00:00Z' }),
  ];

  it('counts every live link, not only the deck one', () => {
    const live = liveLinks(links, now);
    expect(live.map((l) => l.id)).toEqual(['deck', 'site']);
    expect(deckLinkOf(live)?.id).toBe('deck');
    expect(otherLinks(live).map((l) => l.id)).toEqual(['site']);
  });

  it('revokes all live links and keeps going when one fails', async () => {
    const called: string[] = [];
    const out = await revokeAll(liveLinks(links, now), async (id) => {
      called.push(id);
      if (id === 'site') throw new Error('refused');
    });
    expect(called).toEqual(['deck', 'site']);
    expect(out).toEqual({ revoked: ['deck'], failed: ['site'] });
  });
});

describe('pdfLineOf', () => {
  const none: IDeckPublished = {
    video: false,
    pdf: false,
    pdfAuto: true,
    pdfBuilding: false,
    pdfAt: null,
    pdfError: null,
  };

  it('says the first PDF is coming while the link already works', () => {
    expect(pdfLineOf({ ...none, pdfBuilding: true })).toEqual({ k: 'building' });
  });

  it('says a published PDF is catching up while a newer one builds', () => {
    expect(pdfLineOf({ ...none, pdf: true, pdfAt: '2026-10-28T10:00:00Z', pdfBuilding: true })).toEqual({
      k: 'updating',
    });
  });

  it('says when the published PDF was built, and why a build failed', () => {
    expect(pdfLineOf({ ...none, pdf: true, pdfAt: '2026-10-28T10:00:00Z' })).toEqual({
      k: 'ready',
      at: Date.parse('2026-10-28T10:00:00Z'),
    });
    expect(pdfLineOf({ ...none, pdfError: 'No Chrome' })).toEqual({ k: 'failed', reason: 'No Chrome' });
  });

  it('says nothing while there is no PDF and none is coming', () => {
    expect(pdfLineOf(none)).toBeNull();
  });
});
