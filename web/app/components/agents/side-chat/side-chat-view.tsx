import { useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent } from 'react';
import { CornerDownLeft, Eraser, MessageCirclePlus, Square } from 'lucide-react';
import SideChatExcerptChips from '@/components/agents/side-chat/side-chat-excerpt-chips';
import SideChatMessage from '@/components/agents/side-chat/side-chat-message';
import { MAX_QUESTION_CHARS } from '@/lib/hl-side-chat';
import {
  askSideChat,
  cancelSideChat,
  clearSideChat,
  removeSideChatExcerpt,
  retrySideChat,
  useSideChat,
} from '@/lib/hl-side-chat-store';
import { t } from '@/lib/i18n';

interface ISideChatViewProps {
  tabId: string | null;
  sessionName: string | null;
  provider: string;
}

const SUPPORTED_PROVIDER = 'claude';
const MAX_FIELD_HEIGHT = 160;
const NOTE = t('sideChat.sideChatView.note');
const NOTE_SHORT = t('sideChat.sideChatView.noteShort');
const UNSUPPORTED = t('sideChat.sideChatView.unsupported');

/**
 * A quick question about the session of a conversation tab: the answer comes from a fork that sees the whole
 * context and is never saved, so the main conversation does not change. Lives in memory only and goes with the page.
 */
const SideChatView = ({ tabId, sessionName, provider }: ISideChatViewProps) => {
  const { messages, excerpts } = useSideChat(tabId);
  const [draft, setDraft] = useState('');
  const listRef = useRef<HTMLOListElement>(null);
  const fieldRef = useRef<HTMLTextAreaElement>(null);
  const fieldId = useId();
  const supported = provider === SUPPORTED_PROVIDER;
  const enabled = supported && tabId !== null;
  const busy = messages.some((message) => message.status === 'streaming');
  const last = messages.at(-1);

  useEffect(() => {
    const list = listRef.current;
    if (list) list.scrollTop = list.scrollHeight;
  }, [messages.length, last?.answer, last?.status]);

  useLayoutEffect(() => {
    const field = fieldRef.current;
    if (!field) return;
    field.style.height = 'auto';
    field.style.height = `${Math.min(field.scrollHeight, MAX_FIELD_HEIGHT)}px`;
  }, [draft]);

  useEffect(() => {
    if (excerpts.length) fieldRef.current?.focus();
  }, [excerpts.length]);

  const send = () => {
    if (!tabId || !draft.trim() || busy) return;
    if (askSideChat(tabId, draft)) setDraft('');
  };
  const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return;
    event.preventDefault();
    send();
  };

  return (
    <section className="tw-sidechat" aria-label="Side chat">
      <header className="tw-sidechat-head">
        <p className="tw-sidechat-note" data-compact={messages.length > 0 || undefined} title={NOTE}>
          {!supported ? UNSUPPORTED : messages.length > 0 ? NOTE_SHORT : NOTE}
          {supported && sessionName && messages.length === 0 && (
            <span className="tw-sidechat-session">
              {t('sideChat.sideChatView.session', { name: sessionName })}
            </span>
          )}
        </p>
        {messages.length > 0 && tabId && (
          <button
            type="button"
            className="tw-sidechat-clear"
            onClick={() => clearSideChat(tabId)}
            title={t('sideChat.sideChatView.clearLabel')}
            aria-label={t('sideChat.sideChatView.clearLabel')}
          >
            <Eraser aria-hidden="true" />
            <span>{t('sideChat.sideChatView.clear')}</span>
          </button>
        )}
      </header>

      {messages.length === 0 ? (
        <div className="tw-sidechat-empty">
          <MessageCirclePlus aria-hidden="true" />
          <h3>Side chat</h3>
          <p>{t('sideChat.sideChatView.ephemeral')}</p>
        </div>
      ) : (
        <ol
          ref={listRef}
          className="tw-sidechat-list"
          role="log"
          aria-live="polite"
          aria-label={t('sideChat.sideChatView.log')}
        >
          {messages.map((message) => (
            <SideChatMessage
              key={message.id}
              message={message}
              canRetry={!busy && tabId !== null}
              onRetry={(id) => tabId && retrySideChat(tabId, id)}
            />
          ))}
        </ol>
      )}

      <div className="tw-sidechat-composer" data-disabled={!enabled || undefined}>
        {tabId && (
          <SideChatExcerptChips excerpts={excerpts} onRemove={(id) => removeSideChatExcerpt(tabId, id)} />
        )}
        <div className="tw-sidechat-field">
          <label htmlFor={fieldId} className="sr-only">
            {t('sideChat.sideChatView.fieldLabel')}
          </label>
          <textarea
            id={fieldId}
            ref={fieldRef}
            className="tw-sidechat-input"
            rows={1}
            value={draft}
            maxLength={MAX_QUESTION_CHARS}
            disabled={!enabled}
            placeholder={t('sideChat.sideChatView.placeholder')}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={onKeyDown}
          />
          {busy && tabId ? (
            <button
              type="button"
              className="tw-sidechat-send"
              data-stop=""
              onClick={() => cancelSideChat(tabId)}
              aria-label={t('sideChat.sideChatView.stopLabel')}
              title={t('sideChat.sideChatView.stop')}
            >
              <Square aria-hidden="true" />
            </button>
          ) : (
            <button
              type="button"
              className="tw-sidechat-send"
              onClick={send}
              disabled={!enabled || !draft.trim()}
              aria-label={t('sideChat.sideChatView.sendLabel')}
              title={t('sideChat.sideChatView.send')}
            >
              <CornerDownLeft aria-hidden="true" />
            </button>
          )}
        </div>
      </div>
    </section>
  );
};

export default SideChatView;
