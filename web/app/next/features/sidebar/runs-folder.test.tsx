// @vitest-environment jsdom
// The hub marks the chats of the routines' and the triggers' runs (from.app «routines»): they stand in one
// folder of the sidebar, «Регулярные», with the routines' tip, not an app's.
import { cleanup, render } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import Sidebar from '@/next/features/sidebar/sidebar';
import { CHAT_STATUS, NO_FAVORITES, SIDEBAR_VIEW, type ISidebarChat } from '@/next/features/sidebar/types';

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

const chat = (id: string, title: string, from?: ISidebarChat['from']): ISidebarChat => ({
  id,
  title,
  status: CHAT_STATUS.none,
  pinned: false,
  projectId: null,
  createdAt: 5_000,
  lastPromptAt: 5_000,
  lastActivityAt: 5_000,
  terminal: true,
  canStopTerminal: true,
  ...(from ? { from } : {}),
});

const RUNS = { app: 'routines', label: 'Регулярные' };

describe('the runs folder', () => {
  it('holds the routines’ and the triggers’ run chats in one folder', () => {
    const host = createRef<HTMLDivElement>();
    render(
      <PortalContext value={host}>
        <div ref={host} />
        <Sidebar
          brand="harnessly"
          chats={[
            chat('r1', 'Дайджест GitLab', RUNS),
            chat('t1', 'Разбор алертов', RUNS),
            chat('i1', 'Ревью', { app: 'acme.inbox', label: 'Входящие' }),
            chat('c1', 'Свой чат'),
          ]}
          projects={[]}
          favorites={NO_FAVORITES}
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
      </PortalContext>,
    );
    const folders = [...document.querySelectorAll('.hx-project__open')].map((el) => ({
      title: el.querySelector('.hx-item__title')?.textContent,
      tip: (el as HTMLElement).dataset.tip ?? '',
    }));
    const runs = folders.find((f) => f.title === 'Регулярные');
    const inbox = folders.find((f) => f.title === 'Входящие');
    expect(runs).toBeDefined();
    expect(runs?.tip).not.toContain('«Регулярные»');
    expect(inbox?.tip).toContain('Входящие');
    const group = [...document.querySelectorAll('.hx-nav__group')].find(
      (g) => g.querySelector('.hx-project__open .hx-item__title')?.textContent === 'Регулярные',
    );
    const inFolder = [...(group?.querySelectorAll('[data-chat]') ?? [])].map(
      (el) => (el as HTMLElement).dataset.chat,
    );
    expect(inFolder).toEqual(['r1', 't1']);
  });
});
