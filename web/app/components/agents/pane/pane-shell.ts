import { appFetch } from '@/lib/app-path';

// Apart from shell-tab, so closing a pane's tab does not bring xterm with the page.
export const closePaneShell = (tabId: string): void => {
  void appFetch(`/api/platform/pane-shell?tabId=${encodeURIComponent(tabId)}`, { method: 'DELETE' }).catch(
    () => {},
  );
};
