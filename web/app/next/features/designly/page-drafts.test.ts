import { describe, expect, it } from 'vitest';
import { withPageDraft } from '@/next/features/designly/page-drafts';
import { NO_EDIT } from '@/next/features/designly/element-style';
import type { INoteDraft } from '@/next/features/designly/site-notes';

const draft = (text: string) =>
  ({ at: { x: 1, y: 2, right: false }, text, style: NO_EDIT }) as unknown as INoteDraft;

describe('a page’s card', () => {
  it('is kept for its page alone', () => {
    const a = withPageDraft({}, 'home.html', draft('a'));
    const b = withPageDraft(a, 'offer.html', draft('b'));
    expect(b['home.html']?.text).toBe('a');
    expect(b['offer.html']?.text).toBe('b');
  });

  it('is dropped without touching the other pages', () => {
    const both = withPageDraft(withPageDraft({}, 'home.html', draft('a')), 'offer.html', draft('b'));
    const left = withPageDraft(both, 'home.html', null);
    expect(Object.keys(left)).toEqual(['offer.html']);
  });
});
