// The side chat: questions about the open chat and their answers as they come; with nothing asked yet, what it is for
// and a few questions to start. Enter asks, the square stops the answer. The same log and field are a tab of the pane
// (.hx-sidedock) or the window over everything (side-float.tsx). Words picked in the chat and sent here («Ask aside»,
// stores/side-ask.ts) come into the field as a quote from the main chat, after what is typed there, with the caret
// after them for the question.
import { useEffect, useRef, type RefObject } from 'react';
import Composer, { type IComposerProps } from '@/next/features/composer/composer';
import DockBar from '@/next/features/composer/dock-bar';
import type { DockNotice } from '@/next/features/composer/types';
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

/** The field of the side chat: the chat's own composer, taking words only. Its words are kept by the host. */
export interface ISideField {
  text: string;
  setText: (value: string) => void;
  /** What the field holds now, outside a render. */
  read: () => string;
  inputRef: RefObject<HTMLTextAreaElement | null>;
  maxText: number;
  enterSends: boolean;
  /** The voice input as the composer takes it; no microphone where the machine has none. */
  voice: Pick<
    IComposerProps,
    'recording' | 'recognized' | 'caret' | 'onMic' | 'onCancelRecording' | 'onFinishRecording'
  >;
  /** What the voice input says over the field. */
  notice: DockNotice | null;
}

export interface ISideChatProps {
  messages: readonly ISideMessage[];
  busy: boolean;
  field: ISideField;
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

const noFiles = () => undefined;

/** The log and the field, in whatever holds them. */
export const SideChatBody = ({
  messages,
  busy,
  field,
  onAsk,
  onStop,
  onRetry,
  quote,
  onQuoted,
}: ISideChatProps) => {
  const bodyRef = useRef<HTMLDivElement>(null);
  const { text, setText, read, inputRef } = field;
  const last = messages[messages.length - 1];

  // A quote sent here goes after what is typed, then the store lets it go and the caret goes after it, for the
  // question.
  const nonce = quote?.nonce ?? null;
  const words = quote?.quote ?? '';
  // Taking the quote empties the store, so the effect runs again at once: the caret's frame outlives that run and is
  // dropped only by the next quote or when the field goes away.
  const caretFrame = useRef(0);
  useEffect(() => {
    const frame = caretFrame;
    return () => cancelAnimationFrame(frame.current);
  }, []);
  useEffect(() => {
    if (nonce === null) return;
    const block = quoteBlock(words);
    const kept = read().trimEnd();
    setText(kept === '' ? block : `${kept}\n\n${block}`);
    onQuoted?.(nonce);
    // After the render that puts the words in.
    cancelAnimationFrame(caretFrame.current);
    caretFrame.current = requestAnimationFrame(() => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(el.value.length, el.value.length);
      el.scrollTop = el.scrollHeight;
    });
  }, [nonce, words, setText, read, onQuoted, inputRef]);

  useEffect(() => {
    inputRef.current?.focus();
  }, [inputRef]);

  useEffect(() => {
    const el = bodyRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages.length, last?.text]);

  const ask = (asked: string) => {
    const question = asked.trim();
    if (!question || busy) return;
    onAsk(question);
  };
  // What is typed while an answer comes stays in the field: the side chat takes one question at a time.
  const send = () => {
    if (!text.trim() || busy) return;
    ask(text);
    setText('');
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
      <div className="hx-sidechat__foot hx-dock" data-screen>
        <DockBar notice={field.notice} />
        <Composer
          plain
          value={text}
          onChange={setText}
          onSubmit={send}
          mode={busy ? 'working' : 'idle'}
          placeholder={t('next.side.placeholder')}
          onStop={() => Promise.resolve(onStop())}
          picker={null}
          attachments={[]}
          onFiles={noFiles}
          onRemoveAttachment={noFiles}
          onRetryAttachment={noFiles}
          {...field.voice}
          maxText={field.maxText}
          enterSends={field.enterSends}
          inputRef={inputRef}
        />
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
