// @vitest-environment jsdom
// Sergey points at a chat in the sidebar and the list moves under the pointer: a chat written to from another
// device, or a new one, comes in above it, and the click opens another chat. While the pointer is in the list, the rows
// hold their places; what changed is put in when it leaves.
import { cleanup, fireEvent, render } from '@testing-library/react';
import { createRef, type ReactNode } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import Sidebar from '@/next/features/sidebar/sidebar';
import { holdSections, sectionsOf, sidebarLayout } from '@/next/features/sidebar/group';
import {
  CHAT_STATUS,
  NO_FAVORITES,
  SIDEBAR_VIEW,
  type ISidebarChat,
  type ISidebarFavorites,
} from '@/next/features/sidebar/types';

afterEach(cleanup);

beforeAll(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  Element.prototype.animate = vi.fn() as unknown as Element['animate'];
});

const chat = (id: string, prompt: number): ISidebarChat => ({
  id,
  title: `Чат ${id}`,
  status: CHAT_STATUS.none,
  pinned: false,
  projectId: null,
  createdAt: prompt,
  lastPromptAt: prompt,
  lastActivityAt: prompt,
  terminal: true,
  canStopTerminal: true,
});

const host = createRef<HTMLDivElement>();
const draw = (chats: readonly ISidebarChat[] | null, favorites: ISidebarFavorites | null): ReactNode => (
  <PortalContext value={host}>
    <div ref={host} />
    <Sidebar
      brand="harnessly"
      chats={chats}
      projects={[]}
      favorites={favorites}
      loadError={null}
      currentId={null}
      view={SIDEBAR_VIEW.chat}
      now={10_000}
      person={null}
      machine={null}
      otherMachines={[]}
      unread={false}
      notificationsOpen={false}
      searchOpen={false}
      more={[]}
      profile={[]}
      actions={{ onPin: async () => undefined, onPlaceFavorite: async () => undefined }}
      onOpen={() => undefined}
      onNewChat={() => undefined}
      onSearch={() => undefined}
      onNotifications={() => undefined}
      onToggleRail={() => undefined}
      onCloseDrawer={() => undefined}
    />
  </PortalContext>
);

const rows = (): string =>
  [...document.querySelectorAll('.hx-sidebar__list [data-chat]')]
    .map((el) => (el as HTMLElement).dataset.chat)
    .join(' ');

describe('the sidebar holds its rows under the pointer (HRN-1446)', () => {
  it('a chat written to elsewhere and a new chat do not move the rows while the pointer is in the list', () => {
    const view = render(draw([chat('x', 3000), chat('y', 2000), chat('z', 1000)], NO_FAVORITES));
    expect(rows()).toBe('x y z');
    const list = document.querySelector('.hx-sidebar__list');
    if (!list) throw new Error('no list');
    fireEvent.pointerOver(list);
    // z is written to from the phone, w is started there.
    view.rerender(draw([chat('w', 6000), chat('x', 3000), chat('y', 2000), chat('z', 5000)], NO_FAVORITES));
    expect(rows()).toBe('x y z');
    fireEvent.pointerOut(list);
    expect(rows()).toBe('w z x y');
  });
});

describe('holdSections', () => {
  it('keeps the shown chats in their list and place, leaves out the new ones and the gone ones', () => {
    const before = sectionsOf([chat('x', 3000), chat('y', 2000), chat('z', 1000)], [], 10_000);
    const after = sectionsOf([chat('w', 6000), chat('x', 3000), chat('z', 5000)], [], 10_000);
    const held = holdSections(after, sidebarLayout(before));
    expect(held.days.flatMap((d) => d.chats.map((c) => c.id))).toEqual(['x', 'z']);
    expect(held.days[0]?.chats[1]?.lastPromptAt).toBe(5000);
  });
});
