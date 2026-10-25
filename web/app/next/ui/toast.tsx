// Quiet notices at the top of the window (after lib/hx/toast.js of the design): at most three on screen, 4 s each by
// default, an optional action, and an exit animation. The store lives outside React so anything may call toast().
// On the phone they stand above the chat's field and the safe area instead (styles/ui.css), and a finger swipes one away.
import { useSyncExternalStore, type TouchEvent } from 'react';
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

/** A swipe sideways past this, or a quick flick, takes a notice away, px and px/ms. */
const SWIPE_PX = 72;
const SWIPE_VELOCITY = 0.45;

/** Follows a finger moving a notice sideways (straight into --hx-toast-dx, no state per frame); far or fast enough, it goes. */
const swipeAway = (id: number) => {
  let start: { x: number; y: number; t: number } | null = null;
  let dx = 0;
  let dragging = false;
  return {
    onTouchStart: (event: TouchEvent<HTMLElement>) => {
      const touch = event.touches[0];
      start = { x: touch.clientX, y: touch.clientY, t: event.timeStamp };
      dx = 0;
      dragging = false;
    },
    onTouchMove: (event: TouchEvent<HTMLElement>) => {
      if (!start) return;
      const touch = event.touches[0];
      dx = touch.clientX - start.x;
      if (!dragging && Math.abs(touch.clientY - start.y) > Math.abs(dx)) {
        start = null;
        return;
      }
      dragging = true;
      event.currentTarget.dataset.drag = 'true';
      event.currentTarget.style.setProperty('--hx-toast-dx', `${dx}px`);
    },
    onTouchEnd: (event: TouchEvent<HTMLElement>) => {
      if (!start) return;
      const fast = Math.abs(dx) / Math.max(1, event.timeStamp - start.t) > SWIPE_VELOCITY;
      start = null;
      const el = event.currentTarget;
      delete el.dataset.drag;
      if (dragging && (Math.abs(dx) > SWIPE_PX || fast)) {
        el.style.setProperty('--hx-toast-dx', `${Math.sign(dx) * window.innerWidth}px`);
        dismiss(id);
        return;
      }
      el.style.removeProperty('--hx-toast-dx');
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
