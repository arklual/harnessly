import { Send } from 'lucide-react';
import PeerChip from '@/components/agents/peer-chip';
import { ClampedText } from '@/components/timeline/user-message-item';
import type { ITimelineToolCall } from '@/types/timeline';
import { t } from '@/lib/i18n';

/** What this agent wrote to another session: the mirror of the incoming peer card. */
const PeerMessageOutItem = ({
  call,
}: {
  call: ITimelineToolCall & { peerMessage: { to: string; text: string } };
}) => (
  <div className="tw-wrapped tw-wrapped-peer tw-wrapped-out">
    <div className="tw-wrapped-head">
      <Send />
      <span>{t('timeline.peerMessageOutItem.messageTo')}</span>
      <PeerChip name={call.peerMessage.to} />
    </div>
    <ClampedText text={call.peerMessage.text} lines={6} />
  </div>
);

export default PeerMessageOutItem;
