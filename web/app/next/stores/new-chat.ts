// Where the next new chat starts: the project picked with «+» on a project of the sidebar. The start page shows it and
// clears it with a click; opening «Новый диалог» from the sidebar's top clears it too. null: the machine's usual folder.

import { createStore, useStore } from '@/next/core/store';

export const newChatProjectStore = createStore<string | null>(null);

export const useNewChatProject = () => useStore(newChatProjectStore);
