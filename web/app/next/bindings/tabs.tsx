// The tabs over the window on the router and the stores: the address follows the active tab and the active tab follows
// the address, so every way of going somewhere (the sidebar, a link, Back) moves the active tab, and a tab switch is
// a move of the address. Each tab leaves with the scroll of its page, and gets it back. The shortcuts are a browser's:
// ⌘T, ⌘W, ⌘1…9, Ctrl+Tab.
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, type ReactNode } from 'react';
import Router from '@/platform/router';
import TabStrip, { PACE, type IStripTab, type TPace } from '@/next/features/shell/tab-strip';
import { STATUS_LOOK } from '@/next/features/sidebar/status';
import { CHAT_STATUS } from '@/next/features/sidebar/types';
import type { ITabChat } from '@/next/bindings/shell-model';
import { t } from '@/next/i18n';
import { goTo, parseRoute, ROUTE, routePath, useRoute, type TRoute } from '@/next/routes/routes';
import {
  activeTab,
  OPEN,
  planOpen,
  startTabs,
  tabs as tabsStore,
  tabsSnapshot,
  useTabs,
  type ITab,
} from '@/next/stores/tabs';

/** The scrollers of the screens whose place a tab keeps: the feed, Settings, Designly's library, Apps. */
const SCROLLERS = ['.hx-feed', '.hx-set__page', '.hx-dl', '.hx-apps'] as const;
/** A screen that comes back is waited for this long to have its scroller (a chat leaves first, then comes in). */
const RESTORE_MS = 1500;
const WAITING = new Set<string>([CHAT_STATUS.question, CHAT_STATUS.perm, CHAT_STATUS.plan]);

/** A chat at work turns its icon: quickly for a turn, slowly while only its background agents run (as the sidebar). */
const paceOf = (status: string): TPace | null => {
  if (status === CHAT_STATUS.thinking) return PACE.working;
  if (status === CHAT_STATUS.background) return PACE.background;
  return null;
};

const FIXED: Record<Exclude<TRoute['kind'], 'chat' | 'site'>, { icon: string; title: () => string }> = {
  [ROUTE.home]: { icon: 'house', title: () => t('next.tabs.home') },
  [ROUTE.designly]: { icon: 'palette', title: () => t('next.tabs.designly') },
  [ROUTE.designlyNew]: { icon: 'presentation', title: () => t('next.deck.create.title') },
  [ROUTE.invite]: { icon: 'palette', title: () => t('next.tabs.designly') },
  [ROUTE.apps]: { icon: 'app-window', title: () => t('next.sidebar.apps') },
  [ROUTE.settings]: { icon: 'settings', title: () => t('next.shell.settings') },
  [ROUTE.stats]: { icon: 'chart-column', title: () => t('next.sidebar.stats') },
  [ROUTE.efficiency]: { icon: 'gauge', title: () => t('next.sidebar.efficiency') },
};

/** What the strip shows for a tab: its place's icon and name, a chat's status. */
export const stripTabOf = (tab: ITab, chats: ReadonlyMap<string, ITabChat>): IStripTab => {
  const route = parseRoute(tab.path);
  if (route.kind === ROUTE.chat) {
    const chat = chats.get(route.id);
    const status = chat?.status ?? CHAT_STATUS.none;
    const look = status === CHAT_STATUS.none ? null : STATUS_LOOK[status];
    return {
      id: tab.id,
      title: chat?.title ?? tab.title ?? t('next.tabs.chat'),
      icon: look?.icon ?? 'message-square',
      busy: paceOf(status),
      ...(chat?.waitsFor ? { note: chat.waitsFor } : {}),
      attention: WAITING.has(status),
    };
  }
  if (route.kind === ROUTE.site)
    return { id: tab.id, title: tab.title ?? route.id, icon: 'pen-tool', busy: null, attention: false };
  const fixed = FIXED[route.kind];
  return { id: tab.id, title: fixed.title(), icon: fixed.icon, busy: null, attention: false };
};

/** The scroll of the screen on view, by scroller. */
const readScroll = (): Record<string, number> | null => {
  const body = document.querySelector('.hx-main__body');
  if (!body) return null;
  const out: Record<string, number> = {};
  for (const selector of SCROLLERS) {
    const el = body.querySelector<HTMLElement>(selector);
    if (el && el.scrollTop > 0) out[selector] = Math.round(el.scrollTop);
  }
  return Object.keys(out).length > 0 ? out : null;
};

/**
 * Gives the tab's screen its scroll back once it is there: a scroller other than the one left (the leaving chat stays
 * a moment), tall enough to take it. Gives up after RESTORE_MS. Returns the stop.
 */
const restoreScroll = (
  scroll: Readonly<Record<string, number>>,
  left: ReadonlySet<Element>,
): (() => void) => {
  const until = performance.now() + RESTORE_MS;
  const pending = new Map(Object.entries(scroll));
  let frame = 0;
  const step = () => {
    const body = document.querySelector('.hx-main__body');
    for (const [selector, top] of pending) {
      const el = body?.querySelector<HTMLElement>(selector);
      if (!el || left.has(el) || el.scrollHeight - el.clientHeight < top) continue;
      el.scrollTop = top;
      pending.delete(selector);
    }
    if (pending.size > 0 && performance.now() < until) frame = requestAnimationFrame(step);
  };
  frame = requestAnimationFrame(step);
  return () => cancelAnimationFrame(frame);
};

const scrollersNow = (): Set<Element> => {
  const body = document.querySelector('.hx-main__body');
  return new Set(SCROLLERS.flatMap((selector) => Array.from(body?.querySelectorAll(selector) ?? [])));
};

/** A press with ⌘ (Ctrl) or the middle button asks for a new tab: read from the click on its way in. */
let newTabClick = false;
const onClickCapture = (event: MouseEvent) => {
  newTabClick = (event.metaKey || event.ctrlKey) && !event.altKey;
  if (newTabClick) setTimeout(() => (newTabClick = false));
};

/** The click being handled asked for a new tab (⌘-click). */
export const wantsNewTab = (): boolean => newTabClick;

export interface IShellTabs {
  /** The strip, or null where there is none (switched off, phone). */
  strip: ReactNode;
  /** The active tab's id (a view that keeps a tab's state is drawn anew for another tab). */
  activeId: string;
  /** Whether the strip is on: opening a place then keeps the tabs; without it the single view goes there. */
  enabled: boolean;
  /** Opens `route` from a menu: its tab if open, else a new tab after the active one (behind it for a ⌘-click). */
  navigate: (route: TRoute) => void;
  /** Opens `route` in a new tab (`background`: behind the active one). */
  openInNewTab: (route: TRoute, background?: boolean) => void;
}

/** `byId`: the chats of every machine (useTabChats), so a tab of a machine other than the open one keeps its status. */
export const useShellTabs = (byId: ReadonlyMap<string, ITabChat>, enabled: boolean): IShellTabs => {
  const route = useRoute();
  const path = routePath(route);
  const state = useTabs();
  const active = activeTab(state);

  // Once: the kept tabs come back for this address; a bare `/` returns to the tab the person was in.
  useLayoutEffect(() => {
    const search = new URLSearchParams(window.location.search);
    const go = startTabs(routePath(parseRoute(Router.asPath)), enabled && !search.has('c'));
    if (go) goTo(parseRoute(go), { replace: true });
    const flush = () => tabsStore.flush();
    window.addEventListener('pagehide', flush);
    document.addEventListener('click', onClickCapture, true);
    return () => {
      window.removeEventListener('pagehide', flush);
      document.removeEventListener('click', onClickCapture, true);
    };
    // The start is the page's first address only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // The address moved (the sidebar, a link, Back): the active tab goes with it. A switch has moved the active tab
  // first, so its own address arriving changes nothing. The address is read as it is now: the start may have moved it.
  useEffect(() => {
    tabsStore.navigate(routePath(parseRoute(Router.asPath)));
  }, [path]);

  // The scroll comes back when a tab is switched to.
  const restoring = useRef<(() => void) | null>(null);
  const switchTo = useCallback((id: string, close = false) => {
    const before = tabsSnapshot();
    const leaving = activeTab(before);
    if (!close && leaving.id === id) return;
    const left = scrollersNow();
    if (!close) tabsStore.patch(leaving.id, { scroll: readScroll() });
    if (close) tabsStore.close(id);
    else tabsStore.activate(id);
    const target = activeTab(tabsSnapshot());
    if (target.id === leaving.id) return;
    restoring.current?.();
    restoring.current = target.scroll
      ? restoreScroll(target.scroll, target.path === leaving.path ? new Set() : left)
      : null;
    const current = routePath(parseRoute(Router.asPath));
    if (target.path !== current) goTo(parseRoute(target.path), { replace: true });
  }, []);

  const open = useCallback((next: TRoute, background = false, at: 'next' | 'end' = 'next') => {
    const leaving = activeTab(tabsSnapshot());
    tabsStore.patch(leaving.id, { scroll: readScroll() });
    tabsStore.open(routePath(next), { background, at });
    if (!background) goTo(next, { replace: true });
  }, []);

  // A place opened from a menu (sidebar, search, notifications) never replaces what the active tab shows: the tab that
  // has it is switched to, else a new one opens after the active tab. Where there is no strip (phone, switched off) the
  // single view goes there, as there is no tab to close.
  const navigate = useCallback(
    (next: TRoute) => {
      const background = newTabClick;
      newTabClick = false;
      if (!enabled) {
        goTo(next);
        return;
      }
      const target = routePath(next);
      const now = tabsSnapshot();
      const plan = planOpen(now, target, { background });
      if (plan.did === OPEN.create) {
        open(next, background);
        return;
      }
      // The place is open already; a tab shows it as asked (another board, another section).
      for (const tab of plan.state.tabs) tabsStore.patch(tab.id, { path: tab.path });
      if (plan.did === OPEN.focus) switchTo(plan.state.active);
      else if (!background) goTo(parseRoute(activeTab(tabsSnapshot()).path), { replace: true });
    },
    [enabled, open, switchTo],
  );

  const close = useCallback(
    (id: string) => {
      if (tabsSnapshot().active === id || tabsSnapshot().tabs.length === 1) switchTo(id, true);
      else tabsStore.close(id);
    },
    [switchTo],
  );

  // The tabs that stay after «close others» / «close right» include the one the menu is of: the window moves to it
  // first when the one on view is among those that go, so its scroll and address are handled as for any switch.
  const closeOthers = useCallback(
    (id: string) => {
      if (tabsSnapshot().active !== id) switchTo(id);
      tabsStore.closeOthers(id);
    },
    [switchTo],
  );

  const closeRight = useCallback(
    (id: string) => {
      const now = tabsSnapshot();
      const at = now.tabs.findIndex((tab) => tab.id === id);
      const goes = now.tabs.slice(at + 1).some((tab) => tab.id === now.active);
      if (goes) switchTo(id);
      tabsStore.closeRight(id);
    },
    [switchTo],
  );

  const newTab = useCallback(() => open({ kind: ROUTE.home }, false, 'end'), [open]);

  // The browser's shortcuts. In a plain browser tab Chrome keeps ⌘T, ⌘W and Ctrl+Tab for itself; in the installed
  // app's window they come here.
  useEffect(() => {
    if (!enabled) return;
    const onKey = (event: KeyboardEvent) => {
      const mod = (event.metaKey || event.ctrlKey) && !event.altKey;
      const key = event.key.toLowerCase();
      const now = tabsSnapshot();
      if (event.ctrlKey && key === 'tab') {
        event.preventDefault();
        const at = now.tabs.findIndex((tab) => tab.id === now.active);
        const n = now.tabs.length;
        switchTo(now.tabs[(((at + (event.shiftKey ? -1 : 1)) % n) + n) % n].id);
      } else if (mod && !event.shiftKey && key === 't') {
        event.preventDefault();
        newTab();
      } else if (mod && !event.shiftKey && key === 'w') {
        event.preventDefault();
        close(now.active);
      } else if (mod && !event.shiftKey && /^[1-9]$/.test(key)) {
        event.preventDefault();
        const digit = Number(key);
        const tab = digit === 9 ? now.tabs[now.tabs.length - 1] : now.tabs[digit - 1];
        if (tab) switchTo(tab.id);
      }
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [enabled, switchTo, newTab, close]);

  // A chat's tab keeps its last title, to draw it before the list comes after a reload.
  useEffect(() => {
    for (const tab of state.tabs) {
      const r = parseRoute(tab.path);
      const title = r.kind === ROUTE.chat ? byId.get(r.id)?.title : undefined;
      if (title && title !== tab.title) tabsStore.patch(tab.id, { title });
    }
  }, [byId, state.tabs]);

  const shown = useMemo(() => state.tabs.map((tab) => stripTabOf(tab, byId)), [state.tabs, byId]);

  return {
    strip: enabled ? (
      <TabStrip
        tabs={shown}
        active={active.id}
        onActivate={switchTo}
        onClose={close}
        onCloseOthers={closeOthers}
        onCloseRight={closeRight}
        onNew={newTab}
        onMove={tabsStore.move}
      />
    ) : null,
    activeId: active.id,
    enabled,
    navigate,
    openInNewTab: open,
  };
};
