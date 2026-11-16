import { useEffect, useRef, type ReactNode } from 'react';
import LookBackdrop from '@/next/features/looks/backdrop';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { usePresence } from '@/next/ui/presence';

/** How long the project chip stays after it is cleared, leaving. */
const INTO_EXIT_MS = 200;
/** How long the field takes to come up to the stage's bottom (the design's 560 ms). */
const DOCK_MOVE_MS = 560;

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
  /** The greeting that view put in place of the question; null keeps the host's. Its second line may carry links. */
  greeting?: { title: string; sub: ReactNode } | null;
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
  // The view of the stage takes the whole stage and its page starts where the dock ends (the field and the tools under
  // it), however tall they grow. As in the design, the greeting and the field scroll away with the page; once the field
  // has gone over the top it comes up at the stage's bottom and waits there, and goes back under the greeting when the
  // page is scrolled back up.
  useEffect(() => {
    const el = stageRef.current;
    const dock = el?.querySelector<HTMLElement>(':scope > .hx-dock');
    if (!hasStage || !el || !dock) return;
    const hello = dock.querySelector<HTMLElement>(':scope > .hx-hello');
    let under = 0;
    const place = () => {
      el.style.setProperty('--hx-home-dock', `${dock.offsetHeight}px`);
      // Docked at the bottom the dock stands elsewhere: the page keeps the place measured under the greeting.
      if (el.dataset.docked !== undefined) return;
      // On a narrow screen the field goes down as far as the greeting over it needs (a second line, the way back to a
      // terminal session), so its first line is never cut; then the page starts where the dock ends.
      if (hello) el.style.setProperty('--hx-home-hello', `${hello.offsetHeight}px`);
      under = dock.offsetTop + dock.offsetHeight;
      el.style.setProperty('--hx-home-under', `${under}px`);
    };
    let frame = 0;
    let settle: ReturnType<typeof setTimeout> | undefined;
    const dockTo = (down: boolean) => {
      if ((el.dataset.docked !== undefined) === down) return;
      clearTimeout(settle);
      cancelAnimationFrame(frame);
      if (!down) {
        delete el.dataset.docked;
        delete el.dataset.dockpre;
        delete el.dataset.dockmove;
        return;
      }
      // The field comes up from under the stage's bottom edge, as the design's does.
      el.dataset.docked = '';
      el.dataset.dockpre = '';
      frame = requestAnimationFrame(() => {
        frame = requestAnimationFrame(() => {
          delete el.dataset.dockpre;
          el.dataset.dockmove = '';
          settle = setTimeout(() => delete el.dataset.dockmove, DOCK_MOVE_MS);
        });
      });
    };
    // The page of the stage view says where it is scrolled: the one scroller inside the view that scrolls the page.
    const onScroll = (e: Event) => {
      const page = e.target;
      if (!(page instanceof HTMLElement) || !page.matches('[data-home-scroll]')) return;
      const y = Math.max(0, page.scrollTop);
      el.style.setProperty('--hx-home-scroll', `${Math.min(y, under)}px`);
      dockTo(y > under);
    };
    place();
    const watch = new ResizeObserver(place);
    watch.observe(dock);
    watch.observe(el);
    if (hello) watch.observe(hello);
    el.addEventListener('scroll', onScroll, true);
    return () => {
      watch.disconnect();
      el.removeEventListener('scroll', onScroll, true);
      clearTimeout(settle);
      cancelAnimationFrame(frame);
      delete el.dataset.docked;
      delete el.dataset.dockpre;
      delete el.dataset.dockmove;
      for (const v of ['--hx-home-under', '--hx-home-hello', '--hx-home-dock', '--hx-home-scroll'])
        el.style.removeProperty(v);
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
      <LookBackdrop />
      {stage}
      {composer ? composer(hello) : hello}
    </div>
  );
};

export default Greeting;
