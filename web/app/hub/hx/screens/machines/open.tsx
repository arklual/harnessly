// «Открыть»: leads to the machine's runtime; the address comes from the hub when the button is pressed.
import { createContext, useContext, useState } from 'react';

import { orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';

export interface OpenMachine {
  /** The machine being opened now. */
  busyId: string | null;
  error: { id: string; text: string } | null;
  open: (id: string) => Promise<void>;
  clear: () => void;
}

export const useOpenMachine = (): OpenMachine => {
  const { api } = useOrg();
  const [busyId, setBusyId] = useState<string | null>(null);
  const [error, setError] = useState<OpenMachine['error']>(null);
  const open = async (id: string) => {
    setBusyId(id);
    setError(null);
    try {
      const { url } = await api.openMachine(id);
      window.location.assign(url);
    } catch (e) {
      setError({ id, text: orgErrorText(e) });
    } finally {
      setBusyId(null);
    }
  };
  return { busyId, error, open, clear: () => setError(null) };
};

/** The screen's opener, for the rows of the lists. */
export const OpenCtx = createContext<OpenMachine | null>(null);
export const useRowOpen = () => useContext(OpenCtx);
