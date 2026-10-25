// The Machine tab of the right pane: asks the machine for its figures every two seconds while the tab is on screen and
// the page is visible, keeps the last good answer through a stumble, and stops a process only on the machine's word.
import { useEffect, useRef, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import MachineView from '@/next/features/pane/machine-view';
import { loadMachineStats, procKey, stopProcess } from '@/next/features/pane/machine/api';
import { t } from '@/next/i18n';
import type { MachineId, MachineProc, MachineStats } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';
import usePendingList from '@/next/ui/use-pending-list';

/** How often the figures are asked for. */
export const MACHINE_POLL_MS = 2000;
/** A failed ask is told to the person after this many in a row (a loading tab tells it at once). */
const FAILS_BEFORE_TOLD = 3;

interface IMachineTabProps {
  machineId: MachineId;
  machineName: string;
}

const MachineTab = ({ machineId, machineName }: IMachineTabProps) => {
  const [stats, setStats] = useState<MachineStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const [attempt, setAttempt] = useState(0);
  const { pending, run } = usePendingList();
  const loaded = useRef(false);

  useEffect(() => {
    let live = true;
    let asking = false;
    let fails = 0;
    let timer: number | undefined;

    const schedule = () => {
      timer = window.setTimeout(() => void ask(), MACHINE_POLL_MS);
    };
    const ask = async () => {
      window.clearTimeout(timer);
      if (document.hidden || asking) {
        schedule();
        return;
      }
      asking = true;
      const res = await loadMachineStats(machineId);
      asking = false;
      if (!live) return;
      if ('ok' in res) {
        fails = 0;
        loaded.current = true;
        const listed = new Set(res.value.procs.map(procKey));
        setStats(res.value);
        setError(null);
        // A stopped process stays hidden until the machine stops listing it, then its mark goes.
        setHidden((current) => {
          const kept = [...current].filter((key) => listed.has(key));
          return kept.length === current.size ? current : new Set(kept);
        });
      } else {
        fails += 1;
        if (!loaded.current || fails >= FAILS_BEFORE_TOLD) setError(pageText(res));
      }
      schedule();
    };
    const resume = () => {
      if (!document.hidden) void ask();
    };

    void ask();
    document.addEventListener('visibilitychange', resume);
    return () => {
      live = false;
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', resume);
    };
  }, [machineId, attempt]);

  const stop = (proc: MachineProc) => {
    void run(procKey(proc), t('next.machine.stopFailed', { name: proc.name }), async () => {
      await stopProcess(machineId, proc);
      setHidden((current) => new Set(current).add(procKey(proc)));
      toast(t('next.machine.stopped', { name: proc.name }), { tone: 'success' });
    });
  };

  return (
    <MachineView
      name={machineName}
      stats={stats}
      error={error}
      onRetry={() => {
        setError(null);
        setAttempt((n) => n + 1);
      }}
      pending={pending}
      hidden={hidden}
      onStop={stop}
    />
  );
};

export default MachineTab;
