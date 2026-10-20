import { createContext } from 'react';
import { create } from 'zustand';
import type { TProviderStatus } from '@/components/agents/provider-icon';
import type { INoticeDetail } from '@/lib/hl-notices';

export const RuntimeMachineContext = createContext<string | undefined>(undefined);

interface IFleetStatusStore {
  machines: Record<string, Record<string, TProviderStatus>>;
  /** Per machine and tab: what the bell quotes. Only machines watched through their status socket have it. */
  details: Record<string, Record<string, INoticeDetail>>;
  setMachine: (
    machineId: string,
    statuses: Record<string, TProviderStatus>,
    details?: Record<string, INoticeDetail>,
  ) => void;
}

const useFleetStatus = create<IFleetStatusStore>((set) => ({
  machines: {},
  details: {},
  setMachine: (machineId, statuses, details) =>
    set((state) => ({
      machines: { ...state.machines, [machineId]: statuses },
      details: details ? { ...state.details, [machineId]: details } : state.details,
    })),
}));

export default useFleetStatus;
