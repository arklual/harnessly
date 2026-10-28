// The scene «Показ»: the slide fills the screen (the shell sets it up), on the right the presenter's panel with the
// clock, the next slide and the script, below it the capsule with the pager, the pointer, «Записать» and «Завершить».
// The pointer and the recording are for a desktop; on a phone only the pager and «Завершить» stay.
import { useRef, useState } from 'react';
import { PACE, formatClock, pace, slideBudgets } from '@/next/features/designly/deck/clock';
import DeckMini from '@/next/features/designly/deck/deck-mini';
import { useDeckPlay, type IDeckPlayHost } from '@/next/features/designly/deck/deck-play';
import { createPrompter } from '@/next/features/designly/deck/prompter';
import { useDeckClock } from '@/next/features/designly/deck/use-deck-clock';
import { useDeckLaser } from '@/next/features/designly/deck/use-deck-laser';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { useMedia } from '@/next/ui/use-media';

/** The script is spoken at about this many words a second (the notes' own pace). */
const WORDS_PER_SECOND = 2.2;
export const SHOW_PHONE_QUERY = '(max-width: 560px)';

export interface IDeckShowProps {
  host: IDeckPlayHost;
  /** The minutes the talk was planned for. */
  minutes: number;
  onExit: () => void;
  onRecord: () => void;
}

const DeckShow = ({ host, minutes, onExit, onRecord }: IDeckShowProps) => {
  const phone = useMedia(SHOW_PHONE_QUERY);
  const { slides, index } = host;
  const slide = slides[index];
  const upcoming = slides[index + 1];
  const [laser, setLaser] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const dot = useRef<HTMLDivElement>(null);
  const seconds = useDeckClock(true);

  useDeckLaser(host, laser && !phone, root, dot);
  const { next, prev } = useDeckPlay(host, { onExit, onRecord: phone ? undefined : onRecord });

  // when the person came to this slide, and the line they picked by hand on it
  const [arrival, setArrival] = useState({ index, at: seconds });
  const [picked, setPicked] = useState<{ index: number; line: number } | null>(null);
  if (arrival.index !== index) setArrival({ index, at: seconds });

  const prompter = createPrompter(slide?.notes ?? '');
  const spoken = Math.floor((seconds - arrival.at) * WORDS_PER_SECOND);
  const timed = prompter.lines.findIndex((line) => spoken < line.to);
  const current = picked?.index === index ? picked.line : timed < 0 ? prompter.lines.length - 1 : timed;

  const budgets = slideBudgets(
    slides.map((s) => s.seconds),
    minutes,
  );
  const state = pace(seconds, index, budgets);

  return (
    <div className="hx-deckm-play" data-scene="show" ref={root}>
      <aside className="hx-deckm-side" aria-label={t('next.deck.show.side')}>
        <div className="hx-deckm-clock">
          <span className="hx-deckm-clock__now">{formatClock(seconds)}</span>
          {minutes > 0 && <span>{t('next.deck.show.of', { m: minutes })}</span>}
          <em data-pace={state}>
            {t(state === PACE.behind ? 'next.deck.show.behind' : 'next.deck.show.onTime')}
          </em>
        </div>
        <div className="hx-deckm-next">
          {upcoming ? (
            <>
              <div className="hx-deckm-next__mini">
                <DeckMini
                  src={host.frameSrc(upcoming.path)}
                  w={upcoming.w}
                  h={upcoming.h}
                  title={upcoming.title}
                />
              </div>
              <div>
                <small>{t('next.deck.show.next', { n: index + 2 })}</small>
                <span>{upcoming.title}</span>
              </div>
            </>
          ) : (
            <div>
              <small>—</small>
              <span>{t('next.deck.show.end')}</span>
            </div>
          )}
        </div>
        <div className="hx-deckm-script">
          {prompter.lines.map((line, i) => (
            <p key={line.from} data-on={i === current ? '1' : '0'}>
              <button type="button" onClick={() => setPicked({ index, line: i })}>
                {line.text}
              </button>
            </p>
          ))}
        </div>
      </aside>
      <div className="hx-deckm-laser" ref={dot} hidden={!laser || phone} />
      <div className="hx-deckm-cap hx-deckm-cap--show">
        <button type="button" onClick={prev} aria-label={t('next.deck.show.back')}>
          <Icon name="chevron-left" size="sm" />
        </button>
        <span className="hx-deckm-time">
          <small>
            {index + 1} / {slides.length}
          </small>
        </span>
        <button type="button" onClick={next} aria-label={t('next.deck.show.forward')}>
          <Icon name="chevron-right" size="sm" />
        </button>
        <i className="hx-deckm-cap__sep hx-deckm-desk" />
        <button
          className="hx-deckm-desk"
          type="button"
          data-on={laser ? '1' : '0'}
          aria-pressed={laser}
          onClick={() => setLaser((on) => !on)}
        >
          <span className="hx-deckm-cap__dot" data-tone="laser" />
          <span className="hx-deckm-lbl">{t('next.deck.show.laser')}</span>
        </button>
        <button className="hx-deckm-desk" type="button" onClick={onRecord}>
          <span className="hx-deckm-cap__dot" />
          <span className="hx-deckm-lbl">{t('next.deck.show.record')}</span>
        </button>
        <i className="hx-deckm-cap__sep" />
        <button type="button" data-v="done" onClick={onExit}>
          {t('next.deck.show.finish')}
          <span className="hx-deckm-kbd">Esc</span>
        </button>
      </div>
    </div>
  );
};

export default DeckShow;
