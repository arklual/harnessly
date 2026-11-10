// Test chats (HRN-842): chats an agent made to check something. The sidebar and the search leave them out unless the
// person asks to see them; a chat that is open is never taken away from under the person.
import { useSyncExternalStore } from 'react';
import type { ISidebarChat } from './types';

const KEY = 'hx-show-test-chats';

const listeners = new Set<() => void>();
let shown: boolean | null = null;

const read = (): boolean => {
  try {
    return localStorage.getItem(KEY) === '1';
  } catch {
    return false;
  }
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
};

const snapshot = (): boolean => (shown ??= read());

/** Whether the person shows test chats; kept on the device. */
export const useShowTestChats = (): boolean => useSyncExternalStore(subscribe, snapshot, () => false);

export const toggleShowTestChats = () => {
  shown = !snapshot();
  try {
    localStorage.setItem(KEY, shown ? '1' : '0');
  } catch {
    // Private mode or full storage: the choice holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

/** The chats the sidebar and the search list: without the test chats, unless they are shown or one is open. */
export const withoutTestChats = (
  chats: readonly ISidebarChat[],
  show: boolean,
  currentId: string | null,
): readonly ISidebarChat[] => {
  if (show || !chats.some((chat) => chat.test)) return chats;
  return chats.filter((chat) => !chat.test || chat.id === currentId);
};

/** Whether any chat is a test chat: the item that shows them is there only then. */
export const hasTestChats = (chats: readonly ISidebarChat[] | null): boolean =>
  chats !== null && chats.some((chat) => chat.test === true);
