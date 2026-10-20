import { useContext, useState } from 'react';
import {
  CornerDownRight,
  ListEnd,
  Loader2,
  MoreHorizontal,
  Paperclip,
  Trash2,
  TriangleAlert,
} from 'lucide-react';
import { toast } from 'sonner';
import { LAYER_ALIGN, TW_MENU_KIND, TwMenuButton, type TTwMenuEntry } from '@/components/agents/ui';
import { RuntimeMachineContext } from '@/hooks/use-fleet-status';
import {
  removeQueuedPrompt,
  steerQueuedPrompt,
  takeQueuedPromptBack,
  usePromptQueue,
} from '@/hooks/use-prompt-queue';
import type { IQueuedPrompt } from '@/types/server/hl-prompt-queue';
import { t } from '@/lib/i18n';

export type TQueueLaunch = 'starting' | 'failed';

interface IPromptQueueStackProps {
  tabId: string;
  /** «Edit»: the message leaves the queue and its text goes back into the composer. */
  onEdit: (item: IQueuedPrompt) => void;
  /** Why the queue waits when the agent is not working: it is still starting (calm), or its launch gave up. */
  launch?: TQueueLaunch | null;
}

const failed = (what: string) => (error: unknown) =>
  toast.error(what, { description: error instanceof Error ? error.message : undefined });

/**
 * Messages waiting for the agent, stacked on the composer the way Codex shows its queue: each one can be
 * steered in now, dropped, or taken back to edit. The server sends the first one when the agent stops.
 */
const PromptQueueStack = ({ tabId, onEdit, launch = null }: IPromptQueueStackProps) => {
  const machineId = useContext(RuntimeMachineContext);
  const items = usePromptQueue(tabId);
  const [busyId, setBusyId] = useState<string | null>(null);
  if (items.length === 0) return null;

  const run = async (id: string, task: () => Promise<unknown>, error: string) => {
    setBusyId(id);
    await task().catch(failed(error));
    setBusyId(null);
  };
  const edit = (item: IQueuedPrompt) =>
    run(
      item.id,
      async () => {
        const taken = await takeQueuedPromptBack(machineId, tabId, item.id);
        if (taken) onEdit(taken);
      },
      t('composer.promptQueueStack.errorTakeBack'),
    );
  const restoreAll = async () => {
    for (const item of items) await edit(item);
  };
  const starting = launch === 'starting';

  return (
    <ul className="tw-pqueue" aria-label={t('composer.promptQueueStack.queue')}>
      {starting && (
        <li className="tw-pqueue-note" role="status">
          <Loader2 className="tw-delivery-spin" aria-hidden="true" />
          {t('composer.promptQueueStack.startingAgent')}
        </li>
      )}
      {launch === 'failed' && (
        <li className="tw-pqueue-note" data-tone="error" role="alert">
          <TriangleAlert aria-hidden="true" />
          <span>{t('composer.promptQueueStack.agentFailed')}</span>
          <button
            type="button"
            className="tw-pqueue-restore"
            disabled={busyId !== null}
            onClick={() => void restoreAll()}
          >
            {t('composer.promptQueueStack.restore')}
          </button>
        </li>
      )}
      {items.map((item) => {
        const local = item.id.startsWith('local-');
        const menu: TTwMenuEntry[] = [
          {
            kind: TW_MENU_KIND.action,
            id: 'edit',
            label: t('composer.promptQueueStack.edit'),
            onSelect: () => void edit(item),
          },
        ];
        return (
          <li key={item.id} className="tw-pqueue-row" aria-busy={busyId === item.id || local || undefined}>
            <ListEnd className="tw-pqueue-icon" aria-hidden="true" />
            <span className="tw-pqueue-text" title={item.text}>
              {item.text.trim() || t('composer.promptQueueStack.images')}
            </span>
            {item.images.length > 0 && (
              <span
                className="tw-pqueue-files"
                title={t('composer.promptQueueStack.imageCount', { count: item.images.length })}
              >
                <Paperclip aria-hidden="true" />
                {item.images.length}
              </span>
            )}
            <button
              type="button"
              className="tw-pqueue-steer"
              disabled={local || busyId !== null || launch !== null}
              title={
                launch
                  ? t('composer.promptQueueStack.agentStillStarting')
                  : t('composer.promptQueueStack.steerHint')
              }
              onClick={() =>
                void run(
                  item.id,
                  () => steerQueuedPrompt(machineId, tabId, item.id),
                  t('composer.promptQueueStack.errorSteer'),
                )
              }
            >
              <CornerDownRight aria-hidden="true" />
              Steer
            </button>
            <button
              type="button"
              className="tw-icon-button tw-pqueue-icon-button"
              disabled={local || busyId !== null}
              aria-label={t('composer.promptQueueStack.remove')}
              title={t('composer.promptQueueStack.remove')}
              onClick={() =>
                void run(
                  item.id,
                  () => removeQueuedPrompt(machineId, tabId, item.id),
                  t('composer.promptQueueStack.errorRemove'),
                )
              }
            >
              <Trash2 aria-hidden="true" />
            </button>
            <TwMenuButton
              items={menu}
              menuLabel={t('composer.promptQueueStack.queuedMessage')}
              align={LAYER_ALIGN.end}
              className="tw-icon-button tw-pqueue-icon-button"
              aria-label={t('composer.promptQueueStack.more')}
              title={t('composer.promptQueueStack.more')}
              disabled={local || busyId !== null}
            >
              <MoreHorizontal aria-hidden="true" />
            </TwMenuButton>
          </li>
        );
      })}
    </ul>
  );
};

export default PromptQueueStack;
