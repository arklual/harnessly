// The agents' asks for secrets on the machine the person has open: read every few seconds while the page is visible,
// the oldest one shown as «Положить в сейф». The runtime counts these looks as the page being open, and tells the
// asking agent so.
import { useEffect, useEffectEvent, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import { pageClock } from '@/next/core/clock';
import {
  declineAsk,
  isSafeRefusal,
  loadAsks,
  putSecret,
  type ISecretAsk,
  type TSafeOutcome,
} from '@/next/features/safe/api';
import SafeAskDialog from '@/next/features/safe/ask-dialog';
import { safeReasonText } from '@/next/features/settings/safe-section';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export const ASKS_POLL_MS = 3000;

/** An answer as the words of why it was not taken, or null. */
const refusalOf = <T,>(res: TSafeOutcome<T>): string | null => {
  if (!('ok' in res)) return pageText(res);
  if (isSafeRefusal(res.value)) return safeReasonText(res.value.refused);
  return null;
};

export const useSecretAsks = (machine: MachineId | null) => {
  // The asks are the machine's they were read from: another machine on screen shows none until it answers.
  const [read, setRead] = useState<{ machine: MachineId; asks: readonly ISecretAsk[] } | null>(null);
  const [later, setLater] = useState<readonly string[]>([]);

  const look = useEffectEvent(async (m: MachineId) => {
    const res = await loadAsks(m);
    if (!('ok' in res) || isSafeRefusal(res.value)) return;
    const next = res.value;
    setRead((now) => {
      const same =
        now?.machine === m &&
        now.asks.length === next.length &&
        now.asks.every((a, i) => a.id === next[i]?.id);
      return same ? now : { machine: m, asks: next };
    });
  });

  useEffect(() => {
    if (!machine) return;
    const clock = pageClock();
    let timer: unknown = null;
    let looking = false;
    const again = () => {
      clock.clearTimeout(timer);
      timer = clock.setTimeout(again, ASKS_POLL_MS);
      if (looking || document.visibilityState !== 'visible') return;
      looking = true;
      void look(machine)
        .catch(() => undefined)
        .finally(() => {
          looking = false;
        });
    };
    again();
    return () => clock.clearTimeout(timer);
  }, [machine]);

  const asks = read !== null && read.machine === machine ? read.asks : [];
  const open = asks.filter((a) => !later.includes(a.id));
  const drop = (id: string) =>
    setRead((now) =>
      now === null ? now : { machine: now.machine, asks: now.asks.filter((a) => a.id !== id) },
    );
  return { open, drop, putAside: (id: string) => setLater((now) => [...now, id]) };
};

/** The dialog over everything, for the machine on screen. */
export const SecretAskHost = ({ machine }: { machine: MachineId | null }) => {
  const { open, drop, putAside } = useSecretAsks(machine);
  const ask = open[0] ?? null;
  if (!machine) return null;
  return (
    <SafeAskDialog
      ask={ask}
      more={Math.max(open.length - 1, 0)}
      onPut={async (a, value) => {
        const refused = refusalOf(await putSecret(machine, a.name, value));
        if (refused !== null) return refused;
        drop(a.id);
        toast(t('next.safe.ask.done', { name: a.name }), { tone: 'success' });
        return null;
      }}
      onDecline={async (a) => {
        const res = await declineAsk(machine, a.id);
        if (!('ok' in res)) return pageText(res);
        // Declined, or gone already (answered on another page, expired): either way it leaves the screen.
        drop(a.id);
        return null;
      }}
      onLater={(a) => putAside(a.id)}
    />
  );
};
