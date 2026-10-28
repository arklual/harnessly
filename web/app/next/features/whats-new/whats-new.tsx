// «Что нового»: a card at the top of the window, once per release, after the page comes up. It waits for the page to
// settle, stays out of the way of «Вышла новая версия» (a reload brings the newer card anyway) and never comes back
// once put away. What was seen is remembered in this browser; with storage blocked the card shows once per page.
import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import { RELEASES, unseenRelease, type IRelease } from './releases';
import '@/next/styles/whats-new.css';

const SEEN_KEY = 'hx:whats-new';
const EXIT_MS = 280;
const SETTLE_MS = 900;

const readSeen = (): string | null => {
  try {
    return window.localStorage.getItem(SEEN_KEY);
  } catch {
    return null;
  }
};

const writeSeen = (id: string) => {
  try {
    window.localStorage.setItem(SEEN_KEY, id);
  } catch {
    // Storage blocked: the card is gone for this page only.
  }
};

interface IWhatsNewProps {
  /** The page is up (signed in, machines answered). */
  ready: boolean;
  /** «Вышла новая версия» is showing: the card waits for the reload. */
  updating: boolean;
  releases?: readonly IRelease[];
}

const WhatsNew = ({ ready, updating, releases = RELEASES }: IWhatsNewProps) => {
  const container = usePortalContainer();
  const [release, setRelease] = useState<IRelease | null>(null);
  const [closed, setClosed] = useState(false);

  useEffect(() => {
    if (!ready) return;
    const next = unseenRelease(releases, readSeen());
    if (!next) return;
    const timer = window.setTimeout(() => setRelease(next), SETTLE_MS);
    return () => window.clearTimeout(timer);
  }, [ready, releases]);

  const open = release !== null && !closed && !updating;
  const { value, state } = usePresence(open ? release : null, EXIT_MS);
  const host = container?.current;
  if (!value || !host) return null;

  const close = () => {
    writeSeen(value.id);
    setClosed(true);
  };

  return createPortal(
    <section
      className="hx-news"
      role="dialog"
      aria-labelledby="hx-news-title"
      data-state={state}
      onKeyDown={(event) => {
        if (event.key === 'Escape') close();
      }}
    >
      <header className="hx-news__head">
        <span className="hx-news__mark">
          <Icon name="sparkles" size="sm" />
        </span>
        <h2 id="hx-news-title" className="hx-news__title">
          {t('next.whatsNew.title')}
        </h2>
        <button type="button" className="hx-news__x" aria-label={t('next.whatsNew.close')} onClick={close}>
          <Icon name="x" size="sm" />
        </button>
      </header>
      <ul className="hx-news__list">
        {value.items.map((item) => (
          <li key={item.key} className="hx-news__item">
            <span className="hx-news__icon">
              <Icon name={item.icon} size="sm" />
            </span>
            <span className="hx-news__body">
              <span className="hx-news__name">{t(`next.whatsNew.${value.id}.${item.key}.title`)}</span>
              <span className="hx-news__text">{t(`next.whatsNew.${value.id}.${item.key}.text`)}</span>
            </span>
          </li>
        ))}
      </ul>
      <button type="button" className="hx-news__ok" onClick={close}>
        {t('next.whatsNew.ok')}
      </button>
    </section>,
    host,
  );
};

export default WhatsNew;
