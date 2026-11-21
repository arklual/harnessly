// «Что нового»: after the page comes up, a card at the top of the window with one slide per new feature, leafed to the
// right; the untold ones come first, then a few told before. It waits for the page to settle, stays out of the way of
// «Вышла новая версия» (a reload brings the newer card anyway) and never comes back once put away. What was told is
// remembered in this browser; with storage blocked the card shows once per page.
import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import { ENTRIES } from './entries';
import { newsFor, type ICard } from './news';
import { parseSeen, SEEN_KEY, seenAll, type IEntry } from './seen';
import '@/next/styles/whats-new.css';

const EXIT_MS = 280;
const SETTLE_MS = 900;

const readSeen = (): string | null => {
  try {
    return window.localStorage.getItem(SEEN_KEY);
  } catch {
    return null;
  }
};

const writeSeen = (entries: readonly IEntry[]) => {
  try {
    window.localStorage.setItem(SEEN_KEY, JSON.stringify(seenAll(entries)));
  } catch {
    // Storage blocked: the card is gone for this page only.
  }
};

const reducedMotion = () => window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false;

interface IWhatsNewProps {
  /** The page is up (signed in, machines answered). */
  ready: boolean;
  /** «Вышла новая версия» is showing: the card waits for the reload. */
  updating: boolean;
  entries?: readonly IEntry[];
}

const WhatsNew = ({ ready, updating, entries = ENTRIES }: IWhatsNewProps) => {
  const container = usePortalContainer();
  const [cards, setCards] = useState<ICard[] | null>(null);
  const [closed, setClosed] = useState(false);
  const [at, setAt] = useState(0);
  const track = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!ready) return;
    const next = newsFor(entries, parseSeen(readSeen()));
    if (!next) return;
    const timer = window.setTimeout(() => setCards(next), SETTLE_MS);
    return () => window.clearTimeout(timer);
  }, [ready, entries]);

  const open = cards !== null && !closed && !updating;
  const { value, state } = usePresence(open ? cards : null, EXIT_MS);
  const host = container?.current;
  if (!value || !host) return null;

  const last = value.length - 1;
  const close = () => {
    writeSeen(entries);
    setClosed(true);
  };
  const go = (index: number) => {
    const to = Math.min(Math.max(index, 0), last);
    setAt(to);
    const card = track.current?.children[to] as HTMLElement | undefined;
    if (card)
      track.current?.scrollTo?.({ left: card.offsetLeft, behavior: reducedMotion() ? 'auto' : 'smooth' });
  };
  // A swipe or a trackpad moves the strip itself: the dots and the counter follow the card that came to rest.
  const follow = () => {
    const strip = track.current;
    const [first, second] = [strip?.children[0], strip?.children[1]] as (HTMLElement | undefined)[];
    if (!strip || !first) return;
    const step = second ? second.offsetLeft - first.offsetLeft : strip.clientWidth;
    if (step <= 0) return;
    const index = Math.min(Math.max(Math.round(strip.scrollLeft / step), 0), last);
    if (index !== at) setAt(index);
  };

  return createPortal(
    <section
      className="hx-news"
      role="dialog"
      aria-labelledby="hx-news-title"
      data-state={state}
      onKeyDown={(event) => {
        if (event.key === 'Escape') close();
        else if (event.key === 'ArrowRight') go(at + 1);
        else if (event.key === 'ArrowLeft') go(at - 1);
      }}
    >
      <header className="hx-news__head">
        <span className="hx-news__mark">
          <Icon name="sparkles" size="sm" />
        </span>
        <h2 id="hx-news-title" className="hx-news__title">
          {t('next.whatsNew.title')}
        </h2>
        {last > 0 && (
          <span className="hx-news__count">
            {t('next.whatsNew.counter', { n: at + 1, total: value.length })}
          </span>
        )}
        <button type="button" className="hx-news__x" aria-label={t('next.whatsNew.close')} onClick={close}>
          <Icon name="x" size="sm" />
        </button>
      </header>
      <div className="hx-news__track" ref={track} onScroll={follow} data-no-swipe="">
        {value.map((card, index) => (
          <article
            key={card.id}
            className="hx-news__card"
            data-unseen={card.unseen ? '' : undefined}
            aria-label={t('next.whatsNew.counter', { n: index + 1, total: value.length })}
            aria-hidden={index === at ? undefined : true}
          >
            <span className="hx-news__top">
              <span className="hx-news__icon">
                <Icon name={card.icon} />
              </span>
              {card.unseen && <span className="hx-news__fresh">{t('next.whatsNew.fresh')}</span>}
            </span>
            <h3 className="hx-news__name">{t(`next.whatsNew.${card.id}.title`)}</h3>
            <p className="hx-news__text">{t(`next.whatsNew.${card.id}.text`)}</p>
          </article>
        ))}
      </div>
      {last > 0 && (
        <nav className="hx-news__nav">
          <button
            type="button"
            className="hx-news__step"
            aria-label={t('next.whatsNew.prev')}
            disabled={at === 0}
            onClick={() => go(at - 1)}
          >
            <Icon name="chevron-left" size="sm" />
          </button>
          <span className="hx-news__dots" aria-hidden="true">
            {value.map((card, index) => (
              <span
                key={card.id}
                className="hx-news__dot"
                data-on={index === at ? '' : undefined}
                data-unseen={card.unseen ? '' : undefined}
              />
            ))}
          </span>
          <button
            type="button"
            className="hx-news__step"
            aria-label={t('next.whatsNew.next')}
            disabled={at === last}
            onClick={() => go(at + 1)}
          >
            <Icon name="chevron-right" size="sm" />
          </button>
        </nav>
      )}
      <button type="button" className="hx-news__ok" onClick={close}>
        {t('next.whatsNew.ok')}
      </button>
    </section>,
    host,
  );
};

export default WhatsNew;
