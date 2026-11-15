// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { cardChatOf, chatOf, itemChatOf, leadsTo } from '@/next/features/day/chat-link';

const HUB = 'https://hub.example.com';

describe("the agent's chat a thing names", () => {
  it('takes the id of the conversation', () => {
    expect(chatOf({ chat: 'tab-abc' }, HUB)).toBe('tab-abc');
    expect(chatOf({ run: { chat: 'tab-run' } }, HUB)).toBe('tab-run');
  });

  it('takes the address of a chat of this hub', () => {
    expect(chatOf({ run: { url: `${HUB}/c/tab-abc?pane=1` } }, HUB, '/')).toBe('tab-abc');
    expect(chatOf({ url: '/c/tab-xyz' }, HUB, '/')).toBe('tab-xyz');
  });

  it("takes the hub's address of a machine's chat only on that machine's page", () => {
    const url = `${HUB}/?c=${encodeURIComponent('agent:m1:tab-abc')}`;
    expect(chatOf({ url }, HUB, '/m/m1/')).toBe('tab-abc');
    expect(chatOf({ url }, HUB, '/m/m2/')).toBeNull();
  });

  it('opens nothing for another hub, another page or a broken name', () => {
    expect(chatOf({ url: 'https://other.example.com/c/tab-abc' }, HUB, '/')).toBeNull();
    expect(chatOf({ url: `${HUB}/settings` }, HUB, '/')).toBeNull();
    expect(chatOf({ chat: 'not a chat' }, HUB)).toBeNull();
    expect(chatOf({}, HUB, '/')).toBeNull();
  });
});

describe("the agent's chat an opened thing shows inside it", () => {
  const onM1 = (tab: string) => `${HUB}/agents/?c=${encodeURIComponent(`agent:m1:${tab}`)}`;

  it("shows the hub's chat of the page's machine, from any host", () => {
    expect(itemChatOf({ url: onM1('tab-abc') }, 'm1', 'https://m1.example.com')).toEqual({
      conv: 'tab-abc',
      machine: 'm1',
    });
    expect(itemChatOf({ url: onM1('tab-abc') }, 'm2', HUB)).toBeNull();
    expect(itemChatOf({ chat: 'tab-id' }, 'm2', HUB)).toEqual({ conv: 'tab-id', machine: 'm2' });
  });

  it('finds the chat of a card in its run, its material or a chip that opens it', () => {
    const card = { itemId: 1, title: 'x' };
    expect(cardChatOf({ ...card, run: { url: onM1('tab-run') } }, 'm1', HUB)?.conv).toBe('tab-run');
    expect(cardChatOf({ ...card, go: { title: 'g', url: onM1('tab-go') } }, 'm1', HUB)?.conv).toBe('tab-go');
    expect(
      cardChatOf({ ...card, chips: [{ label: 'c', kind: 'open', url: onM1('tab-chip') }] }, 'm1', HUB)?.conv,
    ).toBe('tab-chip');
    expect(
      cardChatOf({ ...card, go: { title: 'g', url: 'https://tracker.example.com/T-1' } }, 'm1', HUB),
    ).toBeNull();
  });

  it("takes the hub's address of a chat on the machine's own page, where the machine is local", () => {
    const meta = document.createElement('meta');
    meta.name = 'api-base';
    meta.content = '/m/m1';
    document.head.append(meta);
    try {
      expect(itemChatOf({ url: onM1('tab-own') }, 'local', HUB)).toEqual({
        conv: 'tab-own',
        machine: 'local',
      });
      expect(itemChatOf({ url: HUB + '/agents/?c=agent:m2:tab-x' }, 'local', HUB)).toBeNull();
    } finally {
      meta.remove();
    }
  });

  it('knows what leads to the chat already shown', () => {
    const chat = { conv: 'tab-abc', machine: 'm1' } as const;
    expect(leadsTo({ url: onM1('tab-abc') }, chat, HUB)).toBe(true);
    expect(leadsTo({ url: onM1('tab-other') }, chat, HUB)).toBe(false);
    expect(leadsTo(null, chat, HUB)).toBe(false);
  });
});
