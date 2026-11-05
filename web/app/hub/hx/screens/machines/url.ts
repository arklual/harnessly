// The open machine card lives in the address: `?m=<machine>&tab=<resources|access>&dlg=<restart|off|remove>`. Opening a card is
// a new history entry, so Back closes it; changing the tab or the dialog replaces the entry. The pure part is here.
import { useCallback, useMemo } from 'react';

import { useLocation } from '../../../lib/router';

export type DrawerTab = 'resources' | 'access';
export type DrawerDlg = 'restart' | 'off' | 'remove';

export interface DrawerUrl {
  m: string | null;
  /** The section of the card that is open for editing. */
  tab: DrawerTab | null;
  dlg: DrawerDlg | null;
}

const TABS: readonly string[] = ['resources', 'access'];
const DLGS: readonly string[] = ['restart', 'off', 'remove'];

export const readDrawer = (search: string): DrawerUrl => {
  const q = new URLSearchParams(search);
  const m = q.get('m');
  const tab = q.get('tab');
  const dlg = q.get('dlg');
  return {
    m: m && m.length > 0 ? m : null,
    tab: tab && TABS.includes(tab) ? (tab as DrawerTab) : null,
    dlg: dlg && DLGS.includes(dlg) ? (dlg as DrawerDlg) : null,
  };
};

/** The query string with the card's keys set (a `null` removes one); every other key stays as it was. */
export const withDrawer = (search: string, next: Partial<DrawerUrl>): string => {
  const q = new URLSearchParams(search);
  const merged = { ...readDrawer(search), ...next };
  // A card that is closed has neither a tab nor a dialog.
  const open = merged.m !== null;
  const set = (k: string, v: string | null) => {
    if (v && open) q.set(k, v);
    else q.delete(k);
  };
  set('m', merged.m);
  set('tab', merged.tab);
  set('dlg', merged.dlg);
  const s = q.toString();
  return s ? `?${s}` : '';
};

const MARK = 'hxm';

const notify = () => window.dispatchEvent(new PopStateEvent('popstate'));

/** Opens the card as a new entry, marked so the app knows Back will close it. */
const push = (search: string) => {
  history.pushState({ [MARK]: 1 }, '', `${location.pathname}${search}`);
  notify();
};

const replace = (search: string) => {
  history.replaceState(history.state, '', `${location.pathname}${search}`);
  notify();
};

export interface DrawerState extends DrawerUrl {
  open: (id: string) => void;
  close: () => void;
  setTab: (tab: DrawerTab | null) => void;
  setDlg: (dlg: DrawerDlg | null) => void;
}

/** The card's address as state. Router `navigate` scrolls to the top; this does not, the list stays where it is. */
export const useDrawerUrl = (): DrawerState => {
  const { search } = useLocation();
  const url = useMemo(() => readDrawer(search), [search]);
  const open = useCallback(
    (id: string) => push(withDrawer(location.search, { m: id, tab: null, dlg: null })),
    [],
  );
  const close = useCallback(() => {
    const marked = (history.state as Record<string, unknown> | null)?.[MARK] === 1;
    if (marked) history.back();
    else replace(withDrawer(location.search, { m: null }));
  }, []);
  const setTab = useCallback((tab: DrawerTab | null) => replace(withDrawer(location.search, { tab })), []);
  const setDlg = useCallback((dlg: DrawerDlg | null) => replace(withDrawer(location.search, { dlg })), []);
  return { ...url, open, close, setTab, setDlg };
};
