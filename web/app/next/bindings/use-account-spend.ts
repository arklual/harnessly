// What every account of the machine spent today and this week, for the provider cards of Settings: the
// machine's report of spend (`…/stats`), asked when the page opens and again every five minutes while it
// stays open. The cards of one page share one ask. A machine that counts nothing (the hub keeps the figures) or did
// not answer gives no spend: the cards then draw none, never zero.
import { useEffect, useState } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import { readAccountSpend, type AccountSpendMap } from '@/next/features/limits/card-model';
import type { MachineId } from '@/next/protocol/v2';

const SPEND_POLL_MS = 300_000;
/** The report is built over the transcripts of the machine: it is given longer than a settings read. */
const SPEND_TIMEOUT_MS = 30_000;
/** An ask younger than this is shared by every card that wants the spend. */
const SHARE_MS = 60_000;

const asks = new Map<MachineId, { at: number; answer: Promise<AccountSpendMap | null> }>();

const askSpend = (machine: MachineId): Promise<AccountSpendMap | null> => {
  const held = asks.get(machine);
  if (held && Date.now() - held.at < SHARE_MS) return held.answer;
  const answer = runtimeJson<unknown>(machine, platformPath('/stats'), undefined, SPEND_TIMEOUT_MS).then(
    (doc) => readAccountSpend(doc, machine, Date.now()),
  );
  asks.set(machine, { at: Date.now(), answer });
  answer.catch(() => asks.delete(machine));
  return answer;
};

export const useAccountSpend = (machine: MachineId): AccountSpendMap | null => {
  const [spend, setSpend] = useState<AccountSpendMap | null>(null);
  useEffect(() => {
    let live = true;
    const ask = () =>
      askSpend(machine).then(
        (next) => live && setSpend(next),
        // The last answer stays; none at all leaves the cards without spend.
        () => undefined,
      );
    void ask();
    const timer = window.setInterval(() => void ask(), SPEND_POLL_MS);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [machine]);
  return spend;
};
