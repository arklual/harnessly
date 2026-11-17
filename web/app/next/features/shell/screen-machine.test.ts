import { describe, expect, it } from 'vitest';
import { screenChatOf, screenMachine } from '@/next/features/shell/screen-machine';
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
