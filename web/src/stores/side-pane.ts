import { type Integrations, integrations, none } from '../api/integrations';
import type { BoardLink, DocumentLink, ServiceLink } from '../lib/links';
import { createStore } from './store';

// The right-side pane where boards and documents open next to the conversation, and what the runtime said about the
// services behind them.

export type SidePaneTab = 'board' | 'document';

export interface SidePane {
  open: boolean;
  tab: SidePaneTab;
  board: BoardLink | null;
  document: DocumentLink | null;
  /** Bumped to load the open item again (after a sign-in, or on request). */
  reload: number;
}

export const integrationsStore = createStore<Integrations>(none);
export const sidePaneStore = createStore<SidePane>({
  open: false,
  tab: 'board',
  board: null,
  document: null,
  reload: 0,
});

export async function loadIntegrations() {
  try {
    integrationsStore.set(await integrations.get());
  } catch {
    integrationsStore.set(none);
  }
}

/** Opens a board or a document in the pane. */
export function openInPane(link: ServiceLink) {
  sidePaneStore.set((s) =>
    link.kind === 'board'
      ? { ...s, open: true, tab: 'board', board: link }
      : { ...s, open: true, tab: 'document', document: link },
  );
}

export function showPane(open: boolean, tab?: SidePaneTab) {
  sidePaneStore.set((s) => ({ ...s, open, tab: tab ?? s.tab }));
}

export function reloadPane() {
  sidePaneStore.set((s) => ({ ...s, reload: s.reload + 1 }));
}
