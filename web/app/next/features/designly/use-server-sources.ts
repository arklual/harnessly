// The runtime's record of a board beyond the slot's network: what the page logged to the library and the design's last
// check, read while the panel is open. The tab draws what came; a source that does not answer is reported as such, never
// filled in.
import { useEffect, useState } from 'react';
import { fetchCheck, fetchPageLog } from '@/next/features/designly/api';
import type { ICheckReport, IPageLogEntry } from '@/next/features/designly/server-data';
import type { MachineId } from '@/next/protocol/v2';

/** Checks and page logs change by the second at most, and nobody watches them as a stream. */
const POLL_MS = 3000;

interface IRecord {
  artifact: string;
  pageLog: IPageLogEntry[];
  check: ICheckReport | null;
  /** The library's log could not be read now. */
  logFailed: boolean;
}

const none = (artifact: string): IRecord => ({ artifact, pageLog: [], check: null, logFailed: false });

export const useServerSources = (machine: MachineId, artifact: string, open: boolean) => {
  const [stored, setRecord] = useState<IRecord>(() => none(artifact));
  const record = stored.artifact === artifact ? stored : none(artifact);

  useEffect(() => {
    if (!open) return;
    const abort = new AbortController();
    let timer = 0;
    const poll = async () => {
      const [log, check] = await Promise.allSettled([
        fetchPageLog(machine, artifact, abort.signal),
        fetchCheck(machine, artifact, abort.signal),
      ]);
      if (abort.signal.aborted) return;
      setRecord((current) => {
        const base = current.artifact === artifact ? current : none(artifact);
        return {
          artifact,
          pageLog: log.status === 'fulfilled' ? log.value : base.pageLog,
          check: check.status === 'fulfilled' ? check.value : base.check,
          logFailed: log.status === 'rejected',
        };
      });
      timer = window.setTimeout(poll, POLL_MS);
    };
    void poll();
    return () => {
      abort.abort();
      window.clearTimeout(timer);
    };
  }, [machine, artifact, open]);

  return record;
};
