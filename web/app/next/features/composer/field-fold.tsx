// The folded field (the design «Поле ввода — сжатое», variant A «По центру»): over a page the field lies as a
// pill 44px high and 440px wide at the bottom in the middle — the hint, or what the agent does now, on the left; the
// model, the limit when it runs low and the microphone on the right, and while the agent works a spinner and «Стоп» in
// the microphone's place. It opens from the same point, smoothly, on a click, on typing or pasting into it, or when the
// pointer comes over the model; it folds on Escape or on a press outside while the field is empty. On a phone it is as
// wide as the screen. The field itself (children) stays mounted all the time: folding only hides it.
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { flushSync } from 'react-dom';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';

export interface IFieldFoldProps {
  /** The field's words when nothing is typed (its placeholder). */
  hint: string;
  /** What the agent does now, while it works; null when it does not. */
  line: string | null;
  /** The model's name; null where the field has none to show. */
  model: string | null;
  /** The limit, only when it runs low («осталось 12%»); null otherwise. */
  limit: string | null;
  working: boolean;
  /** Stops the agent; shown while it works. */
  onStop?: () => void;
  /** Starts the voice input; the field opens for it. */
  onMic?: () => void;
  /** Nothing is typed and nothing is attached: a press outside folds the field. Words that come into it (dictated, put
   *  there by the page) open it. */
  empty: boolean;
  /** The field: a dock with its composer. */
  children: ReactNode;
}

/** Popovers of the field live outside it (the model's menu, the takes): a press in them is not «outside». */
const OUTSIDE_IGNORED = '.hx-portal, [role="dialog"], [role="menu"], [role="listbox"]';

const fieldOf = (el: HTMLElement | null) =>
  el?.querySelector<HTMLTextAreaElement>('.hx-fold__full textarea') ?? null;

const FieldFold = ({
  hint,
  line,
  model,
  limit,
  working,
  onStop,
  onMic,
  empty,
  children,
}: IFieldFoldProps) => {
  const box = useRef<HTMLDivElement>(null);
  const full = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(!empty);
  // Words that come into the field open it (dictated, put there by the page); Escape folds it all the same.
  const [wasEmpty, setWasEmpty] = useState(empty);
  if (wasEmpty !== empty) {
    setWasEmpty(empty);
    if (!empty) setOpen(true);
  }

  // The box grows to the field's own height; the field is measured where it lies, at the bottom of the box.
  useEffect(() => {
    const el = full.current;
    const frame = box.current;
    if (!el || !frame || typeof ResizeObserver === 'undefined') return;
    const follow = () =>
      frame.style.setProperty('--hx-fold-h', `${Math.ceil(el.getBoundingClientRect().height)}px`);
    const watcher = new ResizeObserver(follow);
    watcher.observe(el);
    follow();
    return () => watcher.disconnect();
  }, []);

  const unfold = useCallback((focus = true) => {
    // Drawn open now, so the field is no longer inert and takes the focus at once: typed keys go into it, not the pill.
    flushSync(() => setOpen(true));
    if (focus) fieldOf(box.current)?.focus({ preventScroll: true });
  }, []);

  // A press outside folds an empty field.
  useEffect(() => {
    if (!open || !empty) return;
    const away = (event: PointerEvent) => {
      const target = event.target instanceof Element ? event.target : null;
      if (!target || box.current?.contains(target) || target.closest(OUTSIDE_IGNORED)) return;
      setOpen(false);
    };
    document.addEventListener('pointerdown', away);
    return () => document.removeEventListener('pointerdown', away);
  }, [open, empty]);

  // Escape folds the field. A native listener: the window's one field is drawn through a portal (one-field), so its
  // keys reach this box in the page, not through React's tree.
  useEffect(() => {
    const el = box.current;
    if (!el || !open) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return;
      event.preventDefault();
      setOpen(false);
      const focused = document.activeElement;
      if (focused instanceof HTMLElement && el.contains(focused)) focused.blur();
    };
    el.addEventListener('keydown', onKey);
    return () => el.removeEventListener('keydown', onKey);
  }, [open]);

  return (
    <div ref={box} className="hx-fold" data-open={open} data-working={working || undefined}>
      <div
        className="hx-fold__pill"
        role="button"
        tabIndex={open ? -1 : 0}
        aria-hidden={open || undefined}
        aria-label={hint}
        onClick={() => unfold()}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            unfold();
            return;
          }
          // A typed key goes on into the field: the focus moves before the key makes its letter.
          if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) unfold();
        }}
        onPaste={(event) => {
          // What is pasted into the pill goes into the field, files too: the field's own paste takes them.
          event.preventDefault();
          unfold();
          const field = fieldOf(box.current);
          if (!field) return;
          const data = event.clipboardData;
          if (data.files.length > 0) {
            field.dispatchEvent(
              new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }),
            );
            return;
          }
          document.execCommand('insertText', false, data.getData('text/plain'));
        }}
      >
        <span className="hx-fold__hint" data-line={line !== null || undefined}>
          {line ?? hint}
        </span>
        {model !== null && (
          <span className="hx-fold__model" onPointerEnter={() => unfold(false)}>
            {model}
          </span>
        )}
        {limit !== null && <span className="hx-fold__limit">{limit}</span>}
        {working ? (
          <>
            <span className="hx-fold__spin" aria-hidden="true" />
            {onStop && (
              <button
                type="button"
                className="hx-fold__stop"
                aria-label={t('next.composer.stop')}
                onClick={(event) => {
                  event.stopPropagation();
                  onStop();
                }}
              >
                <i />
              </button>
            )}
          </>
        ) : (
          onMic && (
            <button
              type="button"
              className="hx-fold__mic"
              aria-label={t('next.composer.voice')}
              onClick={(event) => {
                event.stopPropagation();
                unfold(false);
                onMic();
              }}
            >
              <Icon name="mic" size="sm" />
            </button>
          )
        )}
      </div>
      <div ref={full} className="hx-fold__full" aria-hidden={!open || undefined} inert={!open || undefined}>
        {children}
      </div>
    </div>
  );
};

export default FieldFold;
