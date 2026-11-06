// The hands-on tour over the real product: a ring around the element a step is about and a card beside it,
// never over it nor over the menu or panel the step opened. The step is counted when the product shows it was done;
// the ring and the card follow the element as the page moves. Nothing here blocks the page: the person clicks through.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { placeCard, type IRect, type ITourStep } from '@/next/features/tour/model';
import { nextLesson } from '@/next/features/learn/lessons';
import { trackOf } from '@/next/features/tour/tracks';
import { goTo, ROUTE } from '@/next/routes/routes';
import { t } from '@/next/i18n';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { finishTour, leaveTour, startTour, toStep, tourSignals, useTour } from '@/next/stores/tour';
import '@/next/styles/tour.css';

const TOUR_PARAM = 'tour';
/** The lesson the page's address asked for when it loaded. */
const ASKED = new URL(window.location.href).searchParams.get(TOUR_PARAM);
const TICK_MS = 250;
const DONE_MS = 750;
const EXIT_MS = 180;

/** The open menus, lists and dialogs: the card keeps off them too. */
const POPUPS = '[role=menu],[role=listbox],[role=dialog]:not(.hx-tour-card)';

const rectOf = (el: Element): IRect => {
  const b = el.getBoundingClientRect();
  return { x: b.left, y: b.top, w: b.width, h: b.height };
};

const visible = (r: IRect) => r.w > 0 && r.h > 0 && r.y < window.innerHeight && r.y + r.h > 0;

const findTarget = (step: ITourStep): Element | null => {
  for (const selector of step.target) {
    const el = document.querySelector(selector);
    if (el && visible(rectOf(el))) return el;
  }
  return null;
};

interface IFrame {
  ring: IRect | null;
  popups: IRect[];
}

const sameRect = (a: IRect | null, b: IRect | null) =>
  a === b || (!!a && !!b && Math.abs(a.x - b.x) < 1 && Math.abs(a.y - b.y) < 1 && a.w === b.w && a.h === b.h);

/** Where the step's element is and what is open around it, measured every tick while the step is on. */
const useFrame = (step: ITourStep | null): IFrame => {
  const [frame, setFrame] = useState<IFrame>({ ring: null, popups: [] });
  useEffect(() => {
    if (!step) return;
    const measure = () => {
      const el = findTarget(step);
      const ring = el ? rectOf(el) : null;
      const popups = [...document.querySelectorAll(POPUPS)].map(rectOf).filter(visible);
      setFrame((was) =>
        sameRect(was.ring, ring) && was.popups.length === popups.length ? was : { ring, popups },
      );
    };
    measure();
    const timer = setInterval(measure, TICK_MS);
    window.addEventListener('resize', measure);
    return () => {
      clearInterval(timer);
      window.removeEventListener('resize', measure);
    };
  }, [step]);
  return frame;
};

/** True once the step was done (the product showed it), checked every tick. */
const useDone = (step: ITourStep | null, key: string): boolean => {
  const [done, setDone] = useState<string | null>(null);
  useEffect(() => {
    if (!step?.done) return;
    const check = step.done;
    const timer = setInterval(() => {
      if (
        check({
          path: window.location.pathname + window.location.search,
          find: (s) => document.querySelector(s),
          count: (s) => document.querySelectorAll(s).length,
          signals: tourSignals(),
        })
      ) {
        setDone(key);
        clearInterval(timer);
      }
    }, TICK_MS);
    return () => clearInterval(timer);
  }, [step, key]);
  return done === key;
};

interface ICardProps {
  track: string;
  index: number;
  total: number;
  step: ITourStep | null;
  done: boolean;
  frame: IFrame;
  closing: boolean;
}

const Card = ({ track, index, total, step, done, frame, closing }: ICardProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const passed = step === null;

  // The card's place goes to CSS through custom properties: measured after layout, where the card is its own size.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const p = placeCard(
      passed ? null : frame.ring,
      { w: el.offsetWidth, h: el.offsetHeight },
      { w: window.innerWidth, h: window.innerHeight },
      frame.popups,
    );
    el.style.setProperty('--tour-x', `${Math.round(p.x)}px`);
    el.style.setProperty('--tour-y', `${Math.round(p.y)}px`);
    el.dataset.side = p.side;
  });

  const key = `next.tour.${track}`;
  return (
    <div
      ref={ref}
      className="hx-tour-card"
      role="dialog"
      aria-live="polite"
      aria-label={passed ? t('next.tour.passed') : t(`${key}.${step.id}.title`)}
      data-state={closing ? 'closing' : 'open'}
      data-done={done || undefined}
    >
      <div className="hx-tour-card__top">
        <span className="hx-tour-card__track">{t(`${key}.name`)}</span>
        <button
          type="button"
          className="hx-tour-card__x"
          aria-label={t('next.tour.leave')}
          onClick={leaveTour}
        >
          <Icon name="x" size="sm" />
        </button>
      </div>
      {passed ? (
        <>
          <h3 className="hx-tour-card__title">{t('next.tour.passed')}</h3>
          <p className="hx-tour-card__text">{t(`${key}.outro`)}</p>
          <div className="hx-tour-card__actions">
            <button
              type="button"
              className="hx-tour-card__link"
              onClick={() => {
                finishTour();
                goTo({ kind: ROUTE.learn });
              }}
            >
              {t('next.tour.all')}
            </button>
            <span className="hx-spacer" />
            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                // The introduction leads into the first lesson; a lesson into the one after it.
                const next = track === 'welcome' ? 'chat' : nextLesson(track).id;
                finishTour();
                startTour(next);
              }}
            >
              {t('next.tour.nextLesson')}
            </Button>
          </div>
        </>
      ) : (
        <>
          <h3 className="hx-tour-card__title">
            {done && <Icon name="check" size="sm" />}
            {t(`${key}.${step.id}.title`)}
          </h3>
          <p className="hx-tour-card__text">{t(`${key}.${step.id}.text`)}</p>
          <div className="hx-tour-card__bar" aria-hidden="true">
            {Array.from({ length: total }, (_, i) => (
              <i
                key={i}
                data-on={i < index || (i === index && done) || undefined}
                data-now={i === index || undefined}
              />
            ))}
          </div>
          <div className="hx-tour-card__actions">
            {index > 0 && (
              <button type="button" className="hx-tour-card__link" onClick={() => toStep(index - 1)}>
                {t('next.tour.back')}
              </button>
            )}
            <span className="hx-spacer" />
            <button type="button" className="hx-tour-card__link" onClick={() => toStep(index + 1)}>
              {t('next.tour.skip')}
            </button>
            {(!step.done || step.manual) && !done && (
              <Button variant="primary" size="sm" onClick={() => toStep(index + 1)}>
                {step.look ? t('next.tour.next') : t('next.tour.did')}
              </Button>
            )}
          </div>
        </>
      )}
    </div>
  );
};

/** The tour, when one runs; mounted once at the shell's root. */
const TourOverlay = () => {
  const tour = useTour();
  // A link can start a lesson: `?tour=site` (the docs point here); the address forgets it. The ask is read as the page
  // loads, since the shell may move the address to the last open tab before this mounts.
  useEffect(() => {
    if (!ASKED) return;
    const url = new URL(window.location.href);
    if (url.searchParams.has(TOUR_PARAM)) {
      url.searchParams.delete(TOUR_PARAM);
      window.history.replaceState(window.history.state, '', url.toString());
    }
    if (trackOf(ASKED)) startTour(ASKED);
  }, []);
  const track = trackOf(tour.track);
  const total = track?.steps.length ?? 0;
  const step = track && tour.step < total ? track.steps[tour.step] : null;
  const key = `${tour.track}:${tour.step}`;
  const done = useDone(step, key);
  const frame = useFrame(step);

  // A done step stays on screen with its check for a moment, then the next one comes.
  useEffect(() => {
    if (!done) return;
    const timer = setTimeout(() => toStep(tour.step + 1), DONE_MS);
    return () => clearTimeout(timer);
  }, [done, tour.step]);

  const shown = usePresence(track ? key : null, EXIT_MS);
  if (!shown.value || !track) return null;
  const ring = step ? frame.ring : null;
  return createPortal(
    <div className="hx-tour" data-state={shown.state}>
      {ring && <RingBox rect={ring} />}
      <Card
        track={track.id}
        index={tour.step}
        total={total}
        step={step}
        done={done}
        frame={frame}
        closing={shown.state === 'closing'}
      />
    </div>,
    document.body,
  );
};

const RING_PAD = 6;

const RingBox = ({ rect }: { rect: IRect }) => {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.setProperty('--ring-x', `${Math.round(rect.x - RING_PAD)}px`);
    el.style.setProperty('--ring-y', `${Math.round(rect.y - RING_PAD)}px`);
    el.style.setProperty('--ring-w', `${Math.round(rect.w + RING_PAD * 2)}px`);
    el.style.setProperty('--ring-h', `${Math.round(rect.h + RING_PAD * 2)}px`);
  }, [rect]);
  return <div ref={ref} className="hx-tour-ring" aria-hidden="true" />;
};

export default TourOverlay;
