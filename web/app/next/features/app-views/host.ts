// Where the app views of a screen live: the machine, and the chat they work beside. The chat binds it once; the cards in
// its feed and the tabs of its pane read it.
import { createContext, useContext } from 'react';
import type { ConvId, MachineId } from '@/next/protocol/v2';

export interface IAppViewHost {
  machine: MachineId;
  conversation: ConvId | null;
}

export const AppViewHost = createContext<IAppViewHost | null>(null);

export const useAppViewHost = (): IAppViewHost | null => useContext(AppViewHost);
