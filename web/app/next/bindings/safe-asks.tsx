// The agents' asks for secrets on the machine the person has open: read every few seconds while the page is visible,
// the oldest one shown as «Положить в сейф». The runtime counts these looks as the page being open, and tells the
// asking agent so.
import { useEffect, useEffectEvent, useRef, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import { calmPoll } from '@/next/core/net/calm-poll';
import {
  declineAsk,
  isSafeRefusal,
  loadAsks,
  putAsk,
  grantAsk,
  type ISecretAsk,
  type TSafeOutcome,
} from '@/next/features/safe/api';
import SafeAskDialog from '@/next/features/safe/ask-dialog';
import { safeReasonText } from '@/next/features/settings/safe-section';
import type { MachineId } from '@/next/protocol/v2';
import { useProjects } from '@/next/stores/projects';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export const ASKS_POLL_MS = 3000;
/**
 * The longest step while the asks stay the same. The runtime takes a look in the last 15 s as the page being
 * open (internal/secrets pageSeen), so a quiet page still looks well within that.
 */
export const ASKS_QUIET_MS = 9000;

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

  const last = useRef<string | null>(null);
  /** One read; true when the asks changed or one waits for the person. */
  const look = useEffectEvent(async (m: MachineId): Promise<boolean> => {
    const res = await loadAsks(m);
    if (!('ok' in res) || isSafeRefusal(res.value)) return false;
    const next = res.value;
    const said = `${m}\u0000${JSON.stringify(next)}`;
    const changed = said !== last.current;
    last.current = said;
    setRead((now) => {
      const same =
        now?.machine === m &&
        now.asks.length === next.length &&
        now.asks.every((a, i) => JSON.stringify(a) === JSON.stringify(next[i]));
      return same ? now : { machine: m, asks: next };
    });
    return changed || next.length > 0;
  });

  // Read while the page is visible, slower while nothing waits.
  useEffect(() => {
    if (!machine) return;
    return calmPoll({ base: ASKS_POLL_MS, max: ASKS_QUIET_MS, look: () => look(machine) });
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
  const projects = useProjects(machine);
  const { open, drop, putAside } = useSecretAsks(machine);
  const ask = open[0] ?? null;
  if (!machine) return null;
  return (
    <SafeAskDialog
      ask={ask}
      more={Math.max(open.length - 1, 0)}
      projectName={projects.find((p) => p.id === ask?.project)?.name}
      onPut={async (a, value) => {
        const refused = refusalOf(await putAsk(machine, a.id, value));
        if (refused !== null) return refused;
        drop(a.id);
        toast(t('next.safe.ask.done', { name: a.name }), { tone: 'success' });
        return null;
      }}
      onGrant={async (a) => {
        const refused = refusalOf(await grantAsk(machine, a.id));
        if (refused !== null) return refused;
        drop(a.id);
        toast(t('next.safe.ask.granted', { name: a.name }), { tone: 'success' });
        return null;
      }}
      onDecline={async (a) => {
        const res = await declineAsk(machine, a.id);
        const refused = refusalOf(res);
        if (
          refused !== null &&
          !('ok' in res && isSafeRefusal(res.value) && res.value.refused === 'not-found')
        )
          return refused;
        // Declined, or gone already (answered on another page, expired): either way it leaves the screen.
        drop(a.id);
        return null;
      }}
      onLater={(a) => putAside(a.id)}
    />
  );
};
