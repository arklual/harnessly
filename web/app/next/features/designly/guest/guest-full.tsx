// A guest's full screen: the design alone over the window. A computer asks the browser for its own full screen, and a
// capsule at the top edge (it comes while the pointer is there) holds the way out, the comment and a new tab; a phone
// has a handle at the top instead: pulled down it goes out, a tap shows the same capsule.
import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { useMedia } from '@/next/ui/use-media';
import { t } from '@/next/i18n';

/** A finger is the pointer: the phone's handle instead of the edge of a computer. */
export const TOUCH_QUERY = '(pointer: coarse)';
/** How long the capsule stays after the full screen opens, and after the pointer leaves it. */
const CAPSULE_FIRST_MS = 2600;
const CAPSULE_LEAVE_MS = 700;
const CAPSULE_OUT_MS = 160;
/** How far the handle is pulled down to go out. */
const PULL_OUT = 110;
const TAP_SLOP = 6;

export const useTouch = (): boolean => useMedia(TOUCH_QUERY);

interface IGuestFullProps {
  touch: boolean;
  onExit: () => void;
  /** Turns picking a block on or off; null for a link that only shows. */
  comment: { picking: boolean; onToggle: () => void } | null;
  onNewTab?: () => void;
}

const GuestFull = ({ touch, onExit, comment, onNewTab }: IGuestFullProps) => {
  const [shown, setShown] = useState(true);
  const capsule = usePresence(shown ? true : null, CAPSULE_OUT_MS);
  const hide = useRef(0);

  const later = (ms: number) => {
    window.clearTimeout(hide.current);
    hide.current = window.setTimeout(() => setShown(false), ms);
  };
  const stay = () => {
    window.clearTimeout(hide.current);
    setShown(true);
  };
  useEffect(() => {
    later(CAPSULE_FIRST_MS);
    return () => window.clearTimeout(hide.current);
  }, []);

  // The handle follows the finger down; far enough, the full screen goes, else it springs back (a tap shows the capsule).
  const pull = (event: ReactPointerEvent<HTMLButtonElement>) => {
    const el = event.currentTarget;
    const root = el.closest<HTMLElement>('.hx-guest');
    const start = event.clientY;
    el.setPointerCapture(event.pointerId);
    let dy = 0;
    const move = (e: PointerEvent) => {
      dy = Math.max(0, e.clientY - start);
      root?.style.setProperty('--hx-pull', `${dy}px`);
      el.dataset.ready = String(dy > PULL_OUT);
    };
    const up = () => {
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
      root?.style.removeProperty('--hx-pull');
      delete el.dataset.ready;
      if (dy > PULL_OUT) onExit();
      else if (dy < TAP_SLOP) {
        stay();
        later(CAPSULE_FIRST_MS);
      }
    };
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
  };

  return (
    <div className="hx-gfull" data-touch={touch}>
      {touch ? (
        <button
          type="button"
          className="hx-gfull__handle"
          aria-label={t('next.guest.exit')}
          onPointerDown={pull}
          onClick={(event) => {
            // A keyboard's press: the pointer's own is taken by the pull.
            if (event.detail === 0) onExit();
          }}
        />
      ) : (
        <div className="hx-gfull__edge" aria-hidden="true" onPointerEnter={stay} />
      )}
      {capsule.value && (
        <div
          className="hx-gfull__capsule"
          data-state={capsule.state}
          onPointerEnter={stay}
          onPointerLeave={() => later(CAPSULE_LEAVE_MS)}
        >
          {comment && (
            <button
              type="button"
              className="hx-gfull__btn"
              aria-pressed={comment.picking}
              onClick={comment.onToggle}
            >
              <Icon name="message-square-plus" size="sm" />
              <span>{t('next.guest.pick')}</span>
              {!touch && <kbd>C</kbd>}
            </button>
          )}
          {onNewTab && (
            <button
              type="button"
              className="hx-gfull__btn"
              data-short="true"
              aria-label={t('next.guest.newTab')}
              onClick={onNewTab}
            >
              <Icon name="external-link" size="sm" />
              <span>{t('next.guest.newTab')}</span>
            </button>
          )}
          <button type="button" className="hx-gfull__btn" data-tone="exit" onClick={onExit}>
            <Icon name="minimize-2" size="sm" />
            <span>{t('next.guest.exit')}</span>
            {!touch && <kbd>Esc</kbd>}
          </button>
        </div>
      )}
    </div>
  );
};

export default GuestFull;
