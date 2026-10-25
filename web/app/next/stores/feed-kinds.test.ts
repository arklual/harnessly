import { describe, expect, it } from 'vitest';
import { DEFAULT_FEED_KINDS, parseFeedKinds } from '@/next/stores/feed-kinds';

describe('what the feed shows', () => {
  it('is everything but the plain files until the person chooses', () => {
    expect(parseFeedKinds(null)).toEqual(DEFAULT_FEED_KINDS);
    expect(DEFAULT_FEED_KINDS.file).toBe(false);
    expect(DEFAULT_FEED_KINDS.artifact).toBe(true);
  });

  it('keeps the saved choices over the defaults and drops what it does not know', () => {
    expect(parseFeedKinds('{"file":true,"mr":false,"junk":true,"doc":"yes"}')).toEqual({
      ...DEFAULT_FEED_KINDS,
      file: true,
      mr: false,
    });
    expect(parseFeedKinds('not json')).toEqual(DEFAULT_FEED_KINDS);
  });
});
