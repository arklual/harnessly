import { RotateCcw } from 'lucide-react';
import AssistantMessageItem from '@/components/timeline/assistant-message-item';
import SideChatExcerptChips from '@/components/agents/side-chat/side-chat-excerpt-chips';
import type { ISideChatMessage } from '@/lib/hl-side-chat-store';
import { t } from '@/lib/i18n';

interface ISideChatMessageProps {
  message: ISideChatMessage;
  canRetry: boolean;
  onRetry: (id: string) => void;
}

/** One question of the side chat and the answer under it, rendered like an answer of the agent in the timeline. */
const SideChatMessage = ({ message, canRetry, onRetry }: ISideChatMessageProps) => {
  const { id, question, excerpts, answer, status, error } = message;
  return (
    <li className="tw-sidechat-turn" data-status={status}>
      <div className="tw-sidechat-question">
        <SideChatExcerptChips excerpts={excerpts} />
        <p>{question}</p>
      </div>
      {answer && (
        <AssistantMessageItem
          entry={{ id, type: 'assistant-message', timestamp: 0, markdown: answer }}
          showActions={false}
        />
      )}
      {status === 'streaming' && (
        <p className="tw-sidechat-status" role="status">
          <span className="tw-sidechat-dots" aria-hidden="true">
            <i />
            <i />
            <i />
          </span>
          {t('sideChat.sideChatMessage.answering')}
        </p>
      )}
      {status === 'stopped' && (
        <p className="tw-sidechat-status" data-tone="muted">
          {t('sideChat.sideChatMessage.stopped')}
        </p>
      )}
      {status === 'error' && (
        <p className="tw-sidechat-status" data-tone="error" role="alert">
          <span>{error}</span>
          {canRetry && (
            <button type="button" className="tw-sidechat-retry" onClick={() => onRetry(id)}>
              <RotateCcw aria-hidden="true" />
              {t('sideChat.sideChatMessage.retry')}
            </button>
          )}
        </p>
      )}
    </li>
  );
};

export default SideChatMessage;
