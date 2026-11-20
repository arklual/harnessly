// The body of a card of the column's stack (a wait of a chat, or its news): it reads its own chat from the store, so the
// stack's `renderAnswer` does not depend on the list of every chat. A callback that changed with each update of any
// chat made every card draw again on each one — during a swipe too.
import { memo } from 'react';
import { NewsBinding } from '@/next/bindings/news-note';
import { WaitBinding } from '@/next/bindings/notification-center';
import WaitRow from '@/next/features/notifications/wait-row';
import { NOTIFICATION_KIND } from '@/next/features/notifications/notification-center';
import { SMART_KIND, type ISmartItem } from '@/next/features/smart-notice/smart-notice';
import type { ConvId } from '@/next/protocol/v2';
import { useConversation } from '@/next/stores/conversations';

/** The news of a chat: a finished turn, a failure, a limit. */
export const NEWS_KINDS: ReadonlySet<string> = new Set([
  SMART_KIND.done,
  SMART_KIND.failed,
  SMART_KIND.limit,
]);

export interface INoteAnswerProps {
  item: ISmartItem;
  onOpenChat: (id: string) => void;
}

const NoteAnswer = ({ item, onOpenChat }: INoteAnswerProps) => {
  const chatId = item.chatId as ConvId;
  const conv = useConversation(chatId);
  // The chat is not here (gone, or not read yet): the card still says what it is and leads to it.
  if (!conv)
    return (
      <WaitRow detail={{ k: 'plain', text: '' }} label={item.text} onOpenChat={() => onOpenChat(chatId)} />
    );
  const open = () => onOpenChat(conv.id);
  if (NEWS_KINDS.has(item.kind)) return <NewsBinding conv={conv} item={item} onOpenChat={open} />;
  return (
    <WaitBinding
      conv={conv}
      item={{
        id: item.key,
        kind: NOTIFICATION_KIND.waiting,
        icon: item.icon,
        title: item.title,
        text: item.text,
        at: item.at,
        unread: true,
        chatId: item.chatId,
      }}
      onOpenChat={open}
    />
  );
};

export default memo(NoteAnswer);
