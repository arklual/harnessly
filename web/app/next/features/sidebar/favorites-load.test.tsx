// @vitest-environment jsdom
// «Избранное» on a page load: the list and the layout come in pieces (the browser's copy, the cluster's list,
// the person's layout, each machine's own list), and the favorites must be drawn once, in their final order, without
// sliding; only the person's own change slides them.
import { cleanup, render } from '@testing-library/react';
import { createRef, type ReactNode } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { PortalContext } from '@/next/ui/portal';
import Sidebar from '@/next/features/sidebar/sidebar';
import FavoritesSection, { GROUP_STATE } from '@/next/features/sidebar/favorites-section';
import { favoritesOf, favRowCount } from '@/next/features/sidebar/favorites';
import { favoritesLayoutKnown, type IFavoritesSources } from '@/next/bindings/shell-model';
import {
  CHAT_STATUS,
  NO_FAVORITES,
  SIDEBAR_VIEW,
  type ISidebarChat,
  type ISidebarFavorites,
} from '@/next/features/sidebar/types';

afterEach(cleanup);

const animate = vi.fn();
beforeAll(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  // jsdom lays nothing out: a row stands at its place among the rows that FLIP follows, 32 px each.
  Element.prototype.getBoundingClientRect = function (this: Element) {
    const i = [...document.querySelectorAll('[data-flip]')].indexOf(this);
    const top = Math.max(i, 0) * 32;
    return { top, bottom: top + 32, left: 0, right: 200, width: 200, height: 32, x: 0, y: top } as DOMRect;
  };
  Element.prototype.animate = animate as unknown as Element['animate'];
});
beforeEach(() => {
  animate.mockClear();
  localStorage.clear();
});

const chat = (id: string, pinned: boolean, createdAt: number, activity = createdAt): ISidebarChat => ({
  id,
  title: `Чат ${id}`,
  status: CHAT_STATUS.none,
  pinned,
  projectId: null,
  createdAt,
  lastPromptAt: activity,
  lastActivityAt: activity,
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

/** The favorites as the page shows them: each group with its chats, then the loose; empty while none are drawn. */
const shown = (): string =>
  [...document.querySelectorAll('.hx-favs .hx-favgroup, .hx-favs [data-chat]')]
    .map((el) =>
      el instanceof HTMLElement && el.dataset.chat ? el.dataset.chat : `[${el.getAttribute('data-drop-id')}]`,
    )
    .join(' ');

// A person with two machines: m1 is the one the page goes through, m2's chats come with the cluster's list.
const A = chat('a', true, 1);
const B = chat('b', true, 2);
const C = chat('c', true, 3);
const D = chat('d', false, 4);
const MACHINE_LAYOUT: ISidebarFavorites = { groups: [], loose: ['a', 'b'] };
const PERSON_LAYOUT: ISidebarFavorites = {
  groups: [{ id: 'g1', name: 'Работа', chats: ['c', 'a'] }],
  loose: ['b'],
};

interface IStep {
  chats: readonly ISidebarChat[];
  sources: IFavoritesSources;
  machineLayout: ISidebarFavorites;
  personLayout: ISidebarFavorites | null;
}

/** The order the sources come in on a real load of the hosted app (shell-model.ts and its stores). */
const LOAD: readonly IStep[] = [
  // The browser's copy of m1's list; the cluster has not answered.
  {
    chats: [A, B, D],
    sources: { unified: false, profile: 'loading', centralOn: false, hereSettled: false },
    machineLayout: NO_FAVORITES,
    personLayout: null,
  },
  // m1's own list with its layout.
  {
    chats: [A, B, D],
    sources: { unified: false, profile: 'loading', centralOn: false, hereSettled: true },
    machineLayout: MACHINE_LAYOUT,
    personLayout: null,
  },
  // The person's sidebar came a moment before the cluster's list.
  {
    chats: [A, B, D],
    sources: { unified: false, profile: 'ready', centralOn: false, hereSettled: true },
    machineLayout: MACHINE_LAYOUT,
    personLayout: PERSON_LAYOUT,
  },
  // The cluster's list: m2's chat joins, the list is the person's.
  {
    chats: [A, B, C, D],
    sources: { unified: true, profile: 'ready', centralOn: true, hereSettled: true },
    machineLayout: MACHINE_LAYOUT,
    personLayout: PERSON_LAYOUT,
  },
  // m2's own list (new records of the same chats, newer activity) and the next beat with the same layout.
  {
    chats: [chat('a', true, 1, 50), B, chat('c', true, 3, 60), D],
    sources: { unified: true, profile: 'ready', centralOn: true, hereSettled: true },
    machineLayout: MACHINE_LAYOUT,
    personLayout: { ...PERSON_LAYOUT },
  },
];

/** What shell-model.ts gives the sidebar for a step. */
const favoritesOfStep = (step: IStep): ISidebarFavorites | null => {
  if (!favoritesLayoutKnown(step.sources)) return null;
  return step.personLayout && step.sources.unified ? step.personLayout : step.machineLayout;
};

describe('favorites on a page load (HRN-1387)', () => {
  it('are drawn once, in their final order, and nothing slides', () => {
    const orders: string[] = [];
    const { rerender } = render(draw(LOAD[0]!.chats, favoritesOfStep(LOAD[0]!)));
    orders.push(shown());
    for (const step of LOAD.slice(1)) {
      rerender(draw(step.chats, favoritesOfStep(step)));
      orders.push(shown());
    }
    const drawn = orders.filter((order) => order !== '');
    expect(new Set(drawn)).toEqual(new Set(['[g1] c a b']));
    expect(animate).not.toHaveBeenCalled();
    expect(document.querySelector('.hx-favs .hx-slot-in')).toBeNull();
  });

  it('show a skeleton while the layout has not come', () => {
    render(draw(LOAD[0]!.chats, null));
    expect(document.querySelector('[data-favorites-wait]')).not.toBeNull();
    expect(document.querySelector('.hx-favs')).toBeNull();
  });

  it('wait for the person’s layout, the cluster’s list, or the machine’s own list', () => {
    expect(
      favoritesLayoutKnown({ unified: true, profile: 'loading', centralOn: true, hereSettled: true }),
    ).toBe(false);
    expect(
      favoritesLayoutKnown({ unified: false, profile: 'ready', centralOn: false, hereSettled: true }),
    ).toBe(false);
    expect(
      favoritesLayoutKnown({ unified: false, profile: 'off', centralOn: false, hereSettled: false }),
    ).toBe(false);
    expect(
      favoritesLayoutKnown({ unified: true, profile: 'ready', centralOn: true, hereSettled: false }),
    ).toBe(true);
    // A machine the person administers: its own layout, once it answered.
    expect(
      favoritesLayoutKnown({ unified: false, profile: 'ready', centralOn: true, hereSettled: true }),
    ).toBe(true);
    // No copy at all (a machine on its own).
    expect(
      favoritesLayoutKnown({ unified: false, profile: 'off', centralOn: false, hereSettled: true }),
    ).toBe(true);
  });
});

/** The rows the favorites take under their label: group heads, chats, notes of empty groups and the hint. */
const listRows = (): number =>
  document.querySelectorAll('.hx-favs .hx-favgroup, .hx-favs [data-chat], .hx-favs .hx-snote').length;
const skeletonRows = (): number => document.querySelectorAll('[data-favorites-wait-row]').length;

describe('the skeleton of the favorites is as tall as the list that replaces it (HRN-1387)', () => {
  const FINAL = LOAD[LOAD.length - 1]!;

  it('takes the rows of the last load in this browser', () => {
    // First load in this browser: nothing kept, a row for each pinned chat it knows.
    const first = render(draw(LOAD[0]!.chats, null));
    expect(skeletonRows()).toBe(2);
    first.rerender(draw(FINAL.chats, PERSON_LAYOUT));
    const drawn = listRows();
    expect(drawn).toBe(4);
    first.unmount();

    // The next load: the skeleton has as many rows as the list then draws, so nothing under it moves.
    const next = render(draw(LOAD[0]!.chats, null));
    expect(skeletonRows()).toBe(drawn);
    next.rerender(draw(FINAL.chats, PERSON_LAYOUT));
    expect(document.querySelector('[data-favorites-wait]')).toBeNull();
    expect(listRows()).toBe(drawn);
  });

  it('no skeleton where the last load showed no favorites, even with a chat pinned since', () => {
    localStorage.setItem('hx-next:favorites-rows', '0');
    render(draw([chat('x', false, 1), B], null));
    expect(document.querySelector('[data-favorites-wait]')).toBeNull();
  });

  it('counts a group head, its chats or its empty note, and the loose chats', () => {
    const view = favoritesOf([A, B, C], {
      groups: [
        { id: 'g1', name: 'Работа', chats: ['a', 'c'] },
        { id: 'g2', name: 'Пусто', chats: [] },
      ],
      loose: ['b'],
    });
    expect(favRowCount(view)).toBe(1 + 2 + 1 + 1 + 1);
    expect(favRowCount(favoritesOf([], NO_FAVORITES))).toBe(1);
  });
});

describe('favorites move with motion only on the person’s word (HRN-1387)', () => {
  const chats = [A, B, C];
  const section = (layout: ISidebarFavorites, moving: { current: boolean }) => (
    <FavoritesSection
      view={favoritesOf(chats, layout)}
      actions={{}}
      renderRow={(c) => <div key={c.id} data-chat={c.id} data-flip={c.id} />}
      folded={new Set()}
      onToggle={() => undefined}
      creating={GROUP_STATE.off}
      onCreateStart={() => undefined}
      onCreateEnd={() => undefined}
      renaming={null}
      onRenameStart={() => undefined}
      onRenameEnd={() => undefined}
      onDelete={() => undefined}
      onShift={() => undefined}
      pending={new Map()}
      movingRef={moving}
    />
  );

  it('a layout changed elsewhere places the rows without motion', () => {
    const moving = { current: false };
    const { rerender } = render(section({ groups: [], loose: ['a', 'b', 'c'] }, moving));
    rerender(section({ groups: [], loose: ['c', 'a', 'b'] }, moving));
    expect(animate).not.toHaveBeenCalled();
  });

  it('the person’s move slides once, and the next change elsewhere does not', () => {
    const moving = { current: false };
    const { rerender } = render(section({ groups: [], loose: ['a', 'b', 'c'] }, moving));
    moving.current = true;
    rerender(section({ groups: [], loose: ['b', 'a', 'c'] }, moving));
    expect(animate).toHaveBeenCalledTimes(2);
    expect(moving.current).toBe(false);
    rerender(section({ groups: [], loose: ['c', 'b', 'a'] }, moving));
    expect(animate).toHaveBeenCalledTimes(2);
  });
});
