// The team's catalog and its changes. A change waits for the machine (pessimistic): the row shows it is busy, and the
// catalog and the apps list are read again from the machine when it answers; nothing is guessed in between.
import { useCallback, useEffect, useState } from 'react';
import {
  installForTeam,
  loadTeam,
  muteTeamApp,
  uninstallForTeam,
  type ITeamInfo,
} from '@/next/features/app-catalog/api';
import { ROW_ACTION, type ICatalogRow } from '@/next/features/app-catalog/model';
import type { MachineId } from '@/next/protocol/v2';

export interface ITeamState {
  /** null while it loads. */
  info: ITeamInfo | null;
  error: string | null;
  /** The app whose change is on its way. */
  busy: string | null;
  /** The words of the last refused change. */
  failure: string | null;
  act: (row: ICatalogRow) => Promise<void>;
}

/** `onChanged` runs after a change the machine took, for the apps list to be read again. */
export const useTeam = (m: MachineId, onChanged: () => void): ITeamState => {
  const [info, setInfo] = useState<ITeamInfo | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [failure, setFailure] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    loadTeam(m).then(
      (next) => live && (setInfo(next), setError(null)),
      (e: Error) => live && setError(e.message),
    );
    return () => {
      live = false;
    };
  }, [m]);

  const act = useCallback(
    async (row: ICatalogRow) => {
      if (busy !== null || row.action === null) return;
      setBusy(row.entry.id);
      setFailure(null);
      try {
        const { id, channel } = row.entry;
        switch (row.action) {
          case ROW_ACTION.install:
            await installForTeam(m, id, channel);
            break;
          case ROW_ACTION.uninstall:
            await uninstallForTeam(m, id);
            break;
          case ROW_ACTION.mute:
            await muteTeamApp(m, id, true);
            break;
          case ROW_ACTION.unmute:
            await muteTeamApp(m, id, false);
            break;
        }
        setInfo(await loadTeam(m));
        onChanged();
      } catch (e) {
        setFailure(e instanceof Error ? e.message : String(e));
      } finally {
        setBusy(null);
      }
    },
    [m, busy, onChanged],
  );

  return { info, error, busy, failure, act };
};
