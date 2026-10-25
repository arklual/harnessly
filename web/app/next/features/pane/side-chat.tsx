// The side chat: questions about the open chat and their answers as they come; with nothing asked yet, what it is for
// and a few questions to start. Enter asks, the square stops the answer. The same log and field are a tab of the pane
// (.hx-sidedock) or the window over everything (side-float.tsx).
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
}

const SUGGESTIONS = ['done', 'why', 'files', 'cost'] as const;

/** The log and the field, in whatever holds them. */
export const SideChatBody = ({ messages, busy, onAsk, onStop, onRetry }: ISideChatProps) => {
  const [draft, setDraft] = useState('');
  const bodyRef = useRef<HTMLDivElement>(null);
  const last = messages[messages.length - 1];

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
