import { t } from '@/lib/i18n';
import { memo } from 'react';
import { ArrowDownLeft, ArrowUpRight, ChevronRight, MessagesSquare } from 'lucide-react';
import PeerChip from '@/components/agents/peer-chip';
import type { IPeerMessage } from '@/lib/hl-artifacts';
import { cn } from '@/lib/utils';

const PeerRow = ({ message }: { message: IPeerMessage }) => {
  const Arrow = message.direction === 'out' ? ArrowUpRight : ArrowDownLeft;
  return (
    <li>
      <details className={cn('tw-peer-row', `tw-peer-row-${message.direction}`)}>
        <summary>
          <span
            className="tw-peer-row-dir"
            title={
              message.direction === 'out' ? t('inspector.peersTab.sent') : t('inspector.peersTab.received')
            }
          >
            <Arrow />
          </span>
          <span className="tw-peer-row-main">
            <span className="tw-peer-row-head">
              <PeerChip name={message.peer} from={message.from} />
              <time>
                {new Date(message.timestamp).toLocaleTimeString('ru-RU', {
                  hour: '2-digit',
                  minute: '2-digit',
                })}
              </time>
            </span>
            <span className="tw-peer-row-preview">
              {message.text
                .split('\n')
                .find((line) => line.trim())
                ?.slice(0, 160)}
            </span>
          </span>
          <ChevronRight className="tw-artifact-more-chevron" />
        </summary>
        <p className="tw-peer-row-text">{message.text}</p>
      </details>
    </li>
  );
};

/** What this agent and other sessions sent each other: handoffs, plans, agreements. */
const PeersTab = ({ messages }: { messages: IPeerMessage[] }) => {
  if (messages.length === 0)
    return (
      <div className="tw-inspector-empty">
        <MessagesSquare />
        <p>{t('inspector.peersTab.empty')}</p>
      </div>
    );
  return (
    <ul className="tw-peer-list">
      {messages.map((message) => (
        <PeerRow key={message.id} message={message} />
      ))}
    </ul>
  );
};

export default memo(PeersTab);
