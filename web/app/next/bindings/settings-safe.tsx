// Settings → Safe on the machine the person has open: the secrets read when the page opens, every change taken only
// from the machine's answer (a refused one leaves the list as it was).
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import {
  isSafeRefusal,
  loadSafe,
  putSecret,
  removeSecret,
  type ISecret,
  type TSafeOutcome,
} from '@/next/features/safe/api';
import SafeSection, { safeReasonText } from '@/next/features/settings/safe-section';
import type { MachineId } from '@/next/protocol/v2';

type TLoad = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; secrets: readonly ISecret[] };

/** An answer as the words of why it was not taken, or the value. */
const outcome = <T,>(res: TSafeOutcome<T>): { value: T } | { error: string } => {
  if (!('ok' in res)) return { error: pageText(res) };
  if (isSafeRefusal(res.value)) return { error: safeReasonText(res.value.refused) };
  return { value: res.value };
};

const byName = (a: ISecret, b: ISecret) => a.name.toLowerCase().localeCompare(b.name.toLowerCase());

export const SafePage = ({ machine, now }: { machine: MachineId; now: number }) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let live = true;
    void loadSafe(machine).then((res) => {
      if (!live) return;
      const got = outcome(res);
      if ('value' in got) setState({ k: 'ready', secrets: got.value.secrets });
      else setState((cur) => (cur.k === 'ready' ? cur : { k: 'failed', text: got.error }));
    });
    return () => {
      live = false;
    };
  }, [machine, attempt]);

  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  return (
    <SafeSection
      secrets={state.k === 'ready' ? state.secrets : null}
      failed={state.k === 'failed' ? { reason: state.text, onRetry: retry } : null}
      now={now}
      onPut={async (name, value) => {
        const got = outcome(await putSecret(machine, name, value));
        if ('error' in got) return got.error;
        setState((cur) => {
          if (cur.k !== 'ready') return cur;
          const rest = cur.secrets.filter((s) => s.name !== got.value.name);
          return { k: 'ready', secrets: [...rest, got.value].sort(byName) };
        });
        return null;
      }}
      onRemove={async (name) => {
        const got = outcome(await removeSecret(machine, name));
        if ('error' in got) return got.error;
        setState((cur) =>
          cur.k === 'ready' ? { k: 'ready', secrets: cur.secrets.filter((s) => s.name !== name) } : cur,
        );
        return null;
      }}
    />
  );
};
