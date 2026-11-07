// The marks sent from the pane's browser, as chips in the field's row: each its own chip with the mark's number and the
// element's name. A card over the chip (hover or keyboard focus, ui/use-hover-intent.ts) says where the mark is — the
// page, the element, the component — and holds the comment to change (Enter saves, Esc takes the change back) and
// «Delete»; × on the chip deletes too. A chip comes in from a little below (a batch of them one after another) and goes
// with its own exit; the others slide to the freed place (ui/use-flip.ts). Only transform and opacity move.
import { useLayoutEffect, useRef, useState, type FocusEvent, type KeyboardEvent } from 'react';
import { annotationName, type IAnnotation } from '@/next/features/pane/annotations';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { useLeavingList } from '@/next/ui/presence';
import useFlip from '@/next/ui/use-flip';
import { useFitInViewport, useHoverIntent } from '@/next/ui/use-hover-intent';

/** A chip's exit, as long as hx-pagenote-out in composer.css. */
export const NOTE_EXIT_MS = 200;
/** The step between chips of one batch coming in, and the most steps the ladder has. */
export const NOTE_STEP_MS = 45;
export const NOTE_STEPS_MAX = 6;

export interface IAnnotationChipsProps {
  notes: readonly IAnnotation[];
  onEdit: (id: string, comment: string) => void;
  onRemove: (id: string) => void;
}

interface ILadder {
  key: string;
  known: ReadonlySet<string>;
  steps: ReadonlyMap<string, number>;
}

const idOf = (note: IAnnotation): string => note.id;

interface INoteChipProps {
  note: IAnnotation;
  n: number;
  /** Place in the ladder of its batch; 0 when it comes alone or is not new. */
  step: number;
  leaving: boolean;
  onEdit: (id: string, comment: string) => void;
  onRemove: (id: string) => void;
}

const NoteChip = ({ note, n, step, leaving, onEdit, onRemove }: INoteChipProps) => {
  const wrapRef = useRef<HTMLSpanElement>(null);
  const mainRef = useRef<HTMLButtonElement>(null);
  const { open, setTrigger, setFloating, getFloating, triggerProps, floatingProps, close } = useHoverIntent({
    disabled: leaving,
  });
  useFitInViewport(getFloating, open);
  const [draft, setDraft] = useState(note.comment);
  // A comment changed elsewhere (the page's own row) is the new starting point of the field.
  const [shown, setShown] = useState(note.comment);
  if (shown !== note.comment) {
    setShown(note.comment);
    setDraft(note.comment);
  }
  const dirty = draft.trim() !== note.comment.trim();
  const name = annotationName(note);

  useLayoutEffect(() => {
    wrapRef.current?.style.setProperty('--step', String(step));
  }, [step]);

  const save = () => {
    if (dirty) onEdit(note.id, draft.trim());
    mainRef.current?.focus();
  };
  const onKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault();
      save();
    } else if (event.key === 'Escape') {
      setDraft(note.comment);
      close();
      mainRef.current?.focus();
    }
  };
  // Focus leaving the whole chip (its button, ×, the card) lets the card go; moving between them keeps it.
  const onBlur = (event: FocusEvent<HTMLElement>) => {
    if (!wrapRef.current?.contains(event.relatedTarget as Node | null)) floatingProps.onBlur(event);
  };

  return (
    <span
      ref={wrapRef}
      className="hx-pagenote"
      data-flip={note.id}
      data-leaving={leaving || undefined}
      data-open={open || undefined}
      onBlur={onBlur}
    >
      <span
        className="hx-attach hx-pagenote__chip"
        data-kind="annotation"
        ref={setTrigger}
        onPointerEnter={triggerProps.onPointerEnter}
        onPointerLeave={triggerProps.onPointerLeave}
      >
        <button
          type="button"
          ref={mainRef}
          className="hx-pagenote__open"
          aria-label={t('next.composer.note.chip', { n, name })}
          aria-expanded={open}
          aria-haspopup="dialog"
          disabled={leaving}
          onFocus={triggerProps.onFocus}
          onClick={triggerProps.onClick}
        >
          <span className="hx-attach__thumb hx-pagenote__num">{n}</span>
          <span className="hx-attach__name">{name}</span>
        </button>
        <button
          type="button"
          className="hx-attach__x"
          aria-label={t('next.composer.removeAttachment', { name })}
          disabled={leaving}
          onFocus={triggerProps.onFocus}
          onClick={() => onRemove(note.id)}
        >
          <Icon name="x" size="sm" />
        </button>
      </span>
      <div
        className="hx-pagenote__pop hx-hover-fit"
        ref={setFloating}
        role="dialog"
        aria-label={t('next.composer.note.title', { n })}
        inert={!open}
        data-open={open}
        onPointerEnter={floatingProps.onPointerEnter}
        onPointerLeave={floatingProps.onPointerLeave}
      >
        <div className="hx-pagenote__head">
          <span className="hx-pagenote__title">{t('next.composer.note.title', { n })}</span>
        </div>
        <dl className="hx-pagenote__facts">
          <dt>{t('next.composer.note.page')}</dt>
          <dd title={note.url}>{note.url}</dd>
          <dt>{t('next.composer.note.element')}</dt>
          <dd title={note.selector}>{name}</dd>
          {note.component && (
            <>
              <dt>{t('next.composer.note.component')}</dt>
              <dd>{note.component.name}</dd>
            </>
          )}
        </dl>
        <textarea
          className="hx-pagenote__comment"
          value={draft}
          rows={2}
          placeholder={t('next.pane.annot.placeholder')}
          aria-label={t('next.pane.annot.row')}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={onKey}
          onFocus={triggerProps.onFocus}
        />
        <div className="hx-pagenote__foot">
          <span className="hx-pagenote__hint" data-on={dirty || undefined}>
            {t('next.composer.note.hint')}
          </span>
          <button type="button" className="hx-pagenote__del" onClick={() => onRemove(note.id)}>
            <Icon name="trash-2" size="sm" />
            <span>{t('next.composer.note.delete')}</span>
          </button>
        </div>
      </div>
    </span>
  );
};

/** The chips of the page notes; they stand among the field's other chips in a box of their own. */
export const AnnotationChips = ({ notes, onEdit, onRemove }: IAnnotationChipsProps) => {
  const rootRef = useRef<HTMLSpanElement>(null);
  useFlip(rootRef, false, 'x');
  const shown = useLeavingList(notes, idOf, NOTE_EXIT_MS);
  // The notes already on screen come in with no delay; the new ones of one render form a ladder (state adjusted during
  // render, as the chips' list is: a ref would be read while drawing).
  const ids = notes.map(idOf);
  const key = ids.join('\n');
  const [ladder, setLadder] = useState<ILadder>({ key: '', known: new Set(), steps: new Map() });
  if (ladder.key !== key) {
    const fresh = ids.filter((id) => !ladder.known.has(id));
    setLadder({
      key,
      known: new Set(ids),
      steps: new Map(fresh.map((id, i) => [id, Math.min(i, NOTE_STEPS_MAX)])),
    });
  }
  let n = 0;
  if (shown.length === 0) return null;
  return (
    <span ref={rootRef} className="hx-pagenotes">
      {shown.map(({ item, leaving }) => {
        // A chip that leaves shows the number it had.
        const num = leaving ? n + 1 : ++n;
        return (
          <NoteChip
            key={item.id}
            note={item}
            n={num}
            step={ladder.steps.get(item.id) ?? 0}
            leaving={leaving}
            onEdit={onEdit}
            onRemove={onRemove}
          />
        );
      })}
    </span>
  );
};
