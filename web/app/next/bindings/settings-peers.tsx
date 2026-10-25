// «Связь между агентами» on the machine the person has open: read when the Machines page opens, every change sent
// against the revision shown and taken only from the machine's answer (pessimistic: a refused change leaves the
// switches where they were). The audit is read again every half minute while the page is open.
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import type { Pending } from '@/next/bindings/settings-agents';
import { loadPeers, savePeers, type IPeerSettings, type IPeersView } from '@/next/features/peers/api';
import PeersSection from '@/next/features/settings/peers-section';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { useMachines } from '@/next/stores/machines';

const REFRESH_MS = 30_000;

type TLoad = { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; view: IPeersView };

interface IPeersPageProps {
  machine: MachineId;
  now: number;
  pending: Pending;
}

export const PeersPage = ({ machine, now, pending }: IPeersPageProps) => {
  const [state, setState] = useState<TLoad>({ k: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const machines = useMachines();

  useEffect(() => {
    let live = true;
    const load = () =>
      void loadPeers(machine).then((res) => {
        if (!live) return;
        if ('ok' in res) setState({ k: 'ready', view: res.value });
        else setState((now) => (now.k === 'ready' ? now : { k: 'failed', text: pageText(res) }));
      });
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [machine, attempt]);

  const retry = useCallback(() => {
    setState({ k: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  const change = (key: string, next: IPeerSettings) =>
    void pending.run(key, t('next.settings.failed'), async () => {
      const res = await savePeers(machine, next);
      if (!('ok' in res)) {
        // Someone changed it meanwhile: show what is there now, the person decides again.
        if (res.r === 'failed' && res.code === 'http' && res.status === 409) retry();
        throw new Error(
          res.r === 'failed' && res.status === 409 ? t('next.settings.peers.conflict') : pageText(res),
        );
      }
      setState({ k: 'ready', view: res.value });
    });

  const fleet = (machines ?? []).map(({ machine: m }) => ({ id: m.id, name: m.name }));
  return (
    <PeersSection
      view={state.k === 'ready' ? state.view : null}
      failed={state.k === 'failed' ? { reason: state.text, onRetry: retry } : null}
      fleet={fleet}
      now={now}
      busy={new Set(pending.pending.keys())}
      onChange={change}
    />
  );
};
