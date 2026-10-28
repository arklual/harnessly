// «People» of the machine on screen: its asks from the shared list, a row opens the ask in a dialog that answers it
// and leads to the asking agent's chat.
import { useEffect, useState } from 'react';
import { askActions } from '@/next/features/asks/ask-step';
import { AskDialog, PeopleSection, peopleAsks } from '@/next/features/asks/people-section';
import { useAsks } from '@/next/features/asks/store';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';

/** The clock of «N min left» and of which closed asks are still recent. */
const useMinute = () => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(timer);
  }, []);
  return now;
};

export const PeopleBlock = ({ machine }: { machine: MachineId | null }) => {
  const asks = useAsks(machine);
  const now = useMinute();
  const [open, setOpen] = useState(false);
  // The ask stays in the dialog while it closes.
  const [shown, setShown] = useState<string | null>(null);
  const ask = (asks ?? []).find((a) => a.id === shown) ?? null;
  const close = () => setOpen(false);
  const actions =
    machine && ask
      ? askActions(
          machine,
          ask.id,
          ask.conversation
            ? () => {
                close();
                goTo({ kind: ROUTE.chat, id: ask.conversation });
              }
            : undefined,
        )
      : null;
  return (
    <>
      <PeopleSection
        asks={peopleAsks(asks, now)}
        now={now}
        onOpen={(id) => {
          setShown(id);
          setOpen(true);
        }}
      />
      <AskDialog ask={ask} open={open} actions={actions} onClose={close} />
    </>
  );
};
