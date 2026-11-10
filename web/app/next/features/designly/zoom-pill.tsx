// The canvas's zoom pill in its corner (HRN-673): −, the scale, +, «Вписать» and the lock, dragged by any part of it to
// any place over the canvas, and with its settings on a right click (a two-finger press on a trackpad): the scales,
// «Вписать», the lock, back to the corner, hide. A hidden pill comes back from the bar's «…».
import { useRef } from 'react';
import {
  fitKeyText,
  ZOOM_PRESETS,
  ZoomControls,
  type IZoomControls,
} from '@/next/features/designly/site-dock';
import { PillMenuFoot, pillStore, useFloatingPill } from '@/next/features/designly/floating-pill';
import { PILL_KEY } from '@/next/features/designly/zoom-pill-pos';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';

/** The zoom pill's place and whether it is hidden, shared by every canvas of the page. */
export const ZOOM_PILL = pillStore(PILL_KEY.zoom);

export const ZoomPill = ({ zoom }: { zoom: IZoomControls }) => {
  const ref = useRef<HTMLDivElement>(null);
  const pill = useFloatingPill(ref, ZOOM_PILL);
  const { pos, hidden } = pill.prefs;
  const { menu } = pill;

  if (hidden) return null;
  const pick = (action: () => void) => {
    pill.closeMenu();
    action();
  };
  return (
    <div ref={ref} className="hx-vpzoom" data-placed={pos !== null} {...pill.bind}>
      <ZoomControls zoom={zoom} />
      {menu.value !== null && (
        <div
          className="hx-site-menu hx-zoompill__menu"
          data-state={menu.state}
          role="menu"
          aria-label={t('next.site.zoomPill')}
          onPointerDown={(event) => event.stopPropagation()}
        >
          <div className="hx-dl-pop__label">{t('next.site.zoom')}</div>
          <div className="hx-zoompill__scales">
            {ZOOM_PRESETS.map((pct) => (
              <button
                key={pct}
                type="button"
                role="menuitemradio"
                className="hx-zoompill__scale"
                aria-checked={zoom.pct === pct}
                aria-disabled={zoom.locked}
                onClick={() => !zoom.locked && pick(() => zoom.zoomTo(pct))}
              >
                {pct}%
              </button>
            ))}
          </div>
          <button type="button" role="menuitem" className="hx-option" onClick={() => pick(zoom.fit)}>
            <Icon name="maximize-2" size="sm" />
            <span className="hx-option__name">{t('next.site.zoomFit')}</span>
            <kbd className="hx-zoompill__kbd">{fitKeyText()}</kbd>
          </button>
          <button
            type="button"
            role="menuitemcheckbox"
            className="hx-option"
            aria-checked={zoom.locked}
            onClick={() => pick(zoom.onLock)}
          >
            <Icon name={zoom.locked ? 'lock' : 'lock-open'} size="sm" />
            <span className="hx-option__name">
              {zoom.locked ? t('next.site.unlockView') : t('next.site.lockView')}
            </span>
          </button>
          <PillMenuFoot
            placed={pill.prefs.pos !== null}
            onReset={pill.reset}
            onHide={pill.hide}
            resetLabel={t('next.site.zoomPillCorner')}
            hideLabel={t('next.site.zoomPillHide')}
            hint={t('next.site.zoomPillHint')}
          />
        </div>
      )}
    </div>
  );
};
