// The screen of a new chat that waits in the hub for its machine (`/c/queued:<item>`): its first message and
// what it waits for; once the machine has made the chat, the chat itself opens in its place.
import { useEffect } from 'react';
import {
  cancelQueuedChat,
  useQueuedChats,
  type QueuedChat as TQueuedChat,
} from '@/next/commands/queued-chats';
import { QueuedChat, QUEUED_VIEW, type TQueuedView } from '@/next/features/queued-chat/queued-chat';
import { t } from '@/next/i18n';
import { goTo, HOME, ROUTE } from '@/next/routes/routes';
import { NEW_CHAT, setDraftText } from '@/next/stores/drafts';
import { useMachine } from '@/next/stores/machines';
import { toast } from '@/next/ui/toast';
import { machineName } from '@/next/bindings/shell-model';
import '@/next/styles/queued-chat.css';

/** What the screen says of the item: a chat being made (or made, about to open) is starting. */
export const queuedViewOf = (item: TQueuedChat | null): TQueuedView => {
  if (!item) return QUEUED_VIEW.gone;
  if (item.state === 'failed') return QUEUED_VIEW.failed;
  if (item.state === 'queued') return QUEUED_VIEW.waiting;
  return QUEUED_VIEW.starting;
};

export const QueuedChatScreen = ({ itemId }: { itemId: string }) => {
  const items = useQueuedChats();
  const item = items?.find((x) => x.id === itemId) ?? null;
  const view = useMachine(item?.machineId ?? null);
  const convId = item?.convId ?? null;
  useEffect(() => {
    if (convId) goTo({ kind: ROUTE.chat, id: convId }, { replace: true });
  }, [convId]);
  // The hub has not said yet: nothing to draw but the stage.
  if (items === null) return null;
  const cancel = async () => {
    if (!item) return;
    if (!(await cancelQueuedChat(item.id))) {
      toast(t('next.queuedChat.cancelFailed'), { tone: 'warning' });
      return;
    }
    setDraftText(NEW_CHAT, item.text);
    toast(t('next.queuedChat.cancelled'), { tone: 'info' });
    goTo(HOME);
  };
  return (
    <QueuedChat
      view={queuedViewOf(item)}
      text={item?.text ?? null}
      machine={view ? machineName(view.machine) : ''}
      error={item?.error ?? null}
      onCancel={cancel}
    />
  );
};
