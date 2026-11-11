// The side chat: questions about the open chat and their answers as they come; with nothing asked yet, what it is for
// and a few questions to start. Enter asks, the square stops the answer. The same log and field are a tab of the pane
// (.hx-sidedock) or the window over everything (side-float.tsx). Words picked in the chat and sent here («Ask aside»,
// stores/side-ask.ts) come into the field as a quote from the main chat, after what is typed there, with the caret
// after them for the question.
import { useEffect, useRef, useState } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/side-chat.css';

export interface ISideMessage {
  id: string;
  who: 'me' | 'agent';
  text: string;
  /** The answer is still coming. */
  typing: boolean;
  /** Stopped before its end, or failed: the words of why. */
  cut: string | null;
}

export interface ISideChatProps {
  messages: readonly ISideMessage[];
  busy: boolean;
  onAsk: (question: string) => void;
  onStop: () => void;
  onRetry: (id: string) => void;
  /** Words of the main chat for the field, as a quote; taken once (by their nonce). */
  quote?: { quote: string; nonce: number } | null;
  /** The field has the quote. */
  onQuoted?: (nonce: number) => void;
}

/** The words as a quote of the main chat, a question to write after them. */
export const quoteBlock = (words: string): string =>
  `${t('next.side.quoteFrom')}\n${words
    .split('\n')
    .map((line) => `> ${line}`)
    .join('\n')}\n\n`;

const SUGGESTIONS = ['done', 'why', 'files', 'cost'] as const;

/** The log and the field, in whatever holds them. */
export const SideChatBody = ({ messages, busy, onAsk, onStop, onRetry, quote, onQuoted }: ISideChatProps) => {
  const [draft, setDraft] = useState('');
  const bodyRef = useRef<HTMLDivElement>(null);
  const fieldRef = useRef<HTMLTextAreaElement>(null);
  const last = messages[messages.length - 1];

  // A quote sent here goes after what is typed (in the render that brings it), then the store lets it go and the caret
  // goes after it, for the question.
  const [taken, setTaken] = useState<number | null>(null);
  if (quote && quote.nonce !== taken) {
    setTaken(quote.nonce);
    const block = quoteBlock(quote.quote);
    const kept = draft.trimEnd();
    setDraft(kept === '' ? block : `${kept}\n\n${block}`);
  }
  useEffect(() => {
    if (taken === null) return;
    onQuoted?.(taken);
    // After the render that puts the words in.
    requestAnimationFrame(() => {
      const field = fieldRef.current;
      if (!field) return;
      field.focus();
      field.setSelectionRange(field.value.length, field.value.length);
      field.scrollTop = field.scrollHeight;
    });
  }, [taken, onQuoted]);

  useEffect(() => {
    const el = bodyRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages.length, last?.text]);

  const ask = (text = draft) => {
    const question = text.trim();
    if (!question || busy) return;
    setDraft('');
    onAsk(question);
  };

  return (
    <>
      <div className="hx-sidechat__body" ref={bodyRef}>
        {messages.length === 0 && (
          <div className="hx-sidechat__intro">
            <p>{t('next.side.intro')}</p>
            <div className="hx-sidechat__sugg">
              {SUGGESTIONS.map((key, i) => (
                <button
                  key={key}
                  type="button"
                  className="hx-chip"
                  data-i={i}
                  onClick={() => ask(t(`next.side.sugg.${key}`))}
                >
                  {t(`next.side.sugg.${key}`)}
                </button>
              ))}
            </div>
          </div>
        )}
        {messages.map((m) => (
          <div key={m.id} className="hx-sidechat__msg" data-who={m.who}>
            {m.typing && m.text === '' && (
              <span className="hx-sidechat__dots">
                <i />
                <i />
                <i />
              </span>
            )}
            {m.text}
            {m.cut !== null && (
              <span className="hx-sidechat__cut">
                {m.cut} ·{' '}
                <button type="button" onClick={() => onRetry(m.id)}>
                  {t('next.side.retry')}
                </button>
              </span>
            )}
          </div>
        ))}
      </div>
      <div className="hx-sidechat__foot">
        <textarea
          ref={fieldRef}
          className="hx-sidechat__input"
          rows={1}
          autoFocus
          placeholder={t('next.side.placeholder')}
          value={draft}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault();
              ask();
            }
          }}
        />
        {busy ? (
          <button
            type="button"
            className="hx-sidechat__send"
            data-stop="true"
            aria-label={t('next.side.stop')}
            onClick={onStop}
          >
            <Icon name="square" size="sm" />
          </button>
        ) : (
          <button
            type="button"
            className="hx-sidechat__send"
            aria-label={t('next.side.ask')}
            disabled={!draft.trim()}
            onClick={() => ask()}
          >
            <Icon name="arrow-up" size="sm" />
          </button>
        )}
      </div>
    </>
  );
};

const SideChat = (props: ISideChatProps) => (
  <div className="hx-sidedock">
    <SideChatBody {...props} />
  </div>
);

export default SideChat;
