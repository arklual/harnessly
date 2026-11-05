import { describe, expect, it } from 'vitest';
import type { IGuestLink } from '@/next/features/designly/access/api';
import {
  autoplayOn,
  readVideoLinkView,
  shareFormatsOf,
  startFormat,
  timelineCommentsOn,
  videoLinkOf,
  videoViewOf,
} from '@/next/features/designly/video/share-view';

describe('the view of a video link', () => {
  it('has the video mode, the video flag, the counting and the format', () => {
    expect(videoViewOf({ stats: true, format: '9:16' })).toEqual({
      v: 1,
      deck: 'video',
      video: true,
      stats: true,
      format: '9:16',
    });
  });

  it('reads back what it wrote, and nothing from a deck link', () => {
    expect(readVideoLinkView(videoViewOf({ stats: false, format: '16:9' }))).toEqual({
      stats: false,
      format: '16:9',
    });
    expect(readVideoLinkView({ v: 1, deck: 'slides', pdf: true, video: true, stats: true })).toBeNull();
    expect(readVideoLinkView(null)).toBeNull();
    expect(readVideoLinkView({ deck: 'video', format: '4:3' })).toEqual({ stats: false, format: null });
  });

  it('starts in the link format when it is published, else 16:9, else what there is', () => {
    const view = videoViewOf({ stats: true, format: '9:16' });
    expect(startFormat(view, ['16:9', '9:16'])).toBe('9:16');
    expect(startFormat(view, ['16:9'])).toBe('16:9');
    expect(startFormat(view, ['9:16'])).toBe('9:16');
    expect(startFormat(null, ['9:16', '16:9'])).toBe('16:9');
    expect(startFormat(view, [])).toBeNull();
  });
});

const linkWith = (id: string, view: Record<string, unknown> | null): IGuestLink => ({
  id,
  title: 'T',
  rights: 'comment',
  expiresAt: null,
  revokedAt: null,
  opens: 0,
  createdAt: '2026-10-28T10:00:00Z',
  url: `https://s.test/${id}`,
  comments: 0,
  view,
});

describe('the video link of a project', () => {
  it('is the first live link whose view is a video one', () => {
    const live = [
      linkWith('a', null),
      linkWith('b', { v: 1, deck: 'slides' }),
      linkWith('c', { ...videoViewOf({ stats: false, format: '16:9' }) }),
    ];
    expect(videoLinkOf(live)?.id).toBe('c');
    expect(videoLinkOf(live.slice(0, 2))).toBeNull();
  });
});

describe('the formats of the share popover', () => {
  it('keeps the order and is null for none', () => {
    expect(shareFormatsOf(['16:9', '9:16'])).toEqual(['16:9', '9:16']);
    expect(shareFormatsOf([])).toBeNull();
  });
});

describe('what a video link of HRN-675 asks of the guest page', () => {
  it('starts by itself only when asked; comments stay on the timeline unless turned off', () => {
    expect(autoplayOn({ deck: 'video', autoplay: true })).toBe(true);
    expect(autoplayOn({ deck: 'video' })).toBe(false);
    expect(autoplayOn(null)).toBe(false);
    expect(timelineCommentsOn({ deck: 'video', tlComments: false })).toBe(false);
    expect(timelineCommentsOn({ deck: 'video' })).toBe(true);
    expect(timelineCommentsOn(null)).toBe(true);
  });
});
