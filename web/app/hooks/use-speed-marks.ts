import { useEffect } from 'react';
import { useHlMachines } from '@/hooks/use-hl-machines';
import {
  markFirstScreenPainted,
  markShellPainted,
  startSpeedTelemetry,
  updateSpeedContext,
} from '@/lib/hl-speed-client';

interface ISpeedMarksOptions {
  owner: string;
  /** The conversation list has answered: from its next paint the workspace is a working screen. */
  listReady: boolean;
  defaultMachineId: string | null | undefined;
}

/** Load timings of the workspace shell and the context every speed event carries. */
const useSpeedMarks = ({ owner, listReady, defaultMachineId }: ISpeedMarksOptions) => {
  const machines = useHlMachines().data?.machines;
  const release =
    machines?.find((machine) => machine.id === defaultMachineId)?.release ??
    (machines?.length === 1 ? machines[0].release : null);

  useEffect(() => {
    updateSpeedContext({
      machine: defaultMachineId ?? null,
      release: release?.version ?? null,
      runtimeCommit: release?.commit ?? null,
    });
  }, [defaultMachineId, release?.version, release?.commit]);

  useEffect(() => {
    startSpeedTelemetry(owner);
    markShellPainted();
  }, [owner]);

  useEffect(() => {
    if (listReady) markFirstScreenPainted();
  }, [listReady]);
};

export default useSpeedMarks;
