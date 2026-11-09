// The language of numbered annotations, shared by the pane's browser (features/pane/annotation-layer.tsx), the chat's
// feed (features/chat/selbar.tsx) and the notes on a design's page (features/designly/site-notes.tsx): a numbered dot on what is annotated, the compact row of its comment that opens
// beside the dot, and the floating bar «Аннотирование · N». The host knows where its marks stand: it writes the place
// into the mark's --x / --y and data-flip / data-up (a row that would run past the right or the bottom edge opens on the
// other side), and keeps the list; these parts only draw and answer.
import { useLayoutEffect, useRef, useState, type PointerEvent, type ReactNode, type Ref } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence, usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/notes-shared.css';

/** Words cut to a length, with an ellipsis. */
export const clipQuote = (text: string, max: number) =>
  text.length > max ? `${text.slice(0, max - 1).trim()}…` : text;

/** The comment row's exit and the bar's, as long as hx-note-pop-out / hx-note-bar-out in notes-shared.css. */
export const NOTE_OUT_MS = 160;
/** A row that would run past these edges opens on the other side of its dot. */
export const NOTE_ROW_W = 280;
export const NOTE_ROW_H = 96;

interface INoteRowProps {
  /** The words the row opens with; with `onValue` the host keeps them (a dictation writes into them). */
  comment: string;
  onValue?: (text: string) => void;
  open: boolean;
  placeholder: string;
  label: string;
  /** Enter or ✓; with the words trimmed. */
  onCommit: (comment: string) => void;
  /** 🗑; without it the row has no trash (a guest cannot take a comment back). */
  onRemove?: () => void;
  removeLabel?: string;
  onCancel: () => void;
  /** Before the field: the host's own button or the mark's number. */
  lead?: ReactNode;
  /** Between the field and 🗑: the host's tools (the microphone, «cancel»). */
  tools?: ReactNode;
  /** Under the line, the whole width: what the host unfolds (a block's settings, a picture to draw). */
  more?: ReactNode;
  okLabel?: string;
  /** ✓ waits: the words are wanted, or they are on their way. */
  okDisabled?: boolean;
  busy?: boolean;
  className?: string;
  rowRef?: Ref<HTMLFormElement>;
  /** A press on the row's own ground (not on a field or a button): the host may let the row be dragged by it. */
  onGround?: (event: PointerEvent<HTMLFormElement>) => void;
  onGroundDouble?: () => void;
}

/** The field grows with its words up to a few lines (the CSS caps it); Enter keeps it, Shift+Enter breaks the line. */
const grow = (field: HTMLTextAreaElement | null) => {
  if (!field) return;
  field.style.height = '0px';
  field.style.height = `${field.scrollHeight}px`;
};

/** The comment of one mark: a field and ✓; Enter keeps it, Esc leaves it; 🗑 takes the mark away. */
export const NoteRow = ({
  comment,
  onValue,
  open,
  placeholder,
  label,
  onCommit,
  onRemove,
  removeLabel,
  onCancel,
  lead,
  tools,
  more,
  okLabel,
  okDisabled = false,
  busy = false,
  className,
  rowRef,
  onGround,
  onGroundDouble,
}: INoteRowProps) => {
  const [own, setOwn] = useState(comment);
  const text = onValue ? comment : own;
  const field = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => grow(field.current), [text]);
  const commit = () => {
    if (!okDisabled && !busy) onCommit(text.trim());
  };
  return (
    <form
      ref={rowRef}
      className={className ? `hx-note__row ${className}` : 'hx-note__row'}
      data-state={open ? 'open' : 'closing'}
      data-more={more ? true : undefined}
      aria-label={label}
      onSubmit={(event) => {
        event.preventDefault();
        commit();
      }}
      onKeyDown={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault();
          event.stopPropagation();
          onCancel();
        }
      }}
      onPointerDown={(event) => {
        if (event.target === event.currentTarget) onGround?.(event);
      }}
      onDoubleClick={(event) => {
        if (event.target === event.currentTarget) onGroundDouble?.();
      }}
    >
      {lead}
      <textarea
        ref={field}
        className="hx-note__input"
        rows={1}
        value={text}
        autoFocus
        disabled={busy}
        placeholder={placeholder}
        aria-label={placeholder}
        onChange={(event) => (onValue ?? setOwn)(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault();
            commit();
          }
        }}
      />
      {tools}
      {onRemove && (
        <button
          type="button"
          className="hx-note__btn"
          aria-label={removeLabel ?? t('next.pane.annot.remove')}
          onClick={onRemove}
        >
          <Icon name="trash-2" size="sm" />
        </button>
      )}
      <button
        type="submit"
        className="hx-note__btn"
        data-primary
        aria-label={okLabel ?? t('next.pane.annot.save')}
        aria-disabled={okDisabled || busy || undefined}
        aria-busy={busy || undefined}
      >
        <Icon name="check" size="sm" />
      </button>
      {more && <div className="hx-note__more">{more}</div>}
    </form>
  );
};

interface INoteDotProps {
  n: number;
  /** Its name for a screen reader: «Аннотация 2: …». */
  label: string;
  /** No words yet: the dot is hollow, it asks for them. */
  empty?: boolean;
  /** Its row is open. */
  active?: boolean;
  /** Out of reach (faded, another page): it takes no focus. */
  inert?: boolean;
  onOpen?: () => void;
  className?: string;
}

/** The numbered dot of an annotation; without `onOpen` it only shows the number (in a row, in a list). */
export const NoteDot = ({
  n,
  label,
  empty = false,
  active = false,
  inert = false,
  onOpen,
  className,
}: INoteDotProps) => {
  const name = className ? `hx-note__dot ${className}` : 'hx-note__dot';
  const num = (
    <span key={n} className="hx-note__num">
      {n}
    </span>
  );
  if (!onOpen)
    return (
      <span className={name} data-static data-empty={empty || undefined} aria-label={label} role="img">
        {num}
      </span>
    );
  return (
    <button
      type="button"
      className={name}
      data-empty={empty || undefined}
      data-active={active || undefined}
      aria-label={label}
      tabIndex={inert ? -1 : undefined}
      // A press on the dot must not take a selection of the page's words away.
      onPointerDown={(event) => event.preventDefault()}
      onClick={onOpen}
    >
      {num}
    </button>
  );
};

export interface INoteMarkProps {
  markRef: Ref<HTMLDivElement>;
  n: number;
  /** The dot's name for a screen reader: «Аннотация 2: …». */
  label: string;
  comment: string;
  editing: boolean;
  leaving: boolean;
  /** Not where it can be seen (another page, the words scrolled away): it fades and takes no clicks. */
  away?: boolean;
  sending?: boolean;
  rowLabel: string;
  placeholder: string;
  onOpen: () => void;
  onCommit: (comment: string) => void;
  onRemove: () => void;
  onCancel: () => void;
}

/** One mark: the numbered dot (hollow while it has no word) and its row while it is edited. */
export const NoteMark = ({
  markRef,
  n,
  label,
  comment,
  editing,
  leaving,
  away = false,
  sending = false,
  rowLabel,
  placeholder,
  onOpen,
  onCommit,
  onRemove,
  onCancel,
}: INoteMarkProps) => {
  const row = usePresentFlag(editing, NOTE_OUT_MS);
  return (
    <div
      ref={markRef}
      className="hx-note__mark"
      data-editing={editing || undefined}
      data-leaving={leaving || undefined}
      data-away={away || undefined}
      data-sending={sending || undefined}
      data-empty={comment === '' || undefined}
    >
      <NoteDot
        n={n}
        label={label}
        empty={comment === ''}
        active={editing}
        inert={leaving || away}
        onOpen={onOpen}
      />
      {row.mounted && (
        <NoteRow
          comment={comment}
          open={row.state === PRESENCE.open}
          label={rowLabel}
          placeholder={placeholder}
          onCommit={onCommit}
          onRemove={onRemove}
          onCancel={onCancel}
        />
      )}
    </div>
  );
};

interface INoteBarButtonProps {
  icon: string;
  label: string;
  disabled?: boolean;
  pressed?: boolean;
  onClick: () => void;
}

export const NoteBarButton = ({ icon, label, disabled, pressed, onClick }: INoteBarButtonProps) => (
  <button
    type="button"
    className="hx-note__tool hx-tipped"
    aria-label={label}
    aria-pressed={pressed}
    aria-disabled={disabled || undefined}
    onClick={() => !disabled && onClick()}
  >
    <Icon name={icon} size="sm" />
    <span className="hx-tip">{label}</span>
  </button>
);

interface INoteBarProps {
  /** The bar stands; false lets it leave with its exit. */
  visible: boolean;
  count: number;
  /** «Отправить» was pressed: the button waits until the marks are gone. */
  sending?: boolean;
  sendLabel: string;
  onSend: () => void;
  /** The host's tools before «Отправить» (hide, delete all) and after it (close). */
  tools?: ReactNode;
  after?: ReactNode;
  className?: string;
}

/** «Аннотирование · N» with the host's tools and «Отправить». */
export const NoteBar = ({
  visible,
  count,
  sending = false,
  sendLabel,
  onSend,
  tools,
  after,
  className,
}: INoteBarProps) => {
  // The count the bar had while it leaves, so «0» does not flash in its exit.
  const bar = usePresence(visible ? count : null, NOTE_OUT_MS);
  if (bar.value === null) return null;
  const empty = count === 0;
  return (
    <div
      className={className ? `hx-note__bar ${className}` : 'hx-note__bar'}
      role="toolbar"
      aria-label={t('next.pane.annot.title')}
      data-state={bar.state === PRESENCE.closing ? 'closing' : 'open'}
    >
      <span className="hx-note__title">{t('next.pane.annot.bar', { n: bar.value })}</span>
      {tools}
      <button
        type="button"
        className="hx-note__send"
        aria-disabled={empty || sending || undefined}
        aria-busy={sending || undefined}
        onClick={() => !empty && !sending && onSend()}
      >
        {sendLabel}
      </button>
      {after}
    </div>
  );
};
