// What a person's session on this page holds, and how it is let go: when the account changes or signs out, nothing of
// the previous person stays in memory (stores, cursors, chat cache) and no socket of theirs stays open.

import { resetResumes } from '@/next/core/realtime/cursors';
import { resetLinkGrace } from '@/next/core/realtime/grace';
import { hub } from '@/next/core/realtime/hub';
import { batch, flush, type KeyedStore } from '@/next/core/store';
import { resetCache } from '@/next/stores/cache';
import { connectionStore } from '@/next/stores/connection';
import {
  backgroundStore,
  entryStore,
  feedStore,
  gatesStore,
  nowStore,
  queueStore,
  turnStore,
} from '@/next/stores/conversation';
import { conversationsStore, convsCachedStore, convsLoadedStore } from '@/next/stores/conversations';
import { draftsStore } from '@/next/stores/drafts';
import { draftBaseStore } from '@/next/stores/draft-sync';
import {
  activeMachineStore,
  homeMachineStore,
  machineOrderStore,
  machinesStore,
} from '@/next/stores/machines';
import { meStore } from '@/next/stores/me';
import { convMessagesStore, messagesStore } from '@/next/stores/messages';
import { opsStore } from '@/next/stores/ops';
import { defaultCwdStore, projectsStore } from '@/next/stores/projects';
import { resetProfileSidebar } from '@/next/stores/profile-sidebar';
import { newChatMachineStore } from '@/next/stores/new-chat';

const clear = <K, V>(s: KeyedStore<K, V>) => {
  for (const k of [...s.keys()]) s.delete(k);
};

/** Closes the sockets and empties every store of the core. IndexedDB is not touched: it is the person's own. */
export function clearPersonData(): void {
  hub.stop();
  batch(() => {
    for (const s of [
      machinesStore,
      connectionStore,
      conversationsStore,
      convsLoadedStore,
      convsCachedStore,
      projectsStore,
      defaultCwdStore,
      feedStore,
      turnStore,
      entryStore,
      nowStore,
      gatesStore,
      queueStore,
      backgroundStore,
      messagesStore,
      convMessagesStore,
      opsStore,
      draftsStore,
      draftBaseStore,
    ] as KeyedStore<unknown, unknown>[]) {
      clear(s);
    }
    machineOrderStore.set(null);
    activeMachineStore.set(null);
    homeMachineStore.set(null);
    meStore.set({ k: 'loading' });
  });
  flush();
  resetResumes();
  resetLinkGrace();
  resetCache();
  resetProfileSidebar();
  newChatMachineStore.set(null);
}
