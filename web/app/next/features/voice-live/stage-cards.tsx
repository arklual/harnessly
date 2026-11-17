// What the voice puts on its stage (voice-stage.ts) when the person asks to see something: the chats with
// who waits for what, a text worth reading, a long reading with its place and its pause.
import type { Conversation } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import { pauseReading, resumeReading, useReading } from '@/next/features/voice-live/reader';
import '@/next/styles/voice-cards.css';

const STATE_KEY: Record<string, string> = {
  'waiting-question': 'next.voice.cards.asks',
  'waiting-permission': 'next.voice.cards.permission',
  working: 'next.voice.cards.working',
  starting: 'next.voice.cards.working',
};

interface IChatsCardProps {
  chats: readonly Conversation[];
  onOpen: (c: Conversation) => void;
}

export const ChatsCard = ({ chats, onOpen }: IChatsCardProps) => (
  <ul className="hx-vcards">
    {chats.map((c) => {
      const key = STATE_KEY[c.state.k];
      return (
        <li key={c.id}>
          <button type="button" className="hx-vcards__row" data-state={c.state.k} onClick={() => onOpen(c)}>
            <span className="hx-vcards__title">{c.title || t('next.voice.cards.untitled')}</span>
            {key && <span className="hx-vcards__state">{t(key)}</span>}
            {(c.recap?.text || c.lastTurn?.snippet) && (
              <span className="hx-vcards__last">{c.recap?.text ?? c.lastTurn?.snippet}</span>
            )}
          </button>
        </li>
      );
    })}
  </ul>
);

export const TextCard = ({ text }: { text: string }) => <div className="hx-vtext">{text}</div>;

/** The reading under way: the parts, the one being said lit, a pause and a «go on». */
export const ReadingCard = ({ onResume }: { onResume: () => void }) => {
  const r = useReading();
  if (!r) return <div className="hx-vtext">{t('next.voice.cards.readDone')}</div>;
  return (
    <div className="hx-vread">
      <div className="hx-vread__bar">
        <span>{t('next.voice.cards.readAt', { at: r.at + 1, of: r.parts.length })}</span>
        {r.paused ? (
          <button
            type="button"
            className="hx-vread__btn"
            onClick={() => {
              resumeReading();
              onResume();
            }}
          >
            {t('next.voice.cards.resume')}
          </button>
        ) : (
          <button type="button" className="hx-vread__btn" onClick={pauseReading}>
            {t('next.voice.cards.pause')}
          </button>
        )}
      </div>
      <div className="hx-vread__text">
        {r.parts.map((p, i) => (
          <p key={i} data-at={i === r.at ? '' : undefined} data-past={i < r.at ? '' : undefined}>
            {p}
          </p>
        ))}
      </div>
    </div>
  );
};
