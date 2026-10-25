// The side chat as a window over everything (.hx-sidechat): it opens at the bottom right, the head drags it, the edges
// and corners size it, and let go at the right edge (.hx-sidesnap lights up there) it docks as a tab of the pane.
import { useLayoutEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import Icon from '@/next/ui/icon';
import { SideChatBody, type ISideChatProps } from '@/next/features/pane/side-chat';
import { t } from '@/next/i18n';
import '@/next/styles/side-chat.css';

const EDGE = 8;
const SNAP_ZONE = 72;
const MIN_W = 300;
const MIN_H = 320;
const HEAD_KEEP = 60;
const RESIZERS = ['n', 's', 'e', 'w', 'nw', 'ne', 'sw', 'se'] as const;

export interface ISideFloatProps extends ISideChatProps {
  /** The chat it is about. */
  about: string;
  closing: boolean;
  onDock: () => void;
  onClear: () => void;
  onClose: () => void;
}

interface IBox {
  x: number;
  y: number;
}

/** Follows the pointer until it is let go: the window's own drag and size, as the design does them. */
const follow = (onMove: (event: PointerEvent) => void, onUp: () => void) => {
  const up = () => {
    removeEventListener('pointermove', onMove);
    removeEventListener('pointerup', up);
    onUp();
  };
  addEventListener('pointermove', onMove);
  addEventListener('pointerup', up);
};

export const SideFloat = ({ about, closing, onDock, onClear, onClose, ...chat }: ISideFloatProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<IBox | null>(null);
  const [size, setSize] = useState<{ w: number; h: number } | null>(null);
  const [drag, setDrag] = useState(false);
  const [snap, setSnap] = useState(false);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (pos) {
      el.style.setProperty('--sx', `${pos.x}px`);
      el.style.setProperty('--sy', `${pos.y}px`);
    }
    if (size) {
      el.style.setProperty('--sw', `${size.w}px`);
      el.style.setProperty('--sh', `${size.h}px`);
    }
  }, [pos, size]);

  const grab = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || (event.target as Element).closest('button')) return;
    const card = ref.current;
    if (!card) return;
    const r = card.getBoundingClientRect();
    const dx = event.clientX - r.left;
    const dy = event.clientY - r.top;
    let snapping = false;
    setDrag(true);
    follow(
      (ev) => {
        snapping = ev.clientX > innerWidth - SNAP_ZONE;
        setSnap(snapping);
        setPos({
          x: Math.max(EDGE, Math.min(innerWidth - r.width - EDGE, ev.clientX - dx)),
          y: Math.max(EDGE, Math.min(innerHeight - HEAD_KEEP, ev.clientY - dy)),
        });
      },
      () => {
        setDrag(false);
        setSnap(false);
        if (snapping) onDock();
      },
    );
  };

  const resize = (dir: string) => (event: ReactPointerEvent<HTMLSpanElement>) => {
    if (event.button !== 0) return;
    event.preventDefault();
    event.stopPropagation();
    const card = ref.current;
    if (!card) return;
    const r = card.getBoundingClientRect();
    const sx = event.clientX;
    const sy = event.clientY;
    setDrag(true);
    follow(
      (ev) => {
        const dx = ev.clientX - sx;
        const dy = ev.clientY - sy;
        let { left: x, top: y, width: w, height: h } = r;
        if (dir.includes('e')) w = Math.min(innerWidth - x - EDGE, Math.max(MIN_W, r.width + dx));
        if (dir.includes('s')) h = Math.min(innerHeight - y - EDGE, Math.max(MIN_H, r.height + dy));
        if (dir.includes('w')) {
          w = Math.min(r.right - EDGE, Math.max(MIN_W, r.width - dx));
          x = r.right - w;
        }
        if (dir.includes('n')) {
          h = Math.min(r.bottom - EDGE, Math.max(MIN_H, r.height - dy));
          y = r.bottom - h;
        }
        setPos({ x, y });
        setSize({ w: Math.round(w), h: Math.round(h) });
      },
      () => setDrag(false),
    );
  };

  return (
    <>
      <div
        ref={ref}
        className="hx-sidechat"
        data-state={closing ? 'closing' : 'open'}
        data-moved={pos !== null}
        data-drag={drag}
        role="dialog"
        aria-label={t('next.pane.side')}
        onKeyDown={(event) => {
          if (event.key !== 'Escape') return;
          event.preventDefault();
          onClose();
        }}
      >
        {RESIZERS.map((dir) => (
          <span key={dir} className="hx-sidechat__rz" data-dir={dir} onPointerDown={resize(dir)} />
        ))}
        <div className="hx-sidechat__head" onPointerDown={grab}>
          <span className="hx-sidechat__titles">
            <span className="hx-sidechat__title">{t('next.pane.side')}</span>
            <span className="hx-sidechat__ctx">{about}</span>
          </span>
          <span className="hx-spacer" />
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            aria-label={t('next.side.dock')}
            onClick={onDock}
          >
            <Icon name="panel-right" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.side.dockHint')}
            </span>
          </button>
          {chat.messages.length > 0 && (
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              aria-label={t('next.side.clear')}
              onClick={onClear}
            >
              <Icon name="eraser" size="sm" />
              <span className="hx-tip" data-pos="below" data-align="end">
                {t('next.side.clear')}
              </span>
            </button>
          )}
          <button
            type="button"
            className="hx-head-btn hx-tipped"
            aria-label={t('next.side.close')}
            onClick={onClose}
          >
            <Icon name="x" size="sm" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.side.close')}
              <kbd>Esc</kbd>
            </span>
          </button>
        </div>
        <SideChatBody {...chat} />
      </div>
      <div className="hx-sidesnap" data-on={snap} aria-hidden="true">
        <span>
          <Icon name="panel-right" />
          {t('next.side.snap')}
        </span>
      </div>
    </>
  );
};
