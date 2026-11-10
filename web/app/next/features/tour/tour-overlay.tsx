// The hands-on tour over the real product: a ring around the element a step is about and a card beside it,
// never over it nor over the menu or panel the step opened. The step is counted when the product shows it was done;
// the ring and the card follow the element as the page moves. Nothing here blocks the page: the person clicks through.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { placeCard, type IRect, type ITourStage, type ITourStep } from '@/next/features/tour/model';
import { nextLesson } from '@/next/features/learn/lessons';
import { trackOf } from '@/next/features/tour/tracks';
import { goTo, parseRoute, ROUTE } from '@/next/routes/routes';
import { t } from '@/next/i18n';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { libraryControl } from '@/next/stores/library-view';
import {
  finishTour,
  leaveTour,
  startTour,
  stepBack,
  toStep,
  tourFacts,
  tourSignals,
  useTour,
} from '@/next/stores/tour';
import type { ITourEnv } from '@/next/features/tour/model';
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

/** The ring of a step about an element of the design's page (tour-anchor.tsx), while the page shows it. */
const ANCHOR = '.hx-tour-anchor[data-place="shown"]';
/** A design that is still loading (a site's loader, a deck's or a video's slide with no board yet): the step waits. */
const LOADING = '.hx-site-load, .hx-deck__slide[data-loading], .hx-video__slide[data-loading]';

const envNow = (): ITourEnv => ({
  path: window.location.pathname + window.location.search,
  find: (s) => document.querySelector(s),
  count: (s) => document.querySelectorAll(s).length,
  signals: tourSignals(),
  facts: tourFacts(),
});

const findTarget = (step: ITourStep, stage: ITourStage | null): Element | null => {
  // On the way to the step's element the ring is on the click that leads there.
  const selectors = stage ? stage.target : step.frame ? [ANCHOR, ...step.target] : step.target;
  for (const selector of selectors) {
    const el = document.querySelector(selector);
    if (el && visible(rectOf(el))) return el;
  }
  return null;
};

/** The element is there but out of the view (a card far down the library, in a scrolled list): bring it in. */
const scrolled = new WeakSet<Element>();
const bringIntoView = (step: ITourStep, stage: ITourStage | null) => {
  const selectors = stage ? stage.target : step.frame ? step.target : step.target;
  for (const selector of selectors) {
    const el = document.querySelector(selector);
    if (!el) continue;
    const r = rectOf(el);
    if (r.w <= 0 || r.h <= 0) continue;
    if (visible(r)) return;
    if (!scrolled.has(el)) {
      scrolled.add(el);
      el.scrollIntoView({ block: 'center', behavior: 'smooth' });
    }
    return;
  }
};

interface IFrame {
  ring: IRect | null;
  popups: IRect[];
}

const sameRect = (a: IRect | null, b: IRect | null) =>
  a === b || (!!a && !!b && Math.abs(a.x - b.x) < 1 && Math.abs(a.y - b.y) < 1 && a.w === b.w && a.h === b.h);

/** Where the step's element is and what is open around it, measured every tick while the step is on. */
const useFrame = (step: ITourStep | null, stage: ITourStage | null): IFrame => {
  const [frame, setFrame] = useState<IFrame>({ ring: null, popups: [] });
  useEffect(() => {
    if (!step) return;
    const measure = () => {
      bringIntoView(step, stage);
      const el = findTarget(step, stage);
      const ring = el ? rectOf(el) : null;
      const avoid = (step.avoid ?? []).flatMap((sel) => [...document.querySelectorAll(sel)]);
      const popups = [...document.querySelectorAll(POPUPS), ...avoid].map(rectOf).filter(visible);
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
  }, [step, stage]);
  return frame;
};

/** Whether the step was done (the product showed it, checked every tick), and whether it was done already when the
 *  person came back to it by «Назад»: then it stays, with a «Дальше», instead of leaving again at once. */
const useDone = (step: ITourStep | null, key: string, back: boolean): { done: boolean; held: boolean } => {
  const [done, setDone] = useState<string | null>(null);
  const [held, setHeld] = useState<string | null>(null);
  useEffect(() => {
    if (!step?.done) return;
    const check = step.done;
    let armed = !back;
    const timer = setInterval(() => {
      const ok = check(envNow());
      if (!armed) {
        // Undone again (the person left the page): from now on it counts when done.
        if (ok) setHeld(key);
        else {
          armed = true;
          setHeld(null);
        }
        return;
      }
      if (ok) {
        setDone(key);
        clearInterval(timer);
      }
    }, TICK_MS);
    return () => clearInterval(timer);
  }, [step, key, back]);
  return { done: done === key, held: held === key };
};

/** What stands between the person and the step: they left its page, its design is loading, a step before is undone. */
export const STEP_STATE = {
  on: 'on',
  away: 'away',
  loading: 'loading',
  needs: 'needs',
  skip: 'skip',
} as const;
export type TStepState = (typeof STEP_STATE)[keyof typeof STEP_STATE];

const stateOf = (step: ITourStep | null): TStepState => {
  if (!step) return STEP_STATE.on;
  const env = envNow();
  if (step.skip?.(env)) return STEP_STATE.skip;
  if (step.where && !step.where.test(env.path)) return STEP_STATE.away;
  if (step.needs && !step.needs.test(env)) return STEP_STATE.needs;
  const loader = env.find(LOADING);
  if (loader && rectOf(loader).w > 0) return STEP_STATE.loading;
  return STEP_STATE.on;
};

const useStepState = (step: ITourStep | null): TStepState => {
  const [state, setState] = useState<TStepState>(() => stateOf(step));
  useEffect(() => {
    const tick = () => setState(stateOf(step));
    tick();
    const timer = setInterval(tick, TICK_MS);
    return () => clearInterval(timer);
  }, [step]);
  return state;
};

/** The way to the step's element, when the library hides it; the same object while nothing about it changed. */
const useStage = (step: ITourStep | null): ITourStage | null => {
  const [stage, setStage] = useState<ITourStage | null>(null);
  const read = step?.stage;
  useEffect(() => {
    if (!read) return;
    const tick = () => {
      const next = read(envNow());
      const same = (a: ITourStage | null, b: ITourStage | null) =>
        a === b ||
        (!!a &&
          !!b &&
          a.id === b.id &&
          a.target.join('|') === b.target.join('|') &&
          JSON.stringify(a.vars) === JSON.stringify(b.vars));
      setStage((was) => (same(was, next) ? was : next));
    };
    tick();
    const timer = setInterval(tick, TICK_MS);
    return () => clearInterval(timer);
  }, [read]);
  // A step with no way of its own has none, whatever the step before left.
  return read ? stage : null;
};

/** What the card says of the step: its text, or why it waits (the person left its page, a step before is undone). */
const stepText = (key: string, step: ITourStep, state: TStepState, stage: ITourStage | null): string => {
  if (stage && state === STEP_STATE.on) return t(`next.tour.stage.${stage.id}`, stage.vars);
  if (state === STEP_STATE.away) return t('next.tour.away', { step: t(`${key}.${step.id}.title`) });
  if (state === STEP_STATE.needs) return t(`${key}.${step.id}.needs`);
  return t(`${key}.${step.id}.text`);
};

interface IStepActionProps {
  track: string;
  index: number;
  step: ITourStep;
  done: boolean;
  /** Done already when the person came back to it. */
  held: boolean;
  state: TStepState;
  stage: ITourStage | null;
}

/** The card's main button: the way back to the step, to the step it needs, or «Done» / «Next» where the product cannot
 *  see the step. None while the design loads or once the step is done. */
const StepAction = ({ track, index, step, done, held, state, stage }: IStepActionProps) => {
  if (done || state === STEP_STATE.loading) return null;
  if (state === STEP_STATE.away) {
    const back = step.back;
    return back ? (
      <Button variant="primary" size="sm" onClick={() => goTo(parseRoute(back))}>
        {t('next.tour.return')}
      </Button>
    ) : null;
  }
  if (state === STEP_STATE.needs && step.needs) {
    const to = trackOf(track)?.steps.findIndex((s) => s.id === step.needs?.back) ?? -1;
    return to >= 0 ? (
      <Button variant="primary" size="sm" onClick={() => toStep(to)}>
        {t('next.tour.toStep')}
      </Button>
    ) : null;
  }
  if (stage?.action) {
    const act = stage.action;
    return (
      <Button
        variant="primary"
        size="sm"
        onClick={() => {
          const library = libraryControl();
          if (act === 'clear') library?.clearView();
          else void library?.restoreDemo().catch(() => undefined);
        }}
      >
        {t(`next.tour.stage.${stage.id}.action`)}
      </Button>
    );
  }
  if (held) {
    return (
      <Button variant="primary" size="sm" onClick={() => toStep(index + 1)}>
        {t('next.tour.next')}
      </Button>
    );
  }
  if (step.done && !step.manual) return null;
  return (
    <Button variant="primary" size="sm" onClick={() => toStep(index + 1)}>
      {step.look ? t('next.tour.next') : t('next.tour.did')}
    </Button>
  );
};

interface ICardProps {
  track: string;
  index: number;
  total: number;
  step: ITourStep | null;
  done: boolean;
  held: boolean;
  frame: IFrame;
  closing: boolean;
  state: TStepState;
  stage: ITourStage | null;
}

const Card = ({ track, index, total, step, done, held, frame, closing, state, stage }: ICardProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const passed = step === null;

  // The card's place goes to CSS through custom properties: measured after layout, where the card is its own size.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const view = { w: window.innerWidth, h: window.innerHeight };
    if (state === STEP_STATE.away) {
      // Away from the lesson's page the card keeps to a corner: the page the person went to is theirs.
      el.style.setProperty('--tour-x', `${Math.round(view.w - el.offsetWidth - 16)}px`);
      el.style.setProperty('--tour-y', `${Math.round(view.h - el.offsetHeight - 16)}px`);
      el.dataset.side = 'center';
      return;
    }
    const p = placeCard(
      passed ? null : frame.ring,
      { w: el.offsetWidth, h: el.offsetHeight },
      view,
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
            {(done || held) && <Icon name="check" size="sm" />}
            {t(`${key}.${step.id}.title`)}
          </h3>
          <p className="hx-tour-card__text">{stepText(key, step, state, stage)}</p>
          {state === STEP_STATE.loading && (
            <p className="hx-tour-card__wait" role="status">
              <span className="hx-tour-card__spin" aria-hidden="true" />
              {t('next.tour.loading')}
            </p>
          )}
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
              <button type="button" className="hx-tour-card__link" onClick={() => stepBack(index - 1)}>
                {t('next.tour.back')}
              </button>
            )}
            <span className="hx-spacer" />
            <button type="button" className="hx-tour-card__link" onClick={() => toStep(index + 1)}>
              {t('next.tour.skip')}
            </button>
            <StepAction
              track={track}
              index={index}
              step={step}
              done={done}
              held={held}
              state={state}
              stage={stage}
            />
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
  const key = `${tour.track}:${tour.step}:${tour.at}`;
  const { done, held } = useDone(step, key, tour.back);
  const state = useStepState(step);
  const stage = useStage(step);
  const frame = useFrame(step, state === STEP_STATE.on ? stage : null);

  // A step that makes no sense here (no agent to answer a request) is passed over.
  useEffect(() => {
    if (state === STEP_STATE.skip) toStep(tour.step + 1);
  }, [state, tour.step]);

  // A done step stays on screen with its check for a moment, then the next one comes.
  useEffect(() => {
    if (!done) return;
    const timer = setTimeout(() => toStep(tour.step + 1), DONE_MS);
    return () => clearTimeout(timer);
  }, [done, tour.step]);

  const shown = usePresence(track ? key : null, EXIT_MS);
  if (!shown.value || !track) return null;
  // Away from the step's page or before the step it needs, nothing there is the step's: no ring.
  const ring = step && (state === STEP_STATE.on || state === STEP_STATE.loading) ? frame.ring : null;
  return createPortal(
    <div className="hx-tour" data-state={shown.state}>
      {ring && <RingBox rect={ring} />}
      <Card
        track={track.id}
        index={tour.step}
        total={total}
        step={step}
        done={done}
        held={held}
        frame={frame}
        closing={shown.state === 'closing'}
        state={state}
        stage={state === STEP_STATE.on ? stage : null}
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
