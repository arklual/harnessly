// The inspector of the selected overlay: a slim bar under the picture with its look (kind, way in, way out,
// theme) and «Удалить». The pickers are our own lists in a popover; the bar leaves with an animation (a closing phase).
import { useState } from 'react';
import { kindPatch } from '@/next/features/designly/video/overlays-model';
import type { IVideoOverlayEditor } from '@/next/features/designly/video/use-video-overlays';
import {
  VIDEO_OVERLAY_ENTERS,
  VIDEO_OVERLAY_KINDS,
  VIDEO_OVERLAY_LEAVES,
  VIDEO_OVERLAY_THEMES,
  type IVideoOverlay,
  type TVideoFormat,
} from '@/next/features/designly/video/types';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Popover from '@/next/ui/popover';
import { PRESENCE, usePresence } from '@/next/ui/presence';

const OUT_MS = 180;

interface IPickProps<V extends string> {
  label: string;
  value: V;
  options: readonly V[];
  nameOf: (value: V) => string;
  onPick: (value: V) => void;
}

const Pick = <V extends string>({ label, value, options, nameOf, onPick }: IPickProps<V>) => {
  const [open, setOpen] = useState(false);
  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label={label}
      className="hx-ovl-pick"
      side="top"
      trigger={
        <button type="button" className="hx-ovl-insp__pick" aria-label={label} title={label}>
          <span>{nameOf(value)}</span>
          <Icon name="chevron-down" size="sm" />
        </button>
      }
    >
      <ul role="listbox" aria-label={label}>
        {options.map((option) => (
          <li key={option} role="presentation">
            <button
              type="button"
              role="option"
              aria-selected={option === value}
              data-on={option === value ? '1' : '0'}
              onClick={() => {
                setOpen(false);
                if (option !== value) onPick(option);
              }}
            >
              {nameOf(option)}
            </button>
          </li>
        ))}
      </ul>
    </Popover>
  );
};

export interface IVideoOverlayInspectorProps {
  editor: IVideoOverlayEditor;
  page: string;
  /** The selected overlay (with its draft), or null. */
  overlay: IVideoOverlay | null;
  format: TVideoFormat;
}

const VideoOverlayInspector = ({ editor, page, overlay, format }: IVideoOverlayInspectorProps) => {
  const shown = usePresence(overlay, OUT_MS);
  const o = shown.value;
  if (o === null) return null;
  const set = (patch: Record<string, unknown>) => void editor.save(page, o.id, patch);
  return (
    <div
      className="hx-ovl-insp"
      role="toolbar"
      aria-label={t('next.video.overlay.inspector')}
      data-state={shown.state === PRESENCE.closing ? 'closing' : 'open'}
    >
      <Pick
        label={t('next.video.overlay.kind')}
        value={o.kind}
        options={VIDEO_OVERLAY_KINDS}
        nameOf={(v) => t(`next.video.overlay.kind.${v}`)}
        onPick={(v) => set(kindPatch(o, v, format))}
      />
      <Pick
        label={t('next.video.overlay.enter')}
        value={o.enter}
        options={VIDEO_OVERLAY_ENTERS}
        nameOf={(v) => t(`next.video.overlay.enter.${v}`)}
        onPick={(v) => set({ enter: v })}
      />
      <Pick
        label={t('next.video.overlay.leave')}
        value={o.leave}
        options={VIDEO_OVERLAY_LEAVES}
        nameOf={(v) => t(`next.video.overlay.leave.${v}`)}
        onPick={(v) => set({ leave: v })}
      />
      <div className="hx-ovl-insp__themes" role="radiogroup" aria-label={t('next.video.overlay.theme')}>
        {VIDEO_OVERLAY_THEMES.map((theme) => (
          <button
            key={theme}
            type="button"
            role="radio"
            aria-checked={o.theme === theme}
            aria-label={t(`next.video.overlay.theme.${theme}`)}
            title={t(`next.video.overlay.theme.${theme}`)}
            data-theme={theme}
            onClick={() => o.theme !== theme && set({ theme })}
          />
        ))}
      </div>
      <button
        type="button"
        className="hx-video__btn"
        aria-label={t('next.video.overlay.remove')}
        title={t('next.video.overlay.remove')}
        onClick={() => void editor.remove(page, o.id)}
      >
        <Icon name="trash-2" />
      </button>
    </div>
  );
};

export default VideoOverlayInspector;
