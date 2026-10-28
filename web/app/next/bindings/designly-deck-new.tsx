// The create screen of a presentation over the machine: the agent that draws (the model a new chat starts on), the
// design systems to offer, the request with the deck's settings and uploads, and the move into the
// project once the runtime made it. A refusal is said in a toast and the brief stays.
import { useEffect, useState } from 'react';
import { bindDesignConv } from '@/next/bindings/design-conv';
import { createDesign, fetchSystems, type IDesignSystem } from '@/next/features/designly/api';
import { designKey } from '@/next/features/designly/design-conv';
import DeckCreate, { type IDeckCreateInput } from '@/next/features/designly/deck/deck-create';
import { useStartChoice } from '@/next/bindings/use-start-choice';
import { rememberChoice } from '@/next/features/composer/start-choice';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

interface IDeckCreateViewProps {
  machine: MachineId;
}

const DeckCreateView = ({ machine }: IDeckCreateViewProps) => {
  // The same start as a new chat: the CLI settings file's model and effort, else the last pick, else the first model.
  const { choice } = useStartChoice(machine, true);
  const [systems, setSystems] = useState<IDesignSystem[]>([]);
  useEffect(() => {
    const abort = new AbortController();
    // The systems are only an offer: without them the chip lists the template and «no design system» alone.
    fetchSystems(machine, abort.signal).then(setSystems, () => undefined);
    return () => abort.abort();
  }, [machine]);

  const onCreate = async (input: IDeckCreateInput) => {
    try {
      const created = await createDesign(machine, {
        title: input.title,
        brief: input.brief,
        kind: 'slides',
        provider: choice?.provider ?? 'claude',
        model: choice?.model ?? null,
        effort: choice?.effort ?? null,
        designSystem: input.designSystem,
        deck: input.deck,
        ...(input.attachmentIds.length > 0 ? { attachmentIds: input.attachmentIds } : {}),
      });
      if (choice) rememberChoice(choice);
      // The chat that draws the new deck is the one its page opens on.
      bindDesignConv(designKey(machine, created.id), { id: created.conv, fresh: true });
      toast(t('next.designly.started', { title: input.title }));
      goTo({ kind: ROUTE.site, id: created.id });
    } catch (failure) {
      toast(t('next.designly.createFailed'), { tone: 'error', description: (failure as Error).message });
      throw failure;
    }
  };

  return (
    <DeckCreate
      machine={machine}
      onBack={() => goTo({ kind: ROUTE.designly })}
      systems={systems}
      onCreate={onCreate}
    />
  );
};

export default DeckCreateView;
