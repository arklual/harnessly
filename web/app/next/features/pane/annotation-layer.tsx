// The annotations over a browser tab's page: a numbered mark at the corner of each annotated element, the small row of
// the comment that opens beside the mark, and the floating bar at the foot of the page («Аннотирование · N»). A mark is
// kept on its element by the bridge (`tk:anchors` / `tk:anchored`, the same as the pins of a design's notes): it writes
// its place on its own element on every word of the page, with no render per scroll.
import { useLayoutEffect, useRef, useState, type RefObject } from 'react';
import { fixedPin, placePin, type IAnchorStore } from '@/next/features/designly/note-anchor';
import { annotationName, numberOf, type IAnnotation } from '@/next/features/pane/annotations';
import Icon from '@/next/ui/icon';
import { PRESENCE, useLeavingList, usePresence, usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';

/** The comment row's exit and the bar's, as long as hx-tann-out in tab-annotations.css. */
export const ANNOTATION_OUT_MS = 160;
/** A row that would run past this edge of the page opens on the other side of its mark. */
const ROW_W = 280;
const ROW_H = 96;

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

interface IRowProps {
  annotation: IAnnotation;
  open: boolean;
  onCommit: (comment: string) => void;
  onRemove: () => void;
  onCancel: () => void;
}

/** The comment of one mark: a field and ✓; Enter keeps it, Esc leaves it; 🗑 takes the mark away. */
const Row = ({ annotation, open, onCommit, onRemove, onCancel }: IRowProps) => {
  const [text, setText] = useState(annotation.comment);
  return (
    <form
      className="hx-tann__row"
      data-state={open ? 'open' : 'closing'}
      aria-label={t('next.pane.annot.row')}
      onSubmit={(event) => {
        event.preventDefault();
        onCommit(text.trim());
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault();
          event.stopPropagation();
          onCancel();
        }
      }}
    >
      <input
        className="hx-tann__input"
        value={text}
        autoFocus
        placeholder={t('next.pane.annot.placeholder')}
        aria-label={t('next.pane.annot.placeholder')}
        onChange={(event) => setText(event.target.value)}
      />
      <button
        type="button"
        className="hx-tann__btn"
        aria-label={t('next.pane.annot.remove')}
        onClick={onRemove}
      >
        <Icon name="trash-2" size="sm" />
      </button>
      <button type="submit" className="hx-tann__btn" data-primary aria-label={t('next.pane.annot.save')}>
        <Icon name="check" size="sm" />
      </button>
    </form>
  );
};

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
  const row = usePresentFlag(editing, ANNOTATION_OUT_MS);
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
      el.dataset.flip = left > box.width - ROW_W ? 'left' : 'right';
      el.dataset.up = top > box.height - ROW_H ? 'true' : 'false';
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
    <div
      ref={ref}
      className="hx-tann__mark"
      data-editing={editing || undefined}
      data-leaving={leaving || undefined}
      data-away={away || undefined}
      data-sending={sending || undefined}
      data-empty={annotation.comment === '' || undefined}
    >
      <button
        type="button"
        className="hx-tann__dot"
        aria-label={t('next.pane.annot.mark', { n, name: annotationName(annotation) })}
        tabIndex={leaving || away ? -1 : undefined}
        onClick={() => onOpen(id)}
      >
        <span key={n} className="hx-tann__num">
          {n}
        </span>
      </button>
      {row.mounted && (
        <Row
          annotation={annotation}
          open={row.state === PRESENCE.open}
          onCommit={(comment) => onCommit(id, comment)}
          onRemove={() => onRemove(id)}
          onCancel={onCancel}
        />
      )}
    </div>
  );
};

/** The same page: its address without the hash. */
const samePage = (a: string, b: string | null): boolean => b !== null && a.split('#')[0] === b.split('#')[0];

/** The marks over the page; hidden, they fade out but stay where they are, and a removed one fades out in its place. */
export const AnnotationLayer = ({ list, editing, hidden, sending, at, ...rest }: ILayerProps) => {
  const shown = useLeavingList(list, (a) => a.id, ANNOTATION_OUT_MS);
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

const BarButton = ({
  icon,
  label,
  disabled,
  pressed,
  onClick,
}: {
  icon: string;
  label: string;
  disabled?: boolean;
  pressed?: boolean;
  onClick: () => void;
}) => (
  <button
    type="button"
    className="hx-tann__tool hx-tipped"
    aria-label={label}
    aria-pressed={pressed}
    aria-disabled={disabled || undefined}
    onClick={() => !disabled && onClick()}
  >
    <Icon name={icon} size="sm" />
    <span className="hx-tip">{label}</span>
  </button>
);

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
  // The count the bar had while it leaves, so «0» does not flash in its exit.
  const bar = usePresence(visible ? count : null, ANNOTATION_OUT_MS);
  if (bar.value === null) return null;
  const empty = count === 0;
  return (
    <div
      className="hx-tann__bar"
      role="toolbar"
      aria-label={t('next.pane.annot.title')}
      data-state={bar.state === PRESENCE.closing ? 'closing' : 'open'}
    >
      <span className="hx-tann__title">{t('next.pane.annot.bar', { n: bar.value })}</span>
      <BarButton
        icon={hidden ? 'eye' : 'eye-off'}
        label={hidden ? t('next.pane.annot.show') : t('next.pane.annot.hide')}
        pressed={hidden}
        disabled={empty}
        onClick={onToggleHidden}
      />
      <BarButton icon="trash-2" label={t('next.pane.annot.clear')} disabled={empty} onClick={onClear} />
      <button
        type="button"
        className="hx-tann__send"
        aria-disabled={empty || sending || undefined}
        aria-busy={sending || undefined}
        onClick={() => !empty && !sending && onSend()}
      >
        {t('next.pane.annot.send')}
      </button>
      <BarButton icon="x" label={t('next.pane.annot.close')} onClick={onClose} />
    </div>
  );
};
