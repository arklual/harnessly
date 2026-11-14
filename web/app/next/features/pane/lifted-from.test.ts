import { afterEach, describe, expect, it } from 'vitest';
import { liftedFrom, liftedFromLegacy, markLifted, resetLifted } from '@/next/features/pane/lifted-from';
import { liftedTitle, parseIntents } from '@/next/features/pane/intents';

afterEach(resetLifted);

describe('a tab a subchat opened (HRN-1451)', () => {
  it('keeps the subchat that asked, by what the tab shows, until the chat asks for it itself', () => {
    markLifted('tab-root', { k: 'design', target: 'shop', reload: false }, 'Правка доски');
    expect(liftedFrom('tab-root', { id: 't1', kind: 'designly', design: 'shop' })).toBe('Правка доски');
    expect(liftedFrom('tab-other', { id: 't1', kind: 'designly', design: 'shop' })).toBeNull();
    markLifted('tab-root', { k: 'design', target: 'shop', reload: false }, null);
    expect(liftedFrom('tab-root', { id: 't1', kind: 'designly', design: 'shop' })).toBeNull();
  });

  it('reads the subchat of a lifted intent and refuses a malformed one', () => {
    const intents = parseIntents({
      intents: [
        {
          id: 'a',
          createdAt: 1,
          appliedAt: null,
          appliedOn: null,
          command: {
            kind: 'open',
            tab: 'design',
            target: 'shop',
            auto: true,
            from: { id: 'tab-kid', title: 'Правка' },
          },
        },
        {
          id: 'b',
          createdAt: 1,
          appliedAt: null,
          appliedOn: null,
          command: { kind: 'open', tab: 'files', from: 'x' },
        },
      ],
    });
    expect(intents.map((x) => x.id)).toEqual(['a']);
    expect(liftedTitle(intents[0]!.command)).toBe('Правка');
  });
});

describe('a tab of Legacy’s pane a subchat opened', () => {
  it('is marked by its kind and target', () => {
    markLifted(
      'tab-root',
      {
        k: 'legacy',
        kind: 'gitlab',
        target: 'https://git.example.com/a/b/-/merge_requests/1',
        reload: false,
      },
      'Kid',
    );
    expect(liftedFromLegacy('tab-root', 'gitlab', 'https://git.example.com/a/b/-/merge_requests/1')).toBe(
      'Kid',
    );
    expect(liftedFromLegacy('tab-root', 'task', 'AB-1')).toBeNull();
  });
});
