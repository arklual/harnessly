import { describe, expect, it } from 'vitest';
import { t } from '@/next/i18n';
import {
  blankForm,
  changeOf,
  formOf,
  inputOf,
  linkSub,
  LINK_KIND,
  readOnly,
  seenLabel,
  type ILinkContext,
} from '@/next/features/designly/access/link-form';
import { shortDay } from '@/next/features/designly/access/labels';
import type { IAccessLink } from '@/next/features/designly/access/resource-api';

const NOW = new Date('2026-11-04T12:00:00').getTime();

const link = (over: Partial<IAccessLink> = {}): IAccessLink => ({
  id: 'l1',
  forWhom: 'Иван, инвестор',
  rights: 'view',
  expiresAt: new Date('2026-11-11T12:00:00').toISOString(),
  disabledAt: null,
  opens: 0,
  lastOpenedAt: null,
  createdAt: '2026-11-01T10:00:00Z',
  createdBy: 'boss@x.ru',
  comments: 0,
  options: {},
  version: 'pinned',
  pages: null,
  copyable: true,
  ...over,
});

const design: ILinkContext = {
  kind: LINK_KIND.design,
  pages: [
    { path: 'index.dc.html', title: 'Главная' },
    { path: 'pay.dc.html', title: 'Оплата' },
  ],
};

describe('the form of a link (HRN-675)', () => {
  it('a new design link: view, a week, this version, all pages, on a computer', () => {
    const input = inputOf({ ...blankForm(design), forWhom: '  Иван  ' }, design);
    expect(input).toEqual({
      forWhom: 'Иван',
      rights: 'view',
      ttl: '7d',
      options: { device: 'fill' },
      version: 'pinned',
      pages: null,
    });
  });

  it('a design link on a phone carries a phone look; some pages are sent, all of them are «all»', () => {
    const form = { ...blankForm(design), forWhom: 'М', device: 'phone' as const, pages: ['pay.dc.html'] };
    const input = inputOf(form, design);
    expect(input.options).toMatchObject({ v: 1, device: 'ip14', width: 390, height: 844 });
    expect(input.pages).toEqual(['pay.dc.html']);
    expect(inputOf({ ...form, pages: ['index.dc.html', 'pay.dc.html'] }, design).pages).toBeNull();
  });

  it('a document link carries downloading and copying; both off reads as «only on screen»', () => {
    const doc: ILinkContext = { kind: LINK_KIND.doc };
    const form = { ...blankForm(doc), forWhom: 'Кастдев', download: false, copy: false };
    expect(inputOf(form, doc).options).toEqual({ download: false, copy: false });
    expect(readOnly(form, LINK_KIND.doc)).toBe(true);
    expect(readOnly({ ...form, copy: true }, LINK_KIND.doc)).toBe(false);
  });

  it('a board link offers neither a file nor copying: its options are empty, its line has no «без копирования»', () => {
    const board: ILinkContext = { kind: LINK_KIND.board };
    const form = { ...blankForm(board), forWhom: 'Партнёр', download: true, copy: false };
    expect(inputOf(form, board).options).toEqual({});
    expect(readOnly(form, LINK_KIND.board)).toBe(false);
    expect(linkSub(link({ options: { copy: false } }), LINK_KIND.board, NOW)).not.toContain(
      t('next.access.links.noCopy'),
    );
  });

  it('a deck link: how it opens, from which slide (kept within the deck), notes, counting, its PDF', () => {
    const deck: ILinkContext = { kind: LINK_KIND.deck, slides: 5 };
    const options = inputOf(
      { ...blankForm(deck), forWhom: 'A', from: 9, notes: true, download: true },
      deck,
    ).options;
    expect(options).toEqual({
      v: 1,
      deck: 'slides',
      pdf: true,
      video: true,
      stats: true,
      from: 5,
      notes: true,
    });
  });

  it('a video link: the format only where both are built; comments on the timeline only for a commenting link', () => {
    const one: ILinkContext = { kind: LINK_KIND.video, formats: ['9:16'] };
    const both: ILinkContext = { kind: LINK_KIND.video, formats: ['16:9', '9:16'] };
    const form = { ...blankForm(both), forWhom: 'A', format: '9:16' as const, autoplay: true };
    expect(inputOf(form, both).options).toMatchObject({
      deck: 'video',
      format: '9:16',
      autoplay: true,
      tlComments: false,
    });
    expect(inputOf({ ...form, format: '16:9' }, one).options).toMatchObject({ format: '9:16' });
    expect(inputOf({ ...form, rights: 'comment' }, both).options).toMatchObject({ tlComments: true });
  });

  it('a changed link keeps its end unless the term is touched', () => {
    const form = formOf(link({ options: { device: 'ip14' } }), design);
    expect(form.device).toBe('phone');
    expect(changeOf(form, design)).not.toHaveProperty('ttl');
    expect(changeOf({ ...form, term: '30d', termTouched: true }, design)).toMatchObject({ ttl: '30d' });
    expect(changeOf({ ...form, term: null, termTouched: true }, design)).toMatchObject({ ttl: null });
  });
});

describe('the line of a link', () => {
  it('says what it allows, until when and whether it was opened', () => {
    const line = linkSub(
      link({ opens: 2, lastOpenedAt: new Date(NOW - 86_400_000).toISOString() }),
      LINK_KIND.design,
      NOW,
    );
    expect(line).toBe(
      [
        t('next.access.links.rights.view'),
        t('next.access.links.until', { date: shortDay(new Date('2026-11-11T12:00:00').toISOString()) ?? '' }),
        t('next.access.links.seenYesterday'),
      ].join(' · '),
    );
  });

  it('a document without copying says so; a downloading one names its file', () => {
    const line = linkSub(link({ options: { download: true, copy: false } }), LINK_KIND.doc, NOW);
    expect(line).toContain('PDF');
    expect(line).toContain(t('next.access.links.noCopy'));
  });

  it('a switched off link says only that and when it was opened', () => {
    const off = link({
      disabledAt: new Date(NOW - 1000).toISOString(),
      opens: 1,
      lastOpenedAt: new Date(NOW).toISOString(),
    });
    expect(linkSub(off, LINK_KIND.design, NOW)).toBe(
      [t('next.access.links.off'), t('next.access.links.seenToday')].join(' · '),
    );
  });

  it('never opened, opened on a day', () => {
    expect(seenLabel(link(), NOW)).toBe(t('next.access.links.seenNever'));
    expect(seenLabel(link({ opens: 1, lastOpenedAt: '2026-10-28T09:00:00' }), NOW)).toBe(
      t('next.access.links.seenOn', { date: shortDay('2026-10-28T09:00:00') ?? '' }),
    );
  });
});
