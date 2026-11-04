// The foot of a design opened in the right pane (the Legacy pane's pager): one floating pill under the page with the way
// through the design's pages — back, «Главная 1/2» with the list of them, forward — the canvas's zoom (−, 72 %, +, «Вписать», the lock)
// and the switch of the page's live settings (the «Состояния» panel). It is only a control: the pages, the canvas and
// the panel are the view's.
import { useEffect, useRef, useState } from 'react';
import type { ISitePage } from '@/next/features/designly/site-view';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { SheetScrim, useSheetDrag } from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import PagePicker from '@/next/features/designly/page-picker';
import { t } from '@/next/i18n';

const MENU_MS = 200;

/** The zoom of the canvas as its buttons need it. */
export interface IZoomControls {
  pct: number;
  zoomed: boolean;
  canIn: boolean;
  canOut: boolean;
  zoomIn: () => void;
  zoomOut: () => void;
  /** «Вписать»: the standard view of the device, locked or not. */
  fit: () => void;
  /** The view is the standard one. */
  std: boolean;
  zoomTo: (pct: number) => void;
  locked: boolean;
  onLock: () => void;
}

/** What the scale's menu offers. */
export const ZOOM_PRESETS = [50, 100, 200] as const;
const isMac = () => typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform);
/** The key of «Вписать» as the menus show it. */
export const fitKeyText = (): string => `${isMac() ? '⌘' : 'Ctrl'}+0`;

/** −, the scale, +, «Вписать», the lock: a compact group for the pill, the device menu or the corner of the canvas. The
 *  scale opens a small menu of the usual scales and «Вписать»; «Вписать» puts the device back to its standard view
 *  (a phone or a tablet whole, a computer whole up to its own size, «По окну» filling the window). Locked, the view
 *  neither pans nor zooms: «Вписать» still works. */
export const ZoomControls = ({ zoom }: { zoom: IZoomControls }) => {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLSpanElement>(null);
  const menu = usePresence(open ? true : null, MENU_MS);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const pick = (action: () => void) => {
    setOpen(false);
    action();
  };
  const lockLabel = zoom.locked ? t('next.site.unlockView') : t('next.site.lockView');

  return (
    <span
      className="hx-zoomctl"
      role="group"
      aria-label={t('next.site.zoom')}
      data-locked={zoom.locked}
      ref={root}
    >
      <button
        type="button"
        className="hx-head-btn hx-tipped hx-zoomctl__step"
        aria-label={t('next.site.zoomOut')}
        aria-disabled={!zoom.canOut}
        onClick={() => zoom.canOut && zoom.zoomOut()}
      >
        <Icon name="minus" size="sm" />
        <span className="hx-tip">{t('next.site.zoomOut')}</span>
      </button>
      <button
        type="button"
        className="hx-zoomctl__pct"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-disabled={zoom.locked}
        aria-label={t('next.site.zoomMenu')}
        aria-live="polite"
        onClick={() => !zoom.locked && setOpen((now) => !now)}
      >
        {zoom.pct}%
      </button>
      <button
        type="button"
        className="hx-head-btn hx-tipped hx-zoomctl__step"
        aria-label={t('next.site.zoomIn')}
        aria-disabled={!zoom.canIn}
        onClick={() => zoom.canIn && zoom.zoomIn()}
      >
        <Icon name="plus" size="sm" />
        <span className="hx-tip">{t('next.site.zoomIn')}</span>
      </button>
      <button
        type="button"
        className="hx-head-btn hx-tipped"
        aria-label={t('next.site.zoomFit')}
        onClick={() => pick(zoom.fit)}
      >
        <Icon name="maximize-2" size="sm" />
        <span className="hx-tip" data-align="end">
          {t('next.site.zoomFit')}
          <kbd>{fitKeyText()}</kbd>
        </span>
      </button>
      <button
        type="button"
        className="hx-head-btn hx-tipped"
        aria-label={lockLabel}
        aria-pressed={zoom.locked}
        onClick={zoom.onLock}
      >
        <Icon name={zoom.locked ? 'lock' : 'lock-open'} size="sm" />
        <span className="hx-tip" data-align="end">
          {lockLabel}
        </span>
      </button>
      {menu.value !== null && (
        <div className="hx-site-menu hx-zoomctl__menu" data-state={menu.state} role="menu">
          {ZOOM_PRESETS.map((pct) => (
            <button
              key={pct}
              type="button"
              role="menuitem"
              className="hx-zoomctl__item"
              aria-current={zoom.pct === pct}
              onClick={() => pick(() => zoom.zoomTo(pct))}
            >
              <span>{pct}%</span>
            </button>
          ))}
          <div className="hx-zoomctl__sep" role="separator" />
          <button type="button" role="menuitem" className="hx-zoomctl__item" onClick={() => pick(zoom.fit)}>
            <span>{t('next.site.zoomFit')}</span>
            <kbd>{fitKeyText()}</kbd>
          </button>
        </div>
      )}
    </span>
  );
};

interface IPaneDockProps {
  pages: readonly ISitePage[];
  /** The page on screen; null while there is none. */
  path: string | null;
  onGo: (path: string) => void;
  /** The panel of the page's live settings: its switch. */
  tweaks: { on: boolean; disabled: boolean; onToggle: () => void };
  /** The canvas's zoom; null where the page is not a canvas («Обзор»). */
  zoom: IZoomControls | null;
}

export const PaneDock = ({ pages, path, onGo, tweaks, zoom }: IPaneDockProps) => {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const menu = usePresence(open ? true : null, MENU_MS);
  const phone = usePhone();
  useSheetDrag(
    () => root.current?.querySelector<HTMLElement>('.hx-pgpick') ?? null,
    () => setOpen(false),
    phone && menu.value !== null,
  );
  const at = pages.findIndex((p) => p.path === path);
  const page = at >= 0 ? pages[at] : null;
  const prev = at > 0 ? pages[at - 1] : null;
  const next = at >= 0 && at < pages.length - 1 ? pages[at + 1] : null;

  // A press elsewhere or Esc folds the list.
  useEffect(() => {
    if (!open) return;
    const onDown = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false);
    };
    window.addEventListener('pointerdown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('pointerdown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  return (
    <div className="hx-pdock" ref={root}>
      {menu.value !== null && (
        <>
          <SheetScrim state={menu.state} onClose={() => setOpen(false)} phoneOnly />
          <PagePicker
            pages={pages}
            path={path}
            state={menu.state}
            onGo={(to) => {
              setOpen(false);
              onGo(to);
            }}
            onClose={() => setOpen(false)}
          />
        </>
      )}
      <div className="hx-pdock__pill">
        {pages.length > 1 && (
          <span className="hx-pdock__group">
            <button
              type="button"
              className="hx-head-btn"
              aria-label={t('next.pane.designPrevPage')}
              aria-disabled={prev === null}
              onClick={() => prev && onGo(prev.path)}
            >
              <Icon name="chevron-left" size="sm" />
            </button>
            <button
              type="button"
              className="hx-pdock__pages"
              aria-expanded={open}
              aria-label={t('next.site.pages')}
              onClick={() => setOpen((on) => !on)}
            >
              <span className="hx-pdock__name">{page?.name ?? t('next.site.pages')}</span>
              {page && (
                <span className="hx-pdock__n">
                  {at + 1}/{pages.length}
                </span>
              )}
              <Icon name="chevron-down" size="sm" />
            </button>
            <button
              type="button"
              className="hx-head-btn"
              aria-label={t('next.pane.designNextPage')}
              aria-disabled={next === null}
              onClick={() => next && onGo(next.path)}
            >
              <Icon name="chevron-right" size="sm" />
            </button>
          </span>
        )}
        {zoom && (
          <span className="hx-pdock__group">
            <ZoomControls zoom={zoom} />
          </span>
        )}
        {!tweaks.disabled && (
          <span className="hx-pdock__group">
            <button
              type="button"
              className="hx-head-btn hx-tipped"
              aria-pressed={tweaks.on}
              aria-label={t('next.pane.designStates')}
              onClick={tweaks.onToggle}
            >
              <Icon name="sliders-horizontal" size="sm" />
              <span className="hx-tip" data-align="end">
                {t('next.pane.designStates')}
              </span>
            </button>
          </span>
        )}
      </div>
    </div>
  );
};
