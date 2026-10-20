import { useCallback, useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { ArrowUp, CloudOff } from 'lucide-react';
import TimelineView from '@/components/timeline/timeline-view';
import OfflineQueueList from '@/components/agents/offline-queue-list';
import useOfflineQueue from '@/hooks/use-offline-queue';
import useTimeline from '@/hooks/use-timeline';
import { clearInputDraft, loadInputDraft, saveInputDraft } from '@/hooks/use-web-input';
import { sessionOfTranscript } from '@/lib/machine-reach';
import type { Conversation } from '@/lib/hl-conversations';
import { t } from '@/lib/i18n';

/**
 * A conversation opened while its machine was already down, drawn by the shell from what the cluster stored:
 * the timeline from the database, a composer whose messages wait in the cluster and go to the machine once it answers.
 * A conversation already on screen when the machine goes keeps its own view instead; the draft is the same
 * one the live composer keeps, so text carries over either way.
 */

const clock = (at: number) =>
  new Date(at).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' });

interface IOfflineConversationProps {
  conversation: Conversation;
  machineName: string;
  /** When the machine was first seen down, if the page knows. */
  since: number | null;
}

const OfflineConversation = ({ conversation, machineName, since }: IOfflineConversationProps) => {
  const machineId = conversation.machineId ?? null;
  const provider = conversation.provider === 'codex' ? 'codex' : 'claude';
  const sessionId = sessionOfTranscript(conversation.jsonlPath);
  const timeline = useTimeline({
    sessionName: '',
    agentSessionId: sessionId,
    panelType: provider === 'codex' ? 'codex-cli' : 'claude-code',
    machineId,
    enabled: Boolean(machineId && sessionId),
  });

  const [text, setText] = useState(() => loadInputDraft(conversation.id));
  const [error, setError] = useState('');
  const [sending, setSending] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  useEffect(() => saveInputDraft(conversation.id, text), [conversation.id, text]);

  const { items, queue, drop, retry } = useOfflineQueue(machineId, conversation.id, true);

  const send = useCallback(async () => {
    const message = text.trim();
    if (!message || sending) return;
    setSending(true);
    setError('');
    try {
      await queue(message);
      setText('');
      clearInputDraft(conversation.id);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : t('agents.offlineConversation.queueFailed'));
    } finally {
      setSending(false);
      input.current?.focus();
    }
  }, [text, sending, queue, conversation.id]);

  const retryItem = useCallback(
    (item: Parameters<typeof retry>[0]) => {
      retry(item).catch((failure: unknown) =>
        setError(failure instanceof Error ? failure.message : t('agents.offlineConversation.queueFailed')),
      );
    },
    [retry],
  );

  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      void send();
    }
  };

  return (
    <div className="tw-offline">
      <div className="tw-offline-banner" role="status">
        <CloudOff aria-hidden />
        <span>
          <b>{machineName}</b>{' '}
          {since
            ? t('agents.offlineConversation.unavailableSince', { time: clock(since) })
            : t('agents.offlineConversation.unavailable')}
        </span>
      </div>
      <div className="tw-offline-timeline">
        {sessionId ? (
          <TimelineView
            provider={provider}
            entries={timeline.entries}
            tasks={timeline.tasks}
            sessionId={timeline.sessionId}
            initMeta={timeline.initMeta}
            sessionStats={timeline.sessionStats}
            cliState="idle"
            wsStatus={timeline.wsStatus}
            isLoading={timeline.isLoading}
            error={timeline.error}
            onRetry={timeline.retrySession}
            onLoadMore={timeline.loadMore}
            hasMore={timeline.hasMore}
            jsonlPath={timeline.jsonlPath}
          />
        ) : (
          <p className="tw-offline-empty">{t('agents.offlineConversation.noHistory')}</p>
        )}
      </div>
      <div className="tw-offline-bottom">
        <OfflineQueueList items={items} onRetry={retryItem} onDrop={(item) => void drop(item)} />
        <div className="agent-composer tw-offline-composer">
          <textarea
            ref={input}
            className="agent-prompt-editor tw-offline-editor"
            value={text}
            rows={2}
            placeholder={t('agents.offlineConversation.placeholder', { machine: machineName })}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={onKeyDown}
          />
          <div className="tw-cbar">
            <div className="tw-cbar-start">
              {error && (
                <span className="tw-composer-note" data-tone="error" role="alert">
                  {error}
                </span>
              )}
            </div>
            <div className="tw-cbar-end">
              <button
                type="button"
                className="tw-csend"
                aria-label={t('agents.offlineConversation.queue')}
                disabled={!text.trim() || sending}
                onClick={() => void send()}
              >
                <ArrowUp aria-hidden />
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default OfflineConversation;
