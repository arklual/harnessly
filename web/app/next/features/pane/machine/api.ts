// The Machine tool over v2: the machine's figures now and over the last hour (GET .../machine/stats) and the stop of
// one process (POST .../machine/processes/stop). A stop is refused in words of the page, by the reason the runtime gives.
import { getPage } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { wordedPage } from '@/next/core/net/worded';
import { t } from '@/next/i18n';
import { limitsOf } from '@/next/stores/machines';
import { parseMachineRefusal, parsePage, type MachineId, type MachineProc } from '@/next/protocol/v2';

export const loadMachineStats = (m: MachineId) =>
  getPage(machineApi(m).http('/machine/stats'), parsePage.machineStats, limitsOf(m).cmdTimeoutMs);

/** Stops a process the page was shown (by pid and start time, so a process that took the pid since is not touched). */
export const stopProcess = (m: MachineId, proc: MachineProc) =>
  wordedPage(
    m,
    '/machine/processes/stop',
    parsePage.machineStop,
    (doc) => {
      const refusal = parseMachineRefusal(doc);
      return { code: refusal.code, message: t(`next.machine.stop.${refusal.reason}`) };
    },
    { method: 'POST', body: { pid: proc.pid, startTime: proc.startTime } },
  );

/** The key of a process for its whole life. */
export const procKey = (proc: Pick<MachineProc, 'pid' | 'startTime'>): string =>
  `${proc.pid}:${proc.startTime}`;
