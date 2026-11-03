// The overlay layer over the picture: while the mode is on and the player is paused, every overlay that is on
// screen at the playhead has a box. A click selects, a drag moves, the corner resizes, a callout's point is dragged
// too, a double click types the text in place (Enter and a click away save, Esc cancels), Del removes. What the
// runtime draws underneath follows the drag (the player sends it the draft); the save is on release.
import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from 'react';
import { useVars } from '@/next/features/designly/deck/use-vars';
import {
  DRAG,
  dragPlace,
  placeOf,
  placePatch,
  roundPlace,
  visibleOverlays,
  withPlace,
  type TDrag,
} from '@/next/features/designly/video/overlays-model';
import type { IVideoOverlayEditor } from '@/next/features/designly/video/use-video-overlays';
import {
  VIDEO_OVERLAY_TEXT_MAX,
  type IVideoOverlay,
  type IVideoScene,
  type TVideoFormat,
} from '@/next/features/designly/video/types';
import { t } from '@/next/i18n';

/** A press that moves less than this is a click, px. */
const SLOP_PX = 4;

const pct = (v: number) => `${(v * 100).toFixed(3)}%`;

interface IDrag {
  id: string;
  mode: TDrag;
  px: number;
  py: number;
  pointer: number;
  from: ReturnType<typeof placeOf>;
  moved: boolean;
}

interface IBoxProps {
  overlay: IVideoOverlay;
  format: TVideoFormat;
  selected: boolean;
  onDown: (event: PointerEvent<HTMLElement>, mode: TDrag, overlay: IVideoOverlay) => void;
  onMove: (event: PointerEvent<HTMLElement>) => void;
  onUp: (event: PointerEvent<HTMLElement>) => void;
  onEdit: (id: string) => void;
}

const Box = ({ overlay, format, selected, onDown, onMove, onUp, onEdit }: IBoxProps) => {
  const p = placeOf(overlay, format);
  const ref = useVars<HTMLDivElement>({ '--x': pct(p.x), '--y': pct(p.y), '--w': pct(p.w), '--h': pct(p.h) });
  const targetRef = useVars<HTMLButtonElement>({
    '--x': pct(p.tx ?? 0),
    '--y': pct(p.ty ?? 0),
  });
  const callout = p.tx !== null && p.ty !== null;
  return (
    <>
      <div
        ref={ref}
        className="hx-ovl__box"
        data-kind={overlay.kind}
        data-selected={selected ? '1' : '0'}
        role="button"
        tabIndex={-1}
        aria-label={overlay.text || t(`next.video.overlay.kind.${overlay.kind}`)}
        onPointerDown={(event) => onDown(event, DRAG.move, overlay)}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onUp}
        onDoubleClick={() => onEdit(overlay.id)}
      >
        {selected && (
          <span
            className="hx-ovl__grip"
            aria-hidden="true"
            onPointerDown={(event) => onDown(event, DRAG.resize, overlay)}
            onPointerMove={onMove}
            onPointerUp={onUp}
            onPointerCancel={onUp}
          />
        )}
      </div>
      {selected && callout && (
        <button
          ref={targetRef}
          type="button"
          className="hx-ovl__target"
          aria-label={t('next.video.overlay.target')}
          onPointerDown={(event) => onDown(event, DRAG.target, overlay)}
          onPointerMove={onMove}
          onPointerUp={onUp}
          onPointerCancel={onUp}
        />
      )}
    </>
  );
};

interface IEditorProps {
  overlay: IVideoOverlay;
  format: TVideoFormat;
  onDone: (text: string | null) => void;
}

/** The text typed in place: a field over the box. */
const TextEditor = ({ overlay, format, onDone }: IEditorProps) => {
  const p = placeOf(overlay, format);
  const ref = useVars<HTMLTextAreaElement>({
    '--x': pct(p.x),
    '--y': pct(p.y),
    '--w': pct(p.w),
    '--h': pct(p.h),
  });
  const [value, setValue] = useState(overlay.text);
  const done = useRef(false);
  useEffect(() => {
    const el = ref.current;
    el?.focus({ preventScroll: true });
    el?.select();
  }, [ref]);
  const finish = (text: string | null) => {
    if (done.current) return;
    done.current = true;
    onDone(text);
  };
  const onKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      finish(value);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      finish(null);
    }
  };
  return (
    <textarea
      ref={ref}
      className="hx-ovl__text"
      data-anchor={overlay.anchor}
      value={value}
      maxLength={VIDEO_OVERLAY_TEXT_MAX}
      aria-label={t('next.video.overlay.text')}
      onChange={(event) => setValue(event.target.value)}
      onKeyDown={onKey}
      onBlur={() => finish(value)}
    />
  );
};

export interface IVideoOverlayLayerProps {
  editor: IVideoOverlayEditor;
  scene: IVideoScene;
  /** The playhead in the scene, ms. */
  local: number;
  format: TVideoFormat;
}

const VideoOverlayLayer = ({ editor, scene, local, format }: IVideoOverlayLayerProps) => {
  const root = useRef<HTMLDivElement>(null);
  const drag = useRef<IDrag | null>(null);
  const { selected, editing } = editor;
  const list = editor.overlaysOf(scene);
  const shown = visibleOverlays(list, local, scene.dur);
  const page = scene.id;

  const onDown = (event: PointerEvent<HTMLElement>, mode: TDrag, overlay: IVideoOverlay) => {
    if (event.button !== 0 || editing === overlay.id) return;
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    editor.select(overlay.id);
    drag.current = {
      id: overlay.id,
      mode,
      px: event.clientX,
      py: event.clientY,
      pointer: event.pointerId,
      from: placeOf(overlay, format),
      moved: false,
    };
  };
  const place = (event: PointerEvent<HTMLElement>, d: IDrag) => {
    const box = root.current?.getBoundingClientRect();
    if (!box || box.width <= 0 || box.height <= 0) return null;
    const dx = (event.clientX - d.px) / box.width;
    const dy = (event.clientY - d.py) / box.height;
    return roundPlace(dragPlace(d.from, d.mode, dx, dy, format));
  };
  const find = (id: string) => list.find((o) => o.id === id);
  const onMove = (event: PointerEvent<HTMLElement>) => {
    const d = drag.current;
    if (!d || d.pointer !== event.pointerId) return;
    if (!d.moved && Math.hypot(event.clientX - d.px, event.clientY - d.py) < SLOP_PX) return;
    d.moved = true;
    const o = find(d.id);
    const next = place(event, d);
    if (o && next) editor.preview(page, withPlace(o, format, next));
  };
  const onUp = (event: PointerEvent<HTMLElement>) => {
    const d = drag.current;
    if (!d || d.pointer !== event.pointerId) return;
    drag.current = null;
    if (!d.moved) return;
    const next = place(event, d);
    if (next === null || event.type === 'pointercancel') {
      editor.discard(d.id);
      return;
    }
    void editor.save(page, d.id, placePatch(format, next));
  };

  // Del removes the selected overlay, Esc lets it go; a key typed into a field is the field's.
  useEffect(() => {
    if (selected === null || editing !== null) return;
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key !== 'Delete' && event.key !== 'Backspace' && event.key !== 'Escape') return;
      const target = event.target;
      if (target instanceof HTMLElement && target.closest('input, textarea, select, [contenteditable]'))
        return;
      event.preventDefault();
      if (event.key === 'Escape') editor.select(null);
      else void editor.remove(page, selected);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selected, editing, editor, page]);

  const typing = editing === null ? undefined : find(editing);
  const finishText = (overlay: IVideoOverlay, text: string | null) => {
    editor.setEditing(null);
    if (text === null) return;
    const clean = text.trim();
    if (clean === overlay.text || (clean === '' && overlay.kind !== 'highlight')) return;
    editor.preview(page, { ...overlay, text: clean });
    void editor.save(page, overlay.id, { text: clean });
  };

  return (
    <div
      ref={root}
      className="hx-ovl"
      onPointerDown={() => {
        editor.select(null);
      }}
    >
      {shown.map((o) => (
        <Box
          key={o.id}
          overlay={o}
          format={format}
          selected={selected === o.id}
          onDown={onDown}
          onMove={onMove}
          onUp={onUp}
          onEdit={editor.setEditing}
        />
      ))}
      {typing && (
        <TextEditor
          key={typing.id}
          overlay={typing}
          format={format}
          onDone={(text) => finishText(typing, text)}
        />
      )}
    </div>
  );
};

export default VideoOverlayLayer;
