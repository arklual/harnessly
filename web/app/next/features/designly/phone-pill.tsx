// The phone's pill: beside a phone or a tablet on the canvas, the way it is drawn — turned or upright, its case,
// its status bar, the browser's bars, a fingertip instead of the pointer (no hover). It stands on the phone's right, level
// with its top, and follows it as the canvas moves; with no room there it lies over the phone. Dragged, it stays where it
// was put; its menu (a right click) has the same with names, back to its place, hide (floating-pill.tsx).
import { useRef, useState } from 'react';
import {
  PillMenuFoot,
  pillStore,
  useFloatingPill,
  usePillPrefs,
} from '@/next/features/designly/floating-pill';
import { besideBox, PILL_AXIS, PILL_KEY } from '@/next/features/designly/zoom-pill-pos';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';

/** The phone's pill's place and whether it is hidden, shared by every canvas of the page. */
export const PHONE_PILL = pillStore(PILL_KEY.phone);
export const usePhonePillHidden = (): boolean => usePillPrefs(PHONE_PILL).hidden;

const OUT_MS = 180;
/** What the menu (and a hint beside the pill) needs on the pill's right to open there. */
const MENU_ROOM = 260;
/** The canvas moves smoothly (a fit, a device's new size) this long at most: the pill follows it frame by frame. */
const FOLLOW_MS = 480;

export interface IPhoneToggle {
  on: boolean;
  onToggle: () => void;
}

interface IPhonePillProps {
  /** There is a phone or a tablet on the canvas: the pill comes, and leaves with its animation when there is none. */
  active: boolean;
  rot: IPhoneToggle;
  /** Null where the device has no case (the bare phone), or no interface of its own (turned). */
  frame: IPhoneToggle | null;
  status: IPhoneToggle | null;
  browser: IPhoneToggle | null;
  touch: IPhoneToggle;
  /** What moves the phone on the canvas or changes the pill: another device, a turn. */
  watch: string;
}

interface IItem {
  id: string;
  icon: string;
  label: string;
  /** The hint, where it says more than the name (the turn: what a press does). */
  tip?: string;
  toggle: IPhoneToggle;
}

/** The phone on the canvas, in the coordinates of the pill's parent. */
const phoneBox = (area: HTMLElement) => {
  const box = area.querySelector('.hx-vpbox');
  if (!box) return null;
  const a = area.getBoundingClientRect();
  const b = box.getBoundingClientRect();
  return { left: b.left - a.left, top: b.top - a.top, right: b.right - a.left };
};

/** The pill measures its place again while the canvas pans, zooms or the device changes its size. */
const followPhone = (area: HTMLElement, again: () => void) => {
  const canvas = area.querySelector('.hx-vparea');
  const box = area.querySelector('.hx-vpbox');
  let frame = 0;
  let until = 0;
  const tick = () => {
    again();
    frame = performance.now() < until ? requestAnimationFrame(tick) : 0;
  };
  const kick = () => {
    until = performance.now() + FOLLOW_MS;
    if (frame === 0) frame = requestAnimationFrame(tick);
  };
  const moves = new MutationObserver(kick);
  if (canvas) moves.observe(canvas, { attributes: true, attributeFilter: ['style', 'data-dev', 'data-rot'] });
  const sizes = new ResizeObserver(kick);
  if (box) sizes.observe(box);
  return () => {
    moves.disconnect();
    sizes.disconnect();
    cancelAnimationFrame(frame);
  };
};

export const PhonePill = ({ active, rot, frame, status, browser, touch, watch }: IPhonePillProps) => {
  const hidden = usePillPrefs(PHONE_PILL).hidden;
  const shown = usePresentFlag(active && !hidden, OUT_MS);
  const [axis, setAxis] = useState<string>(PILL_AXIS.col);
  const ref = useRef<HTMLDivElement>(null);
  const pill = useFloatingPill(ref, PHONE_PILL, {
    auto: (area, el) => {
      const box = phoneBox(area);
      if (!box) return null;
      // Measured standing up; lying over the phone it takes its other size before it is kept inside the canvas.
      el.dataset.axis = PILL_AXIS.col;
      const at = besideBox({ w: area.clientWidth, h: area.clientHeight }, box, {
        w: el.offsetWidth,
        h: el.offsetHeight,
      });
      el.dataset.axis = at.axis;
      return at.pos;
    },
    follow: followPhone,
    watch: `${watch}|${shown.mounted}`,
    // The menu and the hints open to the right where they have room, else to the left; a pill put by hand stands up.
    placed: (el, area) => {
      if (PHONE_PILL.get().pos !== null) el.dataset.axis = PILL_AXIS.col;
      const a = area.getBoundingClientRect();
      const p = el.getBoundingClientRect();
      el.dataset.side = a.right - p.left >= MENU_ROOM ? 'end' : 'start';
      el.dataset.drop = p.top + p.height / 2 - a.top > a.height / 2 ? 'up' : 'down';
      setAxis(el.dataset.axis ?? PILL_AXIS.col);
    },
  });
  if (!shown.mounted) return null;

  const turned = rot.on ? t('next.site.portrait') : t('next.site.landscape');
  const items: IItem[] = [
    { id: 'rot', icon: 'rotate-cw', label: t('next.site.landscape'), tip: turned, toggle: rot },
    frame && { id: 'frame', icon: 'smartphone', label: t('next.site.phoneFrame'), toggle: frame },
    status && { id: 'status', icon: 'signal', label: t('next.site.phoneStatus'), toggle: status },
    browser && { id: 'browser', icon: 'panel-bottom', label: t('next.site.phoneBrowser'), toggle: browser },
    {
      id: 'touch',
      icon: 'pointer',
      label: t('next.site.phoneTouch'),
      tip: t('next.site.phoneTouchTip'),
      toggle: touch,
    },
  ].filter((item): item is IItem => item !== null);

  return (
    <div
      ref={ref}
      className="hx-phonepill"
      data-state={shown.state}
      role="toolbar"
      aria-label={t('next.site.phonePill')}
      aria-orientation={axis === PILL_AXIS.col ? 'vertical' : 'horizontal'}
      {...pill.bind}
    >
      {items.map((item) => (
        <button
          key={item.id}
          type="button"
          className="hx-head-btn hx-tipped"
          aria-label={item.label}
          aria-pressed={item.toggle.on}
          data-item={item.id}
          onClick={item.toggle.onToggle}
        >
          <Icon name={item.icon} size="sm" />
          <span className="hx-tip">{item.tip ?? item.label}</span>
        </button>
      ))}
      {pill.menu.value !== null && (
        <div
          className="hx-site-menu hx-phonepill__menu"
          data-state={pill.menu.state}
          role="menu"
          aria-label={t('next.site.phonePill')}
          onPointerDown={(event) => event.stopPropagation()}
        >
          <div className="hx-dl-pop__label">{t('next.site.phonePill')}</div>
          {items.map((item) => (
            <button
              key={item.id}
              type="button"
              role="menuitemcheckbox"
              aria-checked={item.toggle.on}
              className="hx-option hx-phonepill__opt"
              onClick={item.toggle.onToggle}
            >
              <Icon name={item.icon} size="sm" />
              <span className="hx-option__name">{item.label}</span>
              <span className="hx-switch" data-on={item.toggle.on} aria-hidden="true">
                <i />
              </span>
            </button>
          ))}
          <PillMenuFoot
            placed={pill.prefs.pos !== null}
            onReset={pill.reset}
            onHide={pill.hide}
            resetLabel={t('next.site.phonePillReset')}
            hideLabel={t('next.site.phonePillHide')}
            hint={t('next.site.phonePillHint')}
          />
        </div>
      )}
    </div>
  );
};
