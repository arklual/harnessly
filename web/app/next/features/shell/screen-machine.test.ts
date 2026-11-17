import { describe, expect, it } from 'vitest';
import { screenChatOf, screenMachine, screenWaits } from '@/next/features/shell/screen-machine';
import { ROUTE } from '@/next/routes/routes';

describe('screen-machine: a chat does not depend on the machine active in the shell', () => {
  it('draws the design of a chat on the machine of its address', () => {
    const route = { kind: ROUTE.site, id: 'd', chat: 'tab-a', machine: 'vm2' } as const;
    expect(screenMachine(route, 'vm9', 'vm1')).toBe('vm2');
  });

  it('draws it on the machine its chat is on when the address names none, not on the active one', () => {
    const route = { kind: ROUTE.site, id: 'd', chat: 'tab-a' } as const;
    expect(screenMachine(route, 'vm2', 'vm1')).toBe('vm2');
    expect(screenMachine({ kind: ROUTE.app, id: 'a', chat: 'tab-a' }, 'vm2', 'vm1')).toBe('vm2');
  });

  it('falls back to the active machine for a screen without a chat, or while the chat is unknown', () => {
    expect(screenMachine({ kind: ROUTE.site, id: 'd' }, 'vm2', 'vm1')).toBe('vm1');
    expect(screenMachine({ kind: ROUTE.site, id: 'd', chat: 'tab-a' }, null, 'vm1')).toBe('vm1');
    expect(screenMachine({ kind: ROUTE.kit, id: 'k', chat: 'tab-a' }, 'vm2', 'vm1')).toBe('vm1');
  });

  it('names the chat of a design or app screen only', () => {
    expect(screenChatOf({ kind: ROUTE.site, id: 'd', chat: 'tab-a' })).toBe('tab-a');
    expect(screenChatOf({ kind: ROUTE.site, id: 'd' })).toBeNull();
    expect(screenChatOf({ kind: ROUTE.home })).toBeNull();
  });
});

describe('screen-machine: a review opened from a chat', () => {
  const review = {
    kind: ROUTE.review,
    url: 'https://gitlab.example.com/g/p/-/merge_requests/65',
    chat: 'tab-a',
  } as const;

  it('follows the chat of the review, wherever it runs', () => {
    expect(screenChatOf(review)).toBe('tab-a');
    expect(screenMachine(review, 'vm2', 'vm1')).toBe('vm2');
  });

  it('a review without a chat stays on the active machine', () => {
    const alone = { kind: ROUTE.review, url: review.url } as const;
    expect(screenChatOf(alone)).toBeNull();
    expect(screenWaits(alone, null)).toBe(false);
  });

  it('waits while nothing says where the chat is, instead of falling back to the active machine', () => {
    expect(screenWaits(review, null)).toBe(true);
    expect(screenWaits(review, 'vm2')).toBe(false);
    expect(screenWaits({ kind: ROUTE.app, id: 'a', chat: 'tab-a' }, null)).toBe(true);
  });

  it('does not wait when the address names the machine', () => {
    expect(screenWaits({ kind: ROUTE.site, id: 'd', chat: 'tab-a', machine: 'vm2' }, null)).toBe(false);
    expect(screenWaits({ kind: ROUTE.site, id: 'd', chat: 'tab-a' }, null)).toBe(true);
  });
});
