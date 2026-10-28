// Where the next new chat starts: the project picked with «+» on a project of the sidebar, and the machine picked under
// the greeting. The start page shows both; the project clears with a click, the machine goes back to the
// default one (the machine picked in the sidebar) once the chat is made. Opening «Новый диалог» from the sidebar's top
// clears both. null: the usual folder, the default machine.

import { createStore, useStore } from '@/next/core/store';
import type { MachineId } from '@/next/protocol/v2';

export const newChatProjectStore = createStore<string | null>(null);
export const newChatMachineStore = createStore<MachineId | null>(null);

export const useNewChatProject = () => useStore(newChatProjectStore);
export const useNewChatMachine = () => useStore(newChatMachineStore);
