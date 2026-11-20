// A finished turn, a failure or a limit of a chat the person is not looking at, as a card of the column: what
// the agent said, in a line or two, and for a finished turn a field to write back right there. The message goes the way
// the composer's goes (sendMessage), and the card leaves once it is sent. «Открыть чат» is the quiet link.
import { sendMessage } from '@/next/commands';
import WaitRow, { type TWaitDetail } from '@/next/features/notifications/wait-row';
import { SMART_KIND, type ISmartItem } from '@/next/features/smart-notice/smart-notice';
import { STATUS_LOOK } from '@/next/features/sidebar/status';
import { t } from '@/next/i18n';
import type { Conversation } from '@/next/protocol/v2';
import { addHistory } from '@/next/stores/notice-history';
import { dismissNotice } from '@/next/stores/notices';

/** How long the card stays after the reply went, ms: enough to see the field empty again. */
const SENT_MS = 900;

/** The gist of the news: the start of what the agent said; when that is only the status's own word, its recap. */
export const essenceOf = (item: Pick<ISmartItem, 'text' | 'summary'>): string => {
  const generic = item.text.trim() === '' || item.text === STATUS_LOOK.done.label;
  const first = item.summary?.split('\n').find((line) => line.trim() !== '') ?? '';
  return generic && first !== '' ? first : item.text;
};

export interface INewsBindingProps {
  conv: Conversation;
  item: ISmartItem;
  onOpenChat: () => void;
}

export const NewsBinding = ({ conv, item, onOpenChat }: INewsBindingProps) => {
  const text = essenceOf(item);
  const detail: TWaitDetail = item.kind === SMART_KIND.done ? { k: 'reply', text } : { k: 'plain', text };
  const onText = async (said: string) => {
    sendMessage(conv.machineId, { conversationId: conv.id, text: said });
    addHistory({
      key: `${item.key}:${Date.now()}`,
      chatId: conv.id,
      icon: 'arrow-up',
      title: item.title,
      text: t('next.smart.history.reply', { text: said }),
      at: Date.now(),
    });
    window.setTimeout(() => dismissNotice(item.key), SENT_MS);
  };
  return (
    <WaitRow
      detail={detail}
      onOpenChat={onOpenChat}
      onText={item.kind === SMART_KIND.done ? onText : undefined}
    />
  );
};
