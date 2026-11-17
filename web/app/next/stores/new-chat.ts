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

// «Новый диалог» asked again while the start page is already shown: the page stays, but what it had open (a thing
// of the day opened from «Главная») closes, so the person is back at a clean start page. The day watches this count.
export const homeResetStore = createStore(0);
export const useHomeReset = () => useStore(homeResetStore);
export const resetHome = (): void => homeResetStore.update((n) => n + 1);
