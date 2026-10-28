// A recording on the page: read from the runtime, polled while its transcript is still being built, and changed through
// its montage (`edit`). A change is pessimistic: the screen keeps showing the saved montage until the runtime has
// answered with the new one.
import { useEffect, useState } from 'react';
import { fetchDeckRec, putDeckEdit } from '@/next/features/designly/deck/api';
import type { IDeckEdit, IDeckRecording } from '@/next/features/designly/deck/types';
import { WordedError } from '@/next/core/net/worded';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

/** The transcript is built after the recording ends; look again this often, this many times. */
const POLL_MS = 2000;
const POLL_MAX = 45;

export interface IDeckRec {
  /** null while the first read is on its way. */
  rec: IDeckRecording | null;
  /** The transcript is not ready yet. */
  building: boolean;
  failed: boolean;
  /** A change is on its way to the runtime. */
  busy: boolean;
  /** Saves a montage; resolves true when the runtime has taken it. */
  change: (edit: IDeckEdit) => Promise<boolean>;
}

export const useDeckRec = (
  machine: MachineId,
  artifact: string,
  recId: string,
  initial: IDeckRecording | null,
): IDeckRec => {
  const [rec, setRec] = useState<IDeckRecording | null>(initial);
  const [failed, setFailed] = useState(false);
  const [busy, setBusy] = useState(false);
  const [polls, setPolls] = useState(0);

  const building =
    rec !== null && rec.segments.length === 0 && rec.summary.state !== 'failed' && polls < POLL_MAX;

  useEffect(() => {
    if (rec !== null && !building) return;
    const abort = new AbortController();
    const id = setTimeout(
      () => {
        fetchDeckRec(machine, artifact, recId, abort.signal)
          .then((next) => {
            setRec(next);
            setPolls((n) => n + 1);
          })
          .catch(() => {
            if (!abort.signal.aborted) {
              setFailed(rec === null);
              setPolls((n) => n + 1);
            }
          });
      },
      rec === null ? 0 : POLL_MS,
    );
    return () => {
      clearTimeout(id);
      abort.abort();
    };
  }, [machine, artifact, recId, rec, building, polls]);

  const change = async (edit: IDeckEdit): Promise<boolean> => {
    if (busy) return false;
    setBusy(true);
    try {
      setRec(await putDeckEdit(machine, artifact, recId, edit));
      return true;
    } catch (error) {
      toast(error instanceof WordedError ? error.message : t('next.failure.http'), { tone: 'error' });
      return false;
    } finally {
      setBusy(false);
    }
  };

  return { rec, building, failed, busy, change };
};
