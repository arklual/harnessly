import { useEffect, useRef, type ReactNode } from 'react';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { usePresence } from '@/next/ui/presence';

/** How long the project chip stays after it is cleared, leaving. */
const INTO_EXIT_MS = 200;

export interface IGreetingProps {
  /** The person's name; without it the question goes without one. */
  name: string | null;
  onResume?: () => void;
  /**
   * The composer, from the composer feature. It takes the greeting and stands it over its field, so the two stay
   * together however far the field grows (the greeting never goes under it).
   */
  composer?: (hello: ReactNode) => ReactNode;
  /** The project the new chat starts in, when one was picked; the cross clears it. */
  into?: { name: string; onClear: () => void } | null;
  /** The page opens from a chat: its field rises up under the greeting; from any other screen it stands in place. */
  fromChat?: boolean;
  /** The machine's setup card («Настройка машины»), under the greeting. */
  setup?: ReactNode;
  /**
   * A view of an app that takes the stage under the field (the page of the day it brings): the greeting and the field go
   * up to the top and the view fills the rest.
   */
  stage?: ReactNode;
  /** The greeting that view put in place of the question; null keeps the host's. */
  greeting?: { title: string; sub: string } | null;
}

/** The start page (`/`): the question in the middle of the stage, the way to continue a terminal session, the field. */
const Greeting = ({
  name,
  onResume,
  composer,
  into = null,
  fromChat = false,
  setup = null,
  stage = null,
  greeting = null,
}: IGreetingProps) => {
  const chip = usePresence(into, INTO_EXIT_MS);
  const stageRef = useRef<HTMLDivElement>(null);
  const hasStage = stage !== null;
  // The view of the stage starts where the dock ends (the field and the tools under it), however tall they grow.
  useEffect(() => {
    const el = stageRef.current;
    const dock = el?.querySelector<HTMLElement>(':scope > .hx-dock');
    if (!hasStage || !el || !dock) return;
    const place = () => el.style.setProperty('--hx-home-under', `${dock.offsetTop + dock.offsetHeight}px`);
    place();
    const watch = new ResizeObserver(place);
    watch.observe(dock);
    watch.observe(el);
    return () => {
      watch.disconnect();
      el.style.removeProperty('--hx-home-under');
    };
  }, [hasStage]);
  const hello = (
    <div className="hx-hello" data-dock={composer ? '' : undefined}>
      <div className="hx-enter" data-enter="up" data-eb="420">
        <div className="hx-hello__title">
          {greeting?.title ?? (name ? t('next.greeting.title', { name }) : t('next.greeting.titleNoName'))}
        </div>
        {(greeting === null || greeting.sub !== '') && (
          <div className="hx-hello__sub">{greeting?.sub ?? t('next.greeting.sub')}</div>
        )}
        {chip.value && (
          <div className="hx-hello__into" data-state={chip.state}>
            <Icon name="folder" size="sm" />
            <span className="hx-hello__into-name">
              {t('next.newChat.inProject', { project: chip.value.name })}
            </span>
            <button
              type="button"
              className="hx-hello__into-x"
              aria-label={t('next.newChat.inProjectClear')}
              title={t('next.newChat.inProjectClear')}
              onClick={chip.value.onClear}
            >
              <Icon name="x" size="sm" />
            </button>
          </div>
        )}
        {onResume && (
          <button type="button" className="hx-hello__resume" onClick={onResume}>
            <Icon name="history" size="sm" />
            {t('next.greeting.resume')}
          </button>
        )}
      </div>
      {setup}
    </div>
  );
  return (
    <div
      ref={stageRef}
      className="hx-stage hx-enter"
      data-enter="fade"
      data-eb="200"
      data-ed="700"
      data-phase="home"
      data-back={fromChat ? 'chat' : undefined}
      data-home={stage ? 'stage' : undefined}
    >
      {stage}
      {composer ? composer(hello) : hello}
    </div>
  );
};

export default Greeting;
