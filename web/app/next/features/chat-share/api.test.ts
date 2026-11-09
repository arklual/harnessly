import { describe, expect, it } from 'vitest';
import { readDraft, readShare } from '@/next/features/chat-share/api';

describe('chat-share api', () => {
  it('reads a draft and sums the hidden secrets', () => {
    const d = readDraft({
      draft: { id: 'd1', title: 'T', report: { redactions: { key: 2, token: 1 }, pathsShortened: 4 } },
    });
    expect(d).toEqual({ id: 'd1', title: 'T', hidden: 3, shortened: 4 });
  });

  it('tolerates a draft without a report', () => {
    expect(readDraft({ draft: { id: 'd2' } })).toEqual({ id: 'd2', title: '', hidden: 0, shortened: 0 });
  });

  it('refuses an answer without a draft id', () => {
    expect(() => readDraft({ draft: {} })).toThrow();
    expect(() => readDraft(null)).toThrow();
  });

  it('reads a share and refuses one without a url', () => {
    expect(readShare({ share: { id: 's', url: 'https://x/s/1', expiresAt: 5, views: 2 } })).toMatchObject({
      id: 's',
      views: 2,
    });
    expect(() => readShare({ share: { id: 's' } })).toThrow();
  });
});
