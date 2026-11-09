// Where a place the runtime named opens (GET /api/v2/conversations/{id}/open): in a tab of its own, in the pane's
// browser, or nowhere because the person has a step to take. Kept apart from the pane's binding so the rule has one
// place: a connection's issue or page is a native tab, never the remote browser started on the side, and a `link`
// answer starts nothing.
import type { OpenTarget } from '@/next/protocol/v2';

export interface IOpenPlaceActions {
  /** The pane's browser is asked for `target` (the live browser starts). */
  browser: (target: string) => void;
  /** A native tab of the pane; the id of the tab, or null while the pane cannot show one. */
  show: (tab: Exclude<OpenTarget['tab'], 'browser' | 'link'>, target: string) => string | null;
  /** A tab was opened: bring it to the front. */
  reveal: (id: string) => void;
  /** The pane cannot show a tab yet. */
  notYet: () => void;
  /** The connection's sign-in was refused: lead to its settings. */
  connect: () => void;
  /** The provider cannot be shown inside a page: offer the original address. */
  original: (address: string) => void;
}

export const openPlace = ({ tab, target, reason }: OpenTarget, actions: IOpenPlaceActions): void => {
  if (tab === 'link') {
    if (reason === 'connect') actions.connect();
    else actions.original(target);
    return;
  }
  if (tab === 'browser') {
    actions.browser(target);
    return;
  }
  const id = actions.show(tab, target);
  if (!id) {
    actions.notYet();
    return;
  }
  actions.reveal(id);
};
