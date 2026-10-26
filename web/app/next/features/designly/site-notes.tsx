// The person's edits on the page of a design: a note opens at the block they picked (the card .hx-note.hx-ann), a pin
// with its number stays on the block, the notes ride over the island's field as a strip «N annotations» with a list, or,
// in «Правки», in the bar at the foot (.hx-notesbar) that sends them to the agent in one message.
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { AnnotateQuote } from '@/next/ui/annotate/parts';
import Icon from '@/next/ui/icon';
import Sheet from '@/next/ui/sheet';
import { usePhone } from '@/next/ui/use-media';
import {
  changedKeys,
  hasEdit,
  OPACITY,
  shownValues,
  stepRadius,
  SWATCHES,
  withValue,
  type IStyleEdit,
  type TStyleKey,
} from '@/next/features/designly/element-style';
import { pickedLabel, type IPicked, type IPickedLook } from '@/next/features/designly/page-bridge';
import { DRAFT_ANCHOR, useNoteAnchor } from '@/next/features/designly/note-anchor';
import { dragNote, keepNotePos, readNotePos } from '@/next/features/designly/note-drag';
import type { IPoint } from '@/next/features/designly/site-drag';
import { plural, t } from '@/next/i18n';
import { PRESENCE, useLeavingList, usePresence, usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/notes.css';

export const NOTE_OUT_MS = 180;
const LIST_OUT_MS = 200;
const PIN_OUT_MS = 200;
const LASSO_OUT_MS = 160;
const SWATCHES_OUT_MS = 160;

/** Where a note opens: the block's bottom-left corner in the section's pixels with the canvas at its fit (the card
 *  follows the canvas's zoom through --zs/--zcx/--zcy), and the side it opens to. */
export interface INoteAt {
  x: number;
  y: number;
  right: boolean;
}

export interface INote {
  id: string;
  picked: IPicked;
  /** The page the block is on: its path on the library server. */
  page: string;
  text: string;
  /** Where its card opens again, from the pin or the list. */
  at: INoteAt;
  /** The look tried on the block; the page wears it while the note is there. */
  style: IStyleEdit;
}

/** A note as a chip of the field: its number and what it says, or the block it is on. */
export const noteQuote = (note: INote, n: number) => ({
  id: note.id,
  label: `#${n} ${note.text || note.picked.component || note.picked.text || note.picked.tag}`,
  title: note.text || note.picked.selector,
  remark: true,
  icon: 'square-dashed-mouse-pointer',
});

export interface INoteDraft {
  picked: IPicked;
  at: INoteAt;
  /** The note being changed; without it the card makes a new one. */
  id?: string;
  /** The words the card opens with. */
  text: string;
  /** The look on the block as the card has it now. */
  style: IStyleEdit;
}

/** The microphone of the card: dictation puts the words into the card's field. */
export interface INoteMic {
  recording: boolean;
  onToggle: () => void;
}

/**
 * The card of a guest of a link: it has only the words (the name was asked when commenting began), no block settings and no trash, and sends the comment to the
 * link's gateway: the card waits for the answer and stays, with its words, where it was refused.
 */
export interface INoteGuest {
  /** True when the comment was taken. */
  submit: (text: string) => Promise<boolean>;
}

export interface INotePopoverProps {
  n: number;
  draft: INoteDraft;
  text: string;
  onText: (text: string) => void;
  mic: INoteMic | null;
  /** «Правки» want words; the island takes a bare block too. */
  needsText: boolean;
  onOk: (text: string) => void;
  onCancel: () => void;
  /** The trash: a new card is dropped, a note is taken back. */
  onDelete: () => void;
  onStyle: (style: IStyleEdit) => void;
  /** The page no longer has the block: the look cannot be shown on it. */
  gone: boolean;
  /** Another page is on screen: the card steps away (it comes back with its page). */
  hidden?: boolean;
  guest?: INoteGuest;
}

/** The colour of a swatch reaches the CSS through a custom property of its own, not through a style attribute. */
const useSwatchColor = <E extends HTMLElement>(color: string | null) => {
  const ref = useRef<E>(null);
  useLayoutEffect(() => {
    if (color) ref.current?.style.setProperty('--sw', color);
  }, [color]);
  return ref;
};

/** The chip of the colour on the row; crossed out when there is none. */
const SwatchDot = ({ color }: { color: string | null }) => {
  const ref = useSwatchColor<HTMLElement>(color);
  return <i ref={ref} className="hx-ann__sw" data-none={color === null} />;
};

const SwatchButton = ({
  color,
  label,
  on,
  onPick,
}: {
  color: string | null;
  label: string;
  on: boolean;
  onPick: () => void;
}) => {
  const ref = useSwatchColor<HTMLButtonElement>(color);
  return (
    <button
      ref={ref}
      type="button"
      className="hx-ann__swb"
      role="radio"
      aria-checked={on}
      aria-label={label}
      data-none={color === null}
      onClick={onPick}
    />
  );
};

/** The opacity slider: the share of its track that is filled goes to the CSS as `--v`. */
const OpacityRange = ({ value, onChange }: { value: number; onChange: (value: number) => void }) => {
  const ref = useRef<HTMLInputElement>(null);
  useLayoutEffect(() => {
    const share = ((value - OPACITY.min) / (OPACITY.max - OPACITY.min)) * 100;
    ref.current?.style.setProperty('--v', `${Math.min(100, Math.max(0, share)).toFixed(1)}%`);
  }, [value]);
  return (
    <span className="hx-ann__range">
      <input
        ref={ref}
        type="range"
        min={OPACITY.min}
        max={OPACITY.max}
        step={OPACITY.step}
        value={Math.min(OPACITY.max, Math.max(OPACITY.min, value))}
        aria-label={t('next.notes.style.opacity')}
        onChange={(event) => onChange(Number(event.target.value))}
      />
      <span className="hx-ann__vtext">{value}%</span>
    </span>
  );
};

/** The settings of the picked block, as the card of the design has them: what it is, then the fill, the corners and
 *  the opacity; what is changed is dotted, and «Сбросить» gives the block back to the page. */
const NoteProps = ({
  look,
  edit,
  onEdit,
  open,
  gone,
}: {
  look: IPickedLook;
  edit: IStyleEdit;
  onEdit: (edit: IStyleEdit) => void;
  open: boolean;
  gone: boolean;
}) => {
  const [swatches, setSwatches] = useState(false);
  const sws = usePresentFlag(swatches && open, SWATCHES_OUT_MS);
  const shown = shownValues(edit, look);
  const changed = changedKeys(edit);
  const set = <K extends TStyleKey>(key: K, value: IStyleEdit[K]) =>
    onEdit(withValue(edit, look, key, value));
  const mark = (key: TStyleKey) => changed.includes(key);
  const fill = shown.background;
  return (
    <div className="hx-ann__props" aria-hidden={!open} inert={!open}>
      <div className="hx-ann__props-in">
        <div className="hx-ann__el">
          <span className="hx-ann__target">{t('next.notes.tune')}</span>
          <span className="hx-spacer" />
          <button
            type="button"
            className="hx-ann__reset"
            data-on={changed.length > 0}
            tabIndex={changed.length > 0 && open ? 0 : -1}
            onClick={() => {
              setSwatches(false);
              onEdit({ background: null, radius: null, opacity: null });
            }}
          >
            <Icon name="rotate-ccw" size="sm" />
            {t('next.notes.reset')}
          </button>
        </div>
        <div className="hx-ann__grid">
          <div className="hx-ann__prop" data-changed={mark('background')}>
            <span className="hx-ann__label">{t('next.notes.style.background')}</span>
            <button
              type="button"
              className="hx-ann__val"
              aria-expanded={swatches}
              onClick={() => setSwatches((on) => !on)}
            >
              <SwatchDot color={fill} />
              <span className="hx-ann__vtext">{fill ?? t('next.notes.style.asTheme')}</span>
            </button>
          </div>
          {sws.mounted && (
            <div
              className="hx-ann__sws"
              data-state={sws.state}
              role="radiogroup"
              aria-label={t('next.notes.style.background')}
            >
              {[null, ...SWATCHES].map((color) => (
                <SwatchButton
                  key={color ?? 'none'}
                  color={color}
                  label={color ?? t('next.notes.style.asTheme')}
                  on={color?.toLowerCase() === fill?.toLowerCase()}
                  onPick={() => set('background', color)}
                />
              ))}
            </div>
          )}
          <div className="hx-ann__prop" data-changed={mark('radius')}>
            <span className="hx-ann__label">{t('next.notes.style.radius')}</span>
            <span className="hx-ann__step">
              <button
                type="button"
                aria-label={t('next.notes.style.less')}
                onClick={() => set('radius', stepRadius(shown.radius, -1))}
              >
                <Icon name="minus" size="sm" />
              </button>
              <span className="hx-ann__vtext">{shown.radius === null ? '—' : `${shown.radius} px`}</span>
              <button
                type="button"
                aria-label={t('next.notes.style.more')}
                onClick={() => set('radius', stepRadius(shown.radius, 1))}
              >
                <Icon name="plus" size="sm" />
              </button>
            </span>
          </div>
          <div className="hx-ann__prop" data-changed={mark('opacity')}>
            <span className="hx-ann__label">{t('next.notes.style.opacity')}</span>
            <OpacityRange value={shown.opacity} onChange={(value) => set('opacity', value)} />
          </div>
        </div>
        {gone && <div className="hx-ann__hint">{t('next.notes.style.gone')}</div>}
      </div>
    </div>
  );
};

export const NotePopover = ({
  n,
  draft,
  text,
  onText,
  mic,
  needsText,
  onOk,
  onCancel,
  onDelete,
  onStyle,
  gone,
  hidden = false,
  guest,
}: INotePopoverProps) => {
  const ref = useRef<HTMLDivElement>(null);
  const [closing, setClosing] = useState(false);
  const [sending, setSending] = useState(false);
  // The block's settings are folded until asked for: the card opens small, over less of the page.
  const [tune, setTune] = useState(false);
  // Where the person moved the card (this session's); null while it stands at its block.
  const [moved, setMoved] = useState<IPoint | null>(readNotePos);
  // A guest only writes: the look of the block (its settings) is the owner's to try.
  const look = guest ? null : draft.picked.look;
  const wants = needsText && !text.trim() && !hasEdit(draft.style);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.setProperty('--nx', `${draft.at.x}px`);
    el.style.setProperty('--ny', `${draft.at.y}px`);
  }, [draft.at.x, draft.at.y]);

  useLayoutEffect(() => {
    if (!moved) return;
    ref.current?.style.setProperty('--mx', `${moved.x}px`);
    ref.current?.style.setProperty('--my', `${moved.y}px`);
  }, [moved]);

  // Its height goes to the CSS, which keeps the whole card inside the view as the settings unfold.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => el.style.setProperty('--nh', `${el.offsetHeight}px`);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const leave = (then: () => void) => {
    setClosing(true);
    window.setTimeout(then, NOTE_OUT_MS);
  };
  const ok = () => {
    if (wants || sending) return;
    if (!guest) {
      leave(() => onOk(text.trim()));
      return;
    }
    setSending(true);
    void guest.submit(text.trim()).then((taken) => {
      setSending(false);
      if (taken) leave(onCancel);
    });
  };

  return (
    <div
      ref={ref}
      className="hx-note hx-ann"
      data-side={draft.at.right ? 'end' : 'start'}
      data-state={closing || hidden ? 'closing' : 'open'}
      data-tune={look !== null && tune}
      data-moved={moved !== null}
      role="dialog"
      aria-label={t('next.notes.dialog')}
      aria-hidden={hidden}
      inert={hidden}
    >
      <div
        className="hx-ann__plate"
        onPointerDown={(event) =>
          dragNote(event, (at) => {
            setMoved(at);
            keepNotePos(at);
          })
        }
        onDoubleClick={() => {
          setMoved(null);
          keepNotePos(null);
        }}
      >
        <span className="hx-ann__grip" aria-hidden="true" />
        <AnnotateQuote
          lead={`<${draft.picked.tag}>`}
          text={draft.picked.text || draft.picked.component || ''}
        />
      </div>
      <div className="hx-ann__row">
        {look ? (
          <button
            type="button"
            className="hx-ann__tune hx-tipped"
            aria-pressed={tune}
            aria-label={t('next.notes.tune')}
            onClick={() => setTune((on) => !on)}
          >
            <Icon name="sliders-horizontal" />
            <span className="hx-tip" data-pos="below" data-align="start">
              {t('next.notes.tune')}
            </span>
          </button>
        ) : (
          <span className="hx-ann__n">{n}</span>
        )}
        <textarea
          className="hx-ann__input"
          autoFocus
          rows={1}
          disabled={sending}
          placeholder={guest ? t('next.guest.placeholder') : t('next.notes.placeholder')}
          value={text}
          onChange={(event) => onText(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
              event.preventDefault();
              ok();
            }
            if (event.key === 'Escape') {
              event.preventDefault();
              event.stopPropagation();
              leave(onCancel);
            }
          }}
        />
      </div>
      {look && <NoteProps look={look} edit={draft.style} onEdit={onStyle} open={tune} gone={gone} />}
      <div className="hx-ann__foot">
        <button type="button" className="hx-ann__cancel" onClick={() => leave(onCancel)}>
          {t('next.notes.revert')}
        </button>
        {!guest && (
          <button
            type="button"
            className="hx-ann__icon hx-tipped"
            aria-label={t('next.notes.delete')}
            onClick={() => leave(onDelete)}
          >
            <Icon name="trash-2" size="sm" />
            <span className="hx-tip" data-pos="below">
              {t('next.notes.deleteTip')}
            </span>
          </button>
        )}
        <span className="hx-spacer" />
        {mic && (
          <button
            type="button"
            className="hx-ann__icon hx-ann__mic hx-tipped"
            data-rec={mic.recording}
            aria-label={t('next.notes.dictate')}
            onClick={mic.onToggle}
          >
            <Icon name="mic" />
            <span className="hx-tip" data-pos="below" data-align="end">
              {t('next.notes.dictate')}
            </span>
          </button>
        )}
        <button
          type="button"
          className="hx-ann__ok hx-tipped"
          aria-label={guest ? t('next.guest.send') : t('next.notes.toMessage')}
          disabled={wants}
          data-busy={sending || undefined}
          onClick={ok}
        >
          <Icon name="arrow-up" />
          <span className="hx-tip" data-pos="below" data-align="end">
            {guest ? t('next.guest.send') : t('next.notes.toMessage')}
            <kbd>↵</kbd>
          </span>
        </button>
      </div>
    </div>
  );
};

export const NotesBar = ({
  notes,
  shown,
  onSend,
}: {
  notes: readonly INote[];
  shown: boolean;
  onSend: () => void;
}) => (
  <div className="hx-notesbar" data-shown={shown}>
    <span className="hx-notesbar__text">
      {notes.length > 0 ? plural(notes.length, 'next.notes.count') : t('next.notes.hint')}
    </span>
    <button type="button" className="hx-notesbar__send" data-on={notes.length > 0} onClick={onSend}>
      <Icon name="arrow-up" size="sm" />
      {t('next.notes.send')}
    </button>
  </div>
);

const EMPTY_NOTES: readonly INote[] = [];

/** A note's number: the notes that stay before it, and itself (one that is leaving keeps the place it had). */
const numberAt = (rows: readonly { leaving: boolean }[], index: number) =>
  rows.slice(0, index).filter((row) => !row.leaving).length + 1;

/** A pin on a block: the frame, the number at the corner and, on hover, the note's words. */
const NotePin = ({
  note,
  n,
  open,
  leaving,
  onOpen,
}: {
  note: INote;
  n: number;
  open: boolean;
  leaving: boolean;
  onOpen: () => void;
}) => {
  const ref = useRef<HTMLDivElement>(null);
  useNoteAnchor(ref, note.id, note.picked.rect);
  return (
    <div
      ref={ref}
      className="hx-npin"
      data-open={open}
      data-state={leaving ? PRESENCE.closing : PRESENCE.open}
    >
      <span className="hx-npin__frame" />
      <span className="hx-npin__mark">
        <button
          type="button"
          className="hx-npin__dot"
          aria-label={t('next.notes.pin', { n, label: pickedLabel(note.picked) })}
          onClick={onOpen}
        >
          {n}
        </button>
        {note.text && <span className="hx-npin__tip">{note.text}</span>}
      </span>
    </div>
  );
};

/** The frame of the block a new note is being written for. */
const NoteLasso = ({ rect }: { rect: IPicked['rect'] | null }) => {
  const ref = useRef<HTMLDivElement>(null);
  const { value, state } = usePresence(rect, LASSO_OUT_MS);
  useNoteAnchor(ref, DRAFT_ANCHOR, value);
  if (!value) return null;
  return <div ref={ref} className="hx-lasso" data-on={state === PRESENCE.open} aria-hidden="true" />;
};

/** The notes on the page itself, in the page's viewport pixels: they scale with it. */
export const NotePins = ({
  notes,
  draft,
  onOpen,
}: {
  notes: readonly INote[];
  draft: INoteDraft | null;
  onOpen: (id: string) => void;
}) => {
  const pins = useLeavingList(notes, (note) => note.id, PIN_OUT_MS);
  return (
    <>
      {pins.map(({ item, leaving }, index) => (
        <NotePin
          key={item.id}
          note={item}
          n={numberAt(pins, index)}
          open={draft?.id === item.id}
          leaving={leaving}
          onOpen={() => onOpen(item.id)}
        />
      ))}
      <NoteLasso rect={draft && draft.id === undefined ? draft.picked.rect : null} />
    </>
  );
};

/** What was changed on the block, one chip each: «Фон → #FFFFFF». */
const StyleChips = ({ edit }: { edit: IStyleEdit }) => {
  const keys = changedKeys(edit);
  if (keys.length === 0) return null;
  return (
    <div className="hx-anns__diffs">
      {keys.map((key) => (
        <span key={key} className="hx-anns__diff">
          {key === 'background' && <SwatchDot color={edit.background} />}
          {t(`next.notes.styleDiff.${key}`, { value: String(edit[key]) })}
        </span>
      ))}
    </div>
  );
};

/** One row of the list: the number on the block's mark, what it is and what was asked, the pencil and the trash. */
const AnnRow = ({
  note,
  n,
  index,
  leaving,
  onEdit,
  onRemove,
  readOnly,
}: {
  note: INote;
  n: number;
  index: number;
  leaving: boolean;
  onEdit: () => void;
  onRemove: () => void;
  readOnly: boolean;
}) => {
  const ref = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    ref.current?.style.setProperty('--i', String(index));
  }, [index]);
  return (
    <div ref={ref} className="hx-anns__item" data-leaving={leaving}>
      <span className="hx-anns__thumb" data-img="false">
        <Icon name="square-dashed-mouse-pointer" size="sm" />
        <span className="hx-anns__n">{n}</span>
      </span>
      <div className="hx-anns__body">
        <div className="hx-anns__head">
          <code className="hx-anns__tag">{note.picked.tag}</code>
          <span className="hx-anns__target">{pickedLabel(note.picked)}</span>
        </div>
        {note.text && <div className="hx-anns__text">{note.text}</div>}
        <StyleChips edit={note.style} />
      </div>
      {!readOnly && (
        <div className="hx-anns__acts">
          <button
            type="button"
            className="hx-anns__act hx-tipped"
            aria-label={t('next.notes.edit')}
            onClick={onEdit}
          >
            <Icon name="pencil" size="sm" />
            <span className="hx-tip" data-align="end">
              {t('next.notes.edit')}
            </span>
          </button>
          <button
            type="button"
            className="hx-anns__act hx-tipped"
            data-danger="true"
            aria-label={t('next.notes.remove')}
            onClick={onRemove}
          >
            <Icon name="trash-2" size="sm" />
            <span className="hx-tip" data-align="end">
              {t('next.notes.remove')}
            </span>
          </button>
        </div>
      )}
    </div>
  );
};

/** Over the island's field: «N annotations» with the cross that takes them all back; the button opens the list, where
 *  a note is changed or taken back. The strip leaves with the last note, the list with its own animation. */
export const NoteStrip = ({
  notes,
  onEdit,
  onRemove,
  onClear,
  readOnly = false,
}: {
  notes: readonly INote[];
  onEdit: (id: string) => void;
  onRemove: (id: string) => void;
  onClear: () => void;
  /** A guest's list of the comments: only read; on the phone it is a bottom sheet. */
  readOnly?: boolean;
}) => {
  const phone = usePhone();
  const sheeted = readOnly && phone;
  const [open, setOpen] = useState(false);
  const kept = usePresence(notes.length > 0 ? notes : null, LIST_OUT_MS);
  const list = usePresentFlag(open && notes.length > 0, LIST_OUT_MS);
  const rows = useLeavingList(kept.value ?? EMPTY_NOTES, (note) => note.id, LIST_OUT_MS);

  // A press outside the strip and Esc close the list (a sheet closes itself).
  useEffect(() => {
    if (!open || sheeted) return;
    const onDown = (event: PointerEvent) => {
      if (!(event.target as Element).closest('.hx-anns')) setOpen(false);
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
  }, [open, sheeted]);

  if (kept.value === null) return null;
  const listBox = (
    <div
      className="hx-anns__list"
      data-state={sheeted ? PRESENCE.open : list.state}
      role="dialog"
      aria-label={t('next.notes.annotations')}
    >
      {rows.map(({ item, leaving }, index) => (
        <AnnRow
          key={item.id}
          note={item}
          n={numberAt(rows, index)}
          index={index}
          leaving={leaving}
          readOnly={readOnly}
          onEdit={() => {
            setOpen(false);
            onEdit(item.id);
          }}
          onRemove={() => {
            if (notes.length <= 1) setOpen(false);
            onRemove(item.id);
          }}
        />
      ))}
    </div>
  );
  return (
    <div className="hx-island__chips">
      <div className="hx-anns" data-gone={kept.state === PRESENCE.closing}>
        {list.mounted && !sheeted && listBox}
        {sheeted && (
          <Sheet open={open} onClose={() => setOpen(false)} label={t('next.notes.annotations')}>
            {listBox}
          </Sheet>
        )}
        <span className="hx-anns__chip" data-open={open}>
          <button
            type="button"
            className="hx-anns__main"
            aria-expanded={open}
            onClick={() => setOpen((on) => !on)}
          >
            <Icon name="message-square-text" size="sm" />
            <span>{plural(kept.value.length, 'next.notes.annotationsCount')}</span>
            <Icon name="chevron-up" size="sm" className="hx-anns__chev" />
          </button>
          {!readOnly && (
            <button
              type="button"
              className="hx-anns__x"
              aria-label={t('next.notes.clear')}
              onClick={() => {
                setOpen(false);
                onClear();
              }}
            >
              <Icon name="x" size="sm" />
            </button>
          )}
        </span>
      </div>
    </div>
  );
};
