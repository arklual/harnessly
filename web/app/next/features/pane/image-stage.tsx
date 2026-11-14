// A picture in the pane: fitted, and it zooms in place with the wheel, a pinch or a double click, a drag pans it (the image
// viewer's own gestures, ui/use-image-zoom.ts). Shift and a drag mark a region of it, which the chat hears of. The pencil
// in its corner opens it in the editor, where the person marks it and the marked picture goes into the chat's field.
import { useEffect, useRef, useState, type PointerEvent } from 'react';
import useImageZoom from '@/next/ui/use-image-zoom';
import ImageEditView from '@/next/features/image-edit/image-editor';
import { useMarkupTarget } from '@/next/features/image-edit/to-composer';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import type { DraftKey } from '@/next/stores/drafts';
import { regionSelection, type IFileSelection } from '@/next/features/pane/file-selection';

interface IShare {
  x: number;
  y: number;
}

interface IRegion {
  from: IShare;
  to: IShare;
}

interface IImageStageProps {
  src: string;
  name: string;
  onSelection?: (selection: IFileSelection | null) => void;
  /** The field the marked picture goes to (the chat the pane is beside). */
  markTo?: DraftKey;
}

const SMALLEST_SHARE = 0.005;
const clampShare = (value: number) => Math.min(1, Math.max(0, value));

const ImageStage = ({ src, name, onSelection, markTo }: IImageStageProps) => {
  const [editing, setEditing] = useState(false);
  const annotate = useMarkupTarget({ to: markTo });
  const stage = useRef<HTMLDivElement | null>(null);
  const image = useRef<HTMLImageElement | null>(null);
  const box = useRef<HTMLDivElement | null>(null);
  const marking = useRef(false);
  const [region, setRegion] = useState<IRegion | null>(null);
  useImageZoom(stage, image, true, src);

  const shareOf = (event: PointerEvent<HTMLDivElement>): IShare | null => {
    const rect = image.current?.getBoundingClientRect();
    if (!rect || rect.width === 0 || rect.height === 0) return null;
    return {
      x: clampShare((event.clientX - rect.left) / rect.width),
      y: clampShare((event.clientY - rect.top) / rect.height),
    };
  };

  // The box follows the picture as it zooms and pans: its place is a share of the picture, drawn each frame while it is there.
  useEffect(() => {
    if (!region) return;
    const left = Math.min(region.from.x, region.to.x);
    const top = Math.min(region.from.y, region.to.y);
    const width = Math.abs(region.to.x - region.from.x);
    const height = Math.abs(region.to.y - region.from.y);
    let frame = requestAnimationFrame(function tick() {
      const rect = image.current?.getBoundingClientRect();
      const home = stage.current?.getBoundingClientRect();
      const style = box.current?.style;
      if (rect && home && style) {
        style.setProperty('--x', `${rect.left - home.left + left * rect.width}px`);
        style.setProperty('--y', `${rect.top - home.top + top * rect.height}px`);
        style.setProperty('--w', `${width * rect.width}px`);
        style.setProperty('--h', `${height * rect.height}px`);
      }
      frame = requestAnimationFrame(tick);
    });
    return () => cancelAnimationFrame(frame);
  }, [region]);

  const finish = () => {
    if (!marking.current) return;
    marking.current = false;
    const element = image.current;
    if (!region || !element) return;
    const width = Math.abs(region.to.x - region.from.x);
    const height = Math.abs(region.to.y - region.from.y);
    if (width < SMALLEST_SHARE || height < SMALLEST_SHARE) {
      setRegion(null);
      onSelection?.(null);
      return;
    }
    const { naturalWidth, naturalHeight } = element;
    onSelection?.(
      regionSelection(
        Math.round(Math.min(region.from.x, region.to.x) * naturalWidth),
        Math.round(Math.min(region.from.y, region.to.y) * naturalHeight),
        Math.round(width * naturalWidth),
        Math.round(height * naturalHeight),
      ),
    );
  };

  const stageView = (
    <div
      className="hx-deliv__stage"
      ref={stage}
      // Before the viewer's own gestures: with Shift a drag marks, it does not pan.
      onPointerDownCapture={(event) => {
        if (!event.shiftKey || event.button !== 0) return;
        const at = shareOf(event);
        if (!at) return;
        event.stopPropagation();
        event.preventDefault();
        event.currentTarget.setPointerCapture(event.pointerId);
        marking.current = true;
        setRegion({ from: at, to: at });
      }}
      onPointerMove={(event) => {
        if (!marking.current) return;
        const at = shareOf(event);
        if (at) setRegion((before) => before && { from: before.from, to: at });
      }}
      onPointerUp={finish}
      onPointerCancel={finish}
      onKeyDown={(event) => {
        if (event.key !== 'Escape' || !region) return;
        setRegion(null);
        onSelection?.(null);
      }}
    >
      <img ref={image} className="hx-deliv__image" src={src} alt={name} draggable={false} />
      {region && <div className="hx-deliv__region" ref={box} aria-hidden="true" />}
      <button
        type="button"
        className="hx-deliv__mark"
        aria-label={t('next.imageEdit.part.markup')}
        title={t('next.imageEdit.part.markup')}
        onClick={() => setEditing(true)}
      >
        <Icon name="pencil" size="sm" />
      </button>
    </div>
  );
  // Beside the stage, not in it: the editor's presses must not reach the stage's own gestures through the portal.
  return (
    <>
      {stageView}
      <ImageEditView
        item={editing ? { src, name } : null}
        editing={null}
        annotate={annotate}
        onClose={() => setEditing(false)}
      />
    </>
  );
};

export default ImageStage;
