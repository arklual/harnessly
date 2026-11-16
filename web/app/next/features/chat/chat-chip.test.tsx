// @vitest-environment jsdom
// a chat the agent's answer links to opens that chat, as its row in the sidebar does; a chat that is gone is a
// quiet chip saying so, and no red «nothing to open».
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/platform/config', () => ({
  appConfig: () => ({ taskKeyPattern: '[A-Z][A-Z0-9]{1,9}-\\d{1,6}' }),
}));

import Markdown from '@/next/features/chat/markdown';
import { RefContext } from '@/next/features/chat/ref-chip';
import { conversationsStore, convsLoadedStore } from '@/next/stores/conversations';
import type { ConvId, Conversation, MachineId } from '@/next/protocol/v2';
import { pinLocale } from '@/next/i18n';

const CHAT = 'tab-vh0l85vgd' as ConvId;
const MACHINE = 'm1' as MachineId;

const draw = (md: string) => {
  const onOpen = vi.fn();
  render(
    <RefContext.Provider value={{ refs: {}, onOpen }}>
      <Markdown md={md} />
    </RefContext.Provider>,
  );
  return onOpen;
};

beforeEach(() => {
  pinLocale('ru');
  convsLoadedStore.set(MACHINE, true);
});

afterEach(() => {
  cleanup();
  conversationsStore.delete(CHAT);
  convsLoadedStore.delete(MACHINE);
});

describe('a chat the answer names', () => {
  it('a link to its id opens the chat, under the chat’s own title', () => {
    conversationsStore.set(CHAT, { id: CHAT, title: 'Ресерч коннекторов' } as unknown as Conversation);
    const onOpen = draw(`Запустил исследование в подчате [«Ресерч»](${CHAT}).`);
    const chip = screen.getByRole('button', { name: /Ресерч коннекторов/ });
    fireEvent.click(chip);
    expect(onOpen).toHaveBeenCalledWith('chat', CHAT, null);
  });

  it('a bare id of a known chat is its chip too', () => {
    conversationsStore.set(CHAT, { id: CHAT, title: 'Ресерч коннекторов' } as unknown as Conversation);
    draw(`Запустил ${CHAT}.`);
    expect(screen.getByRole('button', { name: /Ресерч коннекторов/ })).toBeTruthy();
  });

  it('a chat that is gone is a quiet chip that opens nothing', () => {
    const onOpen = draw(`Запустил [«Ресерч»](${CHAT}).`);
    const chip = screen.getByTitle('Чат удалён');
    fireEvent.click(chip);
    expect(onOpen).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: /Ресерч/ })).toBeNull();
  });

  it('a bare id of no chat stays a word', () => {
    draw(`Запустил ${CHAT}.`);
    expect(screen.queryByTitle('Чат удалён')).toBeNull();
    expect(screen.getByText(/tab-vh0l85vgd/)).toBeTruthy();
  });
});
