import { useEffect, useRef } from 'react';
import useTimelineSnapshotStore from '@/hooks/use-timeline-snapshot-store';
import { AlertCircle, MessageSquare, RefreshCw, RotateCcw } from 'lucide-react';
import { useStickToBottom } from 'use-stick-to-bottom';
import { Button } from '@/components/ui/button';
import Spinner from '@/components/ui/spinner';
import AgentAvatar from '@/components/timeline/agent-avatar';
import AssistantMessageItem from '@/components/timeline/assistant-message-item';
import ElapsedTime from '@/components/timeline/elapsed-time';
import ScrollToBottomButton from '@/components/timeline/scroll-to-bottom-button';
import UserMessageItem from '@/components/timeline/user-message-item';
import useHlChat from '@/hooks/use-hl-chat';
import { shortModelName, type IChatAssistantEntry } from '@/lib/hl-openwebui';
import ChatComposer from './chat-composer';
import { t } from '@/lib/i18n';

export interface IChatViewProps {
  chatId: string | null;
  onChatCreated: (chat: { id: string; title: string }) => void;
  onTitleChange?: (id: string, title: string) => void;
  /** Sent once as the first message of a new chat (the welcome composer hands it over). */
  initialPrompt?: string;
}

const ChatErrorNotice = ({ message, onRetry }: { message: string; onRetry?: () => void }) => (
  <div
    role="alert"
    className="ml-0 mt-1 flex items-start gap-2.5 rounded-xl border border-negative/30 bg-negative/5 px-3.5 py-2.5 text-xs text-foreground sm:ml-[39px]"
  >
    <AlertCircle size={14} className="mt-0.5 shrink-0 text-negative" />
    <span className="flex-1 leading-relaxed">{message}</span>
    {onRetry && (
      <button
        type="button"
        onClick={onRetry}
        className="flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 font-medium text-foreground/80 hover:bg-muted hover:text-foreground"
      >
        <RotateCcw size={12} />
        {t('chat.chatView.retry')}
      </button>
    )}
  </div>
);

const ChatView = ({ chatId, onChatCreated, onTitleChange, initialPrompt }: IChatViewProps) => {
  const chat = useHlChat(chatId, { onChatCreated, onTitleChange });
  const { scrollRef, contentRef, scrollToBottom, isAtBottom } = useStickToBottom({
    initial: 'instant',
    resize: 'smooth',
  });
  const lastEntry = chat.entries.at(-1);
  const lastAssistantId = lastEntry?.type === 'assistant-message' ? lastEntry.id : null;
  const streamingEntry = chat.streamingId
    ? (chat.entries.find((entry) => entry.id === chat.streamingId) as IChatAssistantEntry | undefined)
    : undefined;
  const modelLabel = shortModelName(chat.model);
  const isEmpty = !chat.loading && !chat.loadError && chat.entries.length === 0;

  useEffect(() => {
    if (chat.streamingId) void scrollToBottom('smooth');
  }, [chat.streamingId, scrollToBottom]);

  const initialSent = useRef(false);
  useEffect(() => {
    if (initialSent.current || chatId || !initialPrompt || chat.loading || !chat.model) return;
    initialSent.current = true;
    void chat.send(initialPrompt);
  }, [chat, chatId, initialPrompt]);

  useEffect(() => {
    if (chatId)
      useTimelineSnapshotStore
        .getState()
        .publish(`chat:${chatId}`, { entries: chat.entries, jsonlPath: null, sessionId: chatId });
  }, [chatId, chat.entries]);

  const handleSend = async (text: string) => {
    const accepted = await chat.send(text);
    if (accepted) void scrollToBottom('smooth');
    return accepted;
  };

  if (chat.loadError) {
    return (
      <div className="agent-timeline flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
        <AlertCircle className="text-negative" size={22} />
        <p className="max-w-sm text-sm text-muted-foreground">{chat.loadError}</p>
        <Button variant="outline" size="sm" onClick={chat.reload}>
          <RefreshCw size={13} className="mr-1.5" />
          {t('chat.chatView.retry')}
        </Button>
      </div>
    );
  }

  return (
    <div className="agent-timeline flex h-full min-h-0 flex-col">
      <div className="relative flex min-h-0 flex-1 flex-col">
        <div
          ref={scrollRef}
          className="relative min-h-0 flex-1 overflow-y-auto"
          role="log"
          aria-label={t('chat.chatView.messages')}
          aria-busy={chat.streaming}
          tabIndex={0}
        >
          <div ref={contentRef} className="mx-auto max-w-content">
            {chat.loading && (
              <div className="flex items-center justify-center gap-2 py-16 text-xs text-muted-foreground">
                <Spinner size={14} />
                {t('chat.chatView.loading')}
              </div>
            )}
            {isEmpty && (
              <div className="flex flex-col items-center px-6 pb-8 pt-[18vh] text-center">
                <span className="mb-5 inline-grid h-12 w-12 place-items-center rounded-2xl border border-border bg-muted/60 text-brand/70">
                  <MessageSquare size={22} />
                </span>
                <h1 className="mb-2 text-2xl font-medium tracking-tight text-foreground">
                  {t('chat.chatView.emptyTitle')}
                </h1>
                <p className="max-w-sm text-sm leading-relaxed text-muted-foreground">
                  {t('chat.chatView.emptyHint', { model: modelLabel })}
                </p>
              </div>
            )}
            {!chat.loading &&
              chat.entries.map((entry) => (
                <div key={entry.id} data-entry-id={entry.id} className="px-4 py-1.5">
                  {entry.type === 'user-message' ? (
                    <UserMessageItem entry={entry} />
                  ) : (
                    (entry.markdown || (!entry.error && entry.id !== chat.streamingId)) && (
                      <AssistantMessageItem
                        entry={entry}
                        provider="chat"
                        label={shortModelName(
                          chat.models.find((model) => model.id === entry.model),
                          entry.modelName,
                        )}
                        showActions={entry.done && entry.id !== chat.streamingId}
                        emptyText={t('chat.chatView.stoppedEmpty')}
                        actions={
                          entry.id === lastAssistantId && !chat.streaming ? (
                            <button
                              type="button"
                              onClick={chat.regenerate}
                              aria-label={t('chat.chatView.regenerate')}
                            >
                              <RotateCcw size={13} />
                              <span>{t('chat.chatView.again')}</span>
                            </button>
                          ) : null
                        }
                      />
                    )
                  )}
                  {entry.type === 'assistant-message' && entry.error && (
                    <ChatErrorNotice
                      message={entry.error}
                      onRetry={entry.id === lastAssistantId && !chat.streaming ? chat.regenerate : undefined}
                    />
                  )}
                </div>
              ))}
            {chat.streaming && (
              <div className="agent-working" role="status">
                <AgentAvatar provider="chat" working />
                <span>
                  <span className="agent-working-title">
                    {streamingEntry?.markdown
                      ? t('chat.chatView.writing', { model: modelLabel })
                      : t('chat.chatView.thinking', { model: modelLabel })}
                  </span>
                  <span className="agent-working-hint animate-shimmer">
                    {streamingEntry?.markdown
                      ? t('chat.chatView.writingHint')
                      : t('chat.chatView.thinkingHint')}
                  </span>
                </span>
                {streamingEntry && (
                  <span aria-hidden="true" className="tabular-nums">
                    <ElapsedTime since={streamingEntry.timestamp} />
                  </span>
                )}
              </div>
            )}
          </div>
        </div>
        <ScrollToBottomButton
          visible={!isAtBottom && !chat.loading && chat.entries.length > 0}
          onClick={() => void scrollToBottom('smooth')}
        />
      </div>
      <ChatComposer
        models={chat.models}
        model={chat.model}
        effort={chat.effort}
        effortOptions={chat.effortOptions}
        streaming={chat.streaming}
        disabled={chat.loading}
        savingParams={chat.savingParams}
        autoFocus={isEmpty}
        onSend={handleSend}
        onStop={() => void chat.stop()}
        onModelChange={chat.selectModel}
        onEffortChange={(value) => void chat.selectEffort(value)}
      />
    </div>
  );
};

export default ChatView;
