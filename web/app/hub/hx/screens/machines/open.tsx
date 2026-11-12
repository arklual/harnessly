// «Открыть»: leads to the machine's runtime; the address comes from the hub when the button is pressed.
import { createContext, useContext, useState } from 'react';

import { OrgApiError, orgErrorText } from '../../../org/api/errors';
import { useOrg } from '../../../org/context';
import { withBase } from '../../../lib/base';

export interface OpenMachine {
  /** The machine being opened now. */
  busyId: string | null;
  error: { id: string; text: string; untrusted?: boolean; ownWork?: boolean; debugFailed?: string } | null;
  open: (id: string) => Promise<void>;
  /** A role the owner's rule keeps out opens the machine for debugging, with a reason the owner sees, then it is opened. */
  debug: (id: string, reason: string) => Promise<void>;
  /** The owner trusts a machine the hub refused to open, then it is opened. */
  trust: (id: string) => Promise<void>;
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
      window.location.assign(withBase(url));
    } catch (e) {
      setError({
        id,
        text: orgErrorText(e),
        ...(e instanceof OrgApiError && e.code === 'untrusted' ? { untrusted: true } : {}),
        ...(e instanceof OrgApiError && e.code === 'own_work' ? { ownWork: true } : {}),
      });
    } finally {
      setBusyId(null);
    }
  };
  const trust = async (id: string) => {
    setBusyId(id);
    setError(null);
    try {
      await api.trustMachine(id);
    } catch (e) {
      setError({ id, text: orgErrorText(e) });
      setBusyId(null);
      return;
    }
    setBusyId(null);
    await open(id);
  };
  const debug = async (id: string, reason: string) => {
    setBusyId(id);
    try {
      await api.debugMachine(id, reason);
    } catch (e) {
      setError({ id, text: orgErrorText(e), ownWork: true, debugFailed: orgErrorText(e) });
      setBusyId(null);
      return;
    }
    setBusyId(null);
    await open(id);
  };
  return { busyId, error, open, trust, debug, clear: () => setError(null) };
};

/** The screen's opener, for the rows of the lists. */
export const OpenCtx = createContext<OpenMachine | null>(null);
export const useRowOpen = () => useContext(OpenCtx);
