// The welcome of a first launch (HRN-792): once per browser, on the greeting, when the person has no chat yet and
// «Что нового» is not on screen, a card at the top offers the one-minute introduction (a tour over the real greeting) or the lessons.
// It blocks nothing: the page stays clickable, and any answer puts it away for good.
import { useEffect, useState } from 'react';
import { createPortal } from 'react-dom';
import { doLesson } from '@/next/features/learn/do-lesson';
import { t } from '@/next/i18n';
import { goTo, ROUTE, useRoute } from '@/next/routes/routes';
import { useTour } from '@/next/stores/tour';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import '@/next/styles/learn.css';

const SEEN_KEY = 'hx:learn-welcome';
const EXIT_MS = 280;
const SETTLE_MS = 1200;
const TICK_MS = 500;
/** «Что нового» goes first: the welcome waits until it is closed. */
const NEWS = '.hx-news';

const seen = (): boolean => {
  try {
    return window.localStorage.getItem(SEEN_KEY) !== null;
  } catch {
    return true;
  }
};

const markSeen = () => {
  try {
    window.localStorage.setItem(SEEN_KEY, '1');
  } catch {
    // Storage that throws: the card goes for this page.
  }
};

const POINTS = [
  { icon: 'message-square', key: 'p1' },
  { icon: 'palette', key: 'p2' },
  { icon: 'app-window', key: 'p3' },
] as const;

interface ILearnWelcomeProps {
  /** The page is up and the chats are listed: none means a first launch. */
  fresh: boolean;
}

const LearnWelcome = ({ fresh }: ILearnWelcomeProps) => {
  const container = usePortalContainer();
  const tour = useTour();
  // Only over the greeting: on an open design or chat it would lie over what the person is working with.
  const home = useRoute().kind === ROUTE.home;
  const [due, setDue] = useState(false);
  const [closed, setClosed] = useState(false);

  useEffect(() => {
    if (!fresh || seen()) return;
    let timer = 0;
    const wait = () => {
      timer = window.setTimeout(() => (document.querySelector(NEWS) ? wait() : setDue(true)), TICK_MS);
    };
    timer = window.setTimeout(wait, SETTLE_MS);
    return () => window.clearTimeout(timer);
  }, [fresh]);

  const open = due && !closed && home && tour.track === null;
  const { value, state } = usePresence(open ? true : null, EXIT_MS);
  const host = container?.current;
  if (!value || !host) return null;

  const close = () => {
    markSeen();
    setClosed(true);
  };

  return createPortal(
    <section
      className="hx-learn-hello"
      role="dialog"
      aria-labelledby="hx-learn-hello-title"
      data-state={state}
      onKeyDown={(event) => {
        if (event.key === 'Escape') close();
      }}
    >
      <button type="button" className="hx-learn-hello__x" aria-label={t('next.learn.close')} onClick={close}>
        <Icon name="x" size="sm" />
      </button>
      <h2 id="hx-learn-hello-title" className="hx-learn-hello__title">
        {t('next.learn.welcome.title')}
      </h2>
      <p className="hx-learn-hello__text">{t('next.learn.welcome.text')}</p>
      <ul className="hx-learn-hello__list">
        {POINTS.map((p) => (
          <li key={p.key}>
            <span className="hx-learn-hello__ic">
              <Icon name={p.icon} size="sm" />
            </span>
            <span>
              <b>{t(`next.learn.welcome.${p.key}`)}</b>
              <span>{t(`next.learn.welcome.${p.key}.sub`)}</span>
            </span>
          </li>
        ))}
      </ul>
      <div className="hx-learn-hello__acts">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            close();
            goTo({ kind: ROUTE.learn });
          }}
        >
          {t('next.learn.welcome.all')}
        </Button>
        <span className="hx-spacer" />
        <Button
          variant="primary"
          size="sm"
          icon="arrow-right"
          onClick={() => {
            close();
            doLesson('welcome');
          }}
        >
          {t('next.learn.welcome.go')}
        </Button>
      </div>
    </section>,
    host,
  );
};

export default LearnWelcome;
