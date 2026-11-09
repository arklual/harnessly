// The annotations over a browser tab's page: a numbered mark at the corner of each annotated element, the small row of
// the comment that opens beside the mark, and the floating bar at the foot of the page («Аннотирование · N»). A mark is
// kept on its element by the bridge (`tk:anchors` / `tk:anchored`, the same as the pins of a design's notes): it writes
// its place on its own element on every word of the page, with no render per scroll.
import { useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { fixedPin, placePin, type IAnchorStore } from '@/next/features/designly/note-anchor';
import { annotationName, numberOf, type IAnnotation } from '@/next/features/pane/annotations';
import { NOTE_OUT_MS, NOTE_ROW_H, NOTE_ROW_W, NoteBar, NoteBarButton, NoteMark } from '@/next/ui/notes/notes';
import { useLeavingList } from '@/next/ui/presence';
import { t } from '@/next/i18n';

export interface IAnnotationEditing {
  id: string;
  /** Just put by a click: leaving it without a word takes the mark back. */
  fresh: boolean;
}

interface ILayerProps {
  list: readonly IAnnotation[];
  editing: IAnnotationEditing | null;
  hidden: boolean;
  /** The marks are being sent: they fade, and the list is cleared when they are gone. */
  sending: boolean;
  /** The page's address now; marks of another page fade out and stay in the list. */
  at: string | null;
  anchors: IAnchorStore;
  frame: RefObject<HTMLIFrameElement | null>;
  stage: RefObject<HTMLDivElement | null>;
  fit: number;
  onOpen: (id: string) => void;
  onCommit: (id: string, comment: string) => void;
  onRemove: (id: string) => void;
  onCancel: () => void;
}

interface IMarkProps extends Omit<ILayerProps, 'list' | 'editing' | 'hidden' | 'sending' | 'at'> {
  annotation: IAnnotation;
  n: number;
  editing: boolean;
  leaving: boolean;
  /** The mark belongs to another page than the one open. */
  away: boolean;
  sending: boolean;
}

const Mark = ({
  annotation,
  n,
  editing,
  leaving,
  away,
  sending,
  anchors,
  frame,
  stage,
  fit,
  onOpen,
  onCommit,
  onRemove,
  onCancel,
}: IMarkProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const { id, rect } = annotation;
  const { x, y, width, height } = rect;
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const draw = () => {
      const report = anchors.get();
      const geo = report?.items.get(id);
      const at = geo && report ? placePin(geo, report.vw, report.vh) : fixedPin({ x, y, width, height });
      const page = frame.current?.getBoundingClientRect();
      const box = stage.current?.getBoundingClientRect();
      if (!page || !box) return;
      const left = page.left - box.left + (at.box.x + at.mark.x) * fit;
      const top = page.top - box.top + (at.box.y + at.mark.y) * fit;
      el.dataset.place = at.place;
      el.dataset.flip = left > box.width - NOTE_ROW_W ? 'left' : 'right';
      el.dataset.up = top > box.height - NOTE_ROW_H ? 'true' : 'false';
      el.style.setProperty('--x', `${Math.round(left)}px`);
      el.style.setProperty('--y', `${Math.round(top)}px`);
    };
    // At most one write per frame, whatever the page says in between.
    let frameId = 0;
    const later = () => {
      if (frameId === 0)
        frameId = requestAnimationFrame(() => {
          frameId = 0;
          draw();
        });
    };
    draw();
    const off = anchors.subscribe(later);
    return () => {
      off();
      cancelAnimationFrame(frameId);
    };
  }, [anchors, frame, stage, fit, id, x, y, width, height]);

  return (
    <NoteMark
      markRef={ref}
      n={n}
      label={t('next.pane.annot.mark', { n, name: annotationName(annotation) })}
      comment={annotation.comment}
      editing={editing}
      leaving={leaving}
      away={away}
      sending={sending}
      rowLabel={t('next.pane.annot.row')}
      placeholder={t('next.pane.annot.placeholder')}
      onOpen={() => onOpen(id)}
      onCommit={(comment) => onCommit(id, comment)}
      onRemove={() => onRemove(id)}
      onCancel={onCancel}
    />
  );
};

/** The same page: its address without the hash. */
const samePage = (a: string, b: string | null): boolean => b !== null && a.split('#')[0] === b.split('#')[0];

/** The marks over the page; hidden, they fade out but stay where they are, and a removed one fades out in its place. */
export const AnnotationLayer = ({ list, editing, hidden, sending, at, ...rest }: ILayerProps) => {
  const shown = useLeavingList(list, (a) => a.id, NOTE_OUT_MS);
  // A mark that is leaving keeps the number it had.
  const current = list.map((a) => `${a.id}:${numberOf(list, a.id)}`).join(' ');
  const [numbers, setNumbers] = useState(new Map<string, number>());
  const [seen, setSeen] = useState('');
  if (seen !== current) {
    setSeen(current);
    setNumbers((was) => new Map([...was, ...list.map((a) => [a.id, numberOf(list, a.id)] as const)]));
  }
  return (
    <div className="hx-tann" data-hidden={hidden || undefined}>
      {shown.map(({ item: annotation, leaving }) => {
        return (
          <Mark
            key={annotation.id}
            {...rest}
            annotation={annotation}
            n={leaving ? (numbers.get(annotation.id) ?? 0) : numberOf(list, annotation.id)}
            editing={!leaving && editing?.id === annotation.id}
            leaving={leaving}
            away={at !== null && !samePage(annotation.url, at)}
            sending={sending}
          />
        );
      })}
    </div>
  );
};

interface IBarProps {
  /** The mode is on or there are marks: the bar stands. */
  visible: boolean;
  count: number;
  hidden: boolean;
  /** «Отправить» was pressed: the button waits until the marks are gone. */
  sending: boolean;
  onToggleHidden: () => void;
  onClear: () => void;
  onSend: () => void;
  onClose: () => void;
}

/** «Аннотирование · N»: hide the marks, delete all, send, close. */
export const AnnotationBar = ({
  visible,
  count,
  hidden,
  sending,
  onToggleHidden,
  onClear,
  onSend,
  onClose,
}: IBarProps) => {
  const empty = count === 0;
  return (
    <NoteBar
      className="hx-tann__bar"
      visible={visible}
      count={count}
      sending={sending}
      sendLabel={t('next.pane.annot.send')}
      onSend={onSend}
      tools={
        <>
          <NoteBarButton
            icon={hidden ? 'eye' : 'eye-off'}
            label={hidden ? t('next.pane.annot.show') : t('next.pane.annot.hide')}
            pressed={hidden}
            disabled={empty}
            onClick={onToggleHidden}
          />
          <NoteBarButton
            icon="trash-2"
            label={t('next.pane.annot.clear')}
            disabled={empty}
            onClick={onClear}
          />
        </>
      }
      after={<NoteBarButton icon="x" label={t('next.pane.annot.close')} onClick={onClose} />}
    />
  );
};
