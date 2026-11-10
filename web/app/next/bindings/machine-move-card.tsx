// The move of a machine's data into the hub under its row in «Машины» (HRN-883): asked of the machine while it is on.
import { useState } from 'react';
import { moveCardOf } from '@/next/bindings/machine-move';
import MoveCard from '@/next/features/settings/move-card';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { retryMove, useMachineMove } from '@/next/stores/machine-move';
import { toast } from '@/next/ui/toast';

const MachineMoveCard = ({
  machine,
  name,
  online,
}: {
  machine: MachineId;
  name: string;
  online: boolean;
}) => {
  const move = useMachineMove(machine, online);
  const [busy, setBusy] = useState(false);
  const card = moveCardOf(machine, name, move, busy, () => {
    setBusy(true);
    void retryMove(machine)
      .then((ok) => {
        if (!ok) toast(t('next.move.retryFailed'), { tone: 'error' });
      })
      .finally(() => setBusy(false));
  });
  return card ? <MoveCard card={card} /> : null;
};

export default MachineMoveCard;
