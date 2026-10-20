import { useCallback, useEffect, useState } from 'react';
import { currentMachineId } from '@/lib/app-path';
import { fetchDesignCheck, startDesignCheck } from '@/lib/hl-design-library-client';
import type { ICheckReport } from '@/types/server/check';
import { t } from '@/lib/i18n';

const POLL_MS = 1_500;

/**
 * The last check of one design, filling while it runs. `report` is null until the first answer, then the
 * report, or `false` when the design was never checked. Mount it with `key={artifactId}`: another design starts over.
 * `autoStart` starts a check as soon as the last report is known, unless one is running.
 */
const useDesignCheck = (artifactId: string, autoStart = false) => {
  const [report, setReport] = useState<ICheckReport | false | null>(null);
  const [error, setError] = useState<string | null>(null);
  const running = report !== null && report !== false && report.finishedAt === null;

  const start = useCallback(async () => {
    setError(null);
    try {
      setReport((await startDesignCheck(currentMachineId(), artifactId)).report);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('hooks.useDesignCheck.startFailed'));
    }
  }, [artifactId]);

  useEffect(() => {
    const controller = new AbortController();
    fetchDesignCheck(currentMachineId(), artifactId, controller.signal)
      .then(({ report: last }) => {
        setReport(last ?? false);
        if (autoStart && last?.finishedAt !== null) void start();
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setError(failure instanceof Error ? failure.message : t('hooks.useDesignCheck.readFailed'));
      });
    return () => controller.abort();
  }, [artifactId, autoStart, start]);

  useEffect(() => {
    if (!running) return;
    const controller = new AbortController();
    const timer = window.setInterval(() => {
      fetchDesignCheck(currentMachineId(), artifactId, controller.signal).then(
        ({ report: next }) => next && setReport(next),
        () => undefined,
      );
    }, POLL_MS);
    return () => {
      window.clearInterval(timer);
      controller.abort();
    };
  }, [running, artifactId]);

  return { report, error, running, start };
};

export default useDesignCheck;
