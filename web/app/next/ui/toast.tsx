// Quiet notices at the top of the window (after lib/hx/toast.js of the design): at most three on screen, 4 s each by
// default, an optional action, and an exit animation. The store lives outside React so anything may call toast().
// On the phone they stand above the chat's field and the safe area instead (styles/ui.css). A swipe takes one away:
// a finger, a held mouse button or a trackpad's two fingers, sideways or up (and down on the phone).
import { useSyncExternalStore, type MouseEvent, type PointerEvent, type WheelEvent } from 'react';
import Icon, { type TIconName } from '@/next/ui/icon';
import '@/next/styles/ui.css';

export const TOAST_MS = 4000;
/** A notice with a way out («Повторить») stays longer: the person needs time to reach it. */
export const TOAST_ACTION_MS = 8000;
const TOAST_EXIT_MS = 260;
const VISIBLE = 3;

export type TToastTone = 'info' | 'success' | 'warning' | 'error';

interface IToast {
  id: number;
  text: string;
  description: string | null;
  tone: TToastTone;
  action: { label: string; onClick: () => void } | null;
  leaving: boolean;
}

let toasts: IToast[] = [];
let seq = 0;
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((listener) => listener());

const dismiss = (id: number) => {
  toasts = toasts.map((item) => (item.id === id ? { ...item, leaving: true } : item));
  emit();
  setTimeout(() => {
    toasts = toasts.filter((item) => item.id !== id);
    emit();
  }, TOAST_EXIT_MS);
};

/** Shows a notice; returns a function that takes it away early. */
export const toast = (
  text: string,
  options: {
    tone?: TToastTone;
    description?: string;
    action?: { label: string; onClick: () => void };
    ms?: number;
  } = {},
): (() => void) => {
  const id = ++seq;
  toasts = [
    ...toasts,
    {
      id,
      text,
      description: options.description ?? null,
      tone: options.tone ?? 'info',
      action: options.action ?? null,
      leaving: false,
    },
  ];
  emit();
  setTimeout(() => dismiss(id), options.ms ?? (options.action ? TOAST_ACTION_MS : TOAST_MS));
  return () => dismiss(id);
};

/** A swipe past this, or a quick flick, takes a notice away, px and px/ms. */
const SWIPE_PX = 72;
const SWIPE_VELOCITY = 0.45;
/** A press moves this far before it is a drag, so a tap on the notice's button stays a click, px. */
const DRAG_SLOP = 6;
/** A trackpad's two-finger swipe has no end event: it ends when the wheel is quiet this long, ms. */
const WHEEL_IDLE_MS = 140;

/** On the phone the notices stand at the foot of the screen, so a swipe down takes them away too. */
const atFoot = () => window.matchMedia('(max-width: 900px)').matches;

/** Where a pull may go: sideways always, up always, down only where the notices stand at the foot. */
const allowed = (x: number, y: number) => ({ x, y: y > 0 && !atFoot() ? 0 : y });

const place = (el: HTMLElement, at: { x: number; y: number }) => {
  el.style.setProperty('--hx-toast-dx', `${at.x}px`);
  el.style.setProperty('--hx-toast-dy', `${at.y}px`);
};

/** Far or fast enough, the notice flies out the way it was pulled; otherwise it springs back. */
const release = (el: HTMLElement, id: number, at: { x: number; y: number }, ms: number) => {
  delete el.dataset.drag;
  const far = Math.hypot(at.x, at.y);
  const fast = far / Math.max(1, ms) > SWIPE_VELOCITY;
  if (far > SWIPE_PX || (fast && far > DRAG_SLOP)) {
    const side = Math.abs(at.x) >= Math.abs(at.y);
    place(
      el,
      side ? { x: Math.sign(at.x) * window.innerWidth, y: at.y } : { x: at.x, y: Math.sign(at.y) * 200 },
    );
    dismiss(id);
    return;
  }
  el.style.removeProperty('--hx-toast-dx');
  el.style.removeProperty('--hx-toast-dy');
};

/**
 * Takes a notice away with a gesture: a finger or a held mouse button pulls it sideways or up (and down on the phone),
 * a trackpad's two-finger swipe does the same. The move goes straight into --hx-toast-dx/-dy, no state per frame.
 */
const swipeAway = (id: number) => {
  let start: { x: number; y: number; t: number; pointer: number } | null = null;
  let at = { x: 0, y: 0 };
  let dragged = false;
  let wheel: { t: number; timer: number } | null = null;
  return {
    onPointerDown: (event: PointerEvent<HTMLElement>) => {
      if (event.button !== 0) return;
      start = { x: event.clientX, y: event.clientY, t: event.timeStamp, pointer: event.pointerId };
      at = { x: 0, y: 0 };
      dragged = false;
    },
    onPointerMove: (event: PointerEvent<HTMLElement>) => {
      if (!start || event.pointerId !== start.pointer) return;
      const next = allowed(event.clientX - start.x, event.clientY - start.y);
      if (!dragged) {
        if (Math.hypot(next.x, next.y) < DRAG_SLOP) return;
        dragged = true;
        event.currentTarget.dataset.drag = 'true';
        try {
          event.currentTarget.setPointerCapture(event.pointerId);
        } catch {
          // The pointer is already gone: the release ends the pull anyway.
        }
      }
      at = next;
      place(event.currentTarget, at);
    },
    onPointerUp: (event: PointerEvent<HTMLElement>) => {
      if (!start || event.pointerId !== start.pointer) return;
      const ms = event.timeStamp - start.t;
      start = null;
      if (dragged) release(event.currentTarget, id, at, ms);
    },
    onPointerCancel: (event: PointerEvent<HTMLElement>) => {
      start = null;
      if (dragged) release(event.currentTarget, id, { x: 0, y: 0 }, 1);
      dragged = false;
    },
    // The release of a pull is no press on the notice's button.
    onClickCapture: (event: MouseEvent<HTMLElement>) => {
      if (!dragged) return;
      dragged = false;
      event.stopPropagation();
      event.preventDefault();
    },
    onWheel: (event: WheelEvent<HTMLElement>) => {
      const el = event.currentTarget;
      if (!wheel) {
        at = { x: 0, y: 0 };
        wheel = { t: event.timeStamp, timer: 0 };
        el.dataset.drag = 'true';
      }
      // The fingers move the notice the way the page would follow them.
      at = allowed(at.x - event.deltaX, at.y - event.deltaY);
      place(el, at);
      window.clearTimeout(wheel.timer);
      const begun = wheel.t;
      wheel.timer = window.setTimeout(() => {
        wheel = null;
        release(el, id, at, performance.now() - begun);
      }, WHEEL_IDLE_MS);
    },
  };
};

const subscribe = (listener: () => void) => {
  listeners.add(listener);
  return () => listeners.delete(listener);
};

const ICONS: Record<TToastTone, TIconName> = {
  info: 'info',
  success: 'circle-check',
  warning: 'triangle-alert',
  error: 'circle-alert',
};

/** Where the notices draw; mount once inside .hx-root. */
export const ToastHost = () => {
  const list = useSyncExternalStore(
    subscribe,
    () => toasts,
    () => toasts,
  );
  return (
    <ol className="hx-toasts" aria-live="polite">
      {list.slice(-VISIBLE).map((item) => (
        // The slot folds to nothing as its notice leaves, so the ones under it slide up instead of jumping.
        <li key={item.id} className="hx-toast-slot" data-leaving={item.leaving || undefined}>
          <div className="hx-toast-slot__clip">
            <div
              className="hx-toast"
              data-tone={item.tone}
              data-leaving={item.leaving || undefined}
              {...swipeAway(item.id)}
            >
              <Icon name={ICONS[item.tone]} size="sm" className="hx-toast__icon" />
              <span className="hx-toast__text">
                {item.text}
                {item.description && <span className="hx-toast__desc">{item.description}</span>}
              </span>
              {item.action && (
                <button
                  type="button"
                  className="hx-toast__action"
                  onClick={() => {
                    item.action?.onClick();
                    dismiss(item.id);
                  }}
                >
                  {item.action.label}
                </button>
              )}
            </div>
          </div>
        </li>
      ))}
    </ol>
  );
};
