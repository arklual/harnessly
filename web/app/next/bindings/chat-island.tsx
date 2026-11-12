// The field at the foot of a page that stands beside a chat (the code review), wired to that chat.
import AppIsland from '@/next/features/app-main/app-island';
import { lastLineOf } from '@/next/features/designly/last-line';
import { useFeedItems } from '@/next/bindings/feed-items';
import { useCentralFeed } from '@/next/bindings/use-central';
import { settled } from '@/next/bindings/outcome';
import { interrupt, sendMessage } from '@/next/commands';
import { useConversationChannel } from '@/next/core/realtime/hub';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import { useConversation } from '@/next/stores/conversations';
import { useAgentLimits } from '@/next/bindings/use-limits';
import LimitsChip from '@/next/features/limits/limits-chip';
import { limitsProviderOf } from '@/next/features/limits/model';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

/** The field at the foot, wired to the chat: the agent's last line, whether it works, and the words it sends. */
export const ChatIsland = ({
  machine,
  conv,
  onExpand,
}: {
  machine: MachineId;
  conv: ConvId;
  onExpand: () => void;
}) => {
  // The chat's own view is not mounted under the folded field: its feed is read from here.
  useConversationChannel(machine, conv);
  useCentralFeed(machine, conv);
  const items = useFeedItems(conv, machine);
  const record = useConversation(conv);
  const state = record?.state.k;
  // The same limits as the full field's chip: the subscription's windows and their card.
  const limits = useAgentLimits(
    machine,
    limitsProviderOf(record ? { provider: record.provider, profile: record.model?.profile } : null),
  );
  let last: string | null = null;
  for (let i = (items?.length ?? 0) - 1; i >= 0; i -= 1) {
    const item = items?.[i];
    if (item?.kind === 'turn' && item.answer) {
      last = lastLineOf(item.answer.md);
      break;
    }
  }
  return (
    <AppIsland
      last={last}
      busy={state === 'starting' || state === 'working'}
      onSend={(text) => sendMessage(machine, { conversationId: conv, text })}
      onStop={() =>
        void settled(interrupt(machine, conv)).catch((e: Error) =>
          toast(t('next.appmain.stopFailed'), { tone: 'error', description: e.message }),
        )
      }
      onExpand={onExpand}
      limits={limits && <LimitsChip view={limits} />}
    />
  );
};
