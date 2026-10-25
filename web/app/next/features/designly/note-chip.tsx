// A design's note that went with a message, as the person reads it: the pin's number, the page it is on and what they
// wrote; the block it is on is only a quiet hint. The `<annotation>` block itself stays what the agent gets.
// The chip under a bubble opens the page in the pane (open-note.ts); the queue row says the same on one line.
import type { ISentNote } from '@/next/features/designly/annotation';
import { openNotePage } from '@/next/features/designly/open-note';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/note-chip.css';

const hintOf = (note: ISentNote) => [note.comment, note.element].filter(Boolean).join('\n');

export const NoteChip = ({ note }: { note: ISentNote }) => {
  const { design, board } = note;
  const label = note.page
    ? t('next.noteChip.open', { page: note.page, n: note.n })
    : t('next.noteChip.noPage', { n: note.n });
  return (
    <button
      type="button"
      className="hx-notechip"
      title={hintOf(note)}
      aria-label={label}
      disabled={design === null || board === null}
      onClick={() => {
        if (design !== null && board !== null) openNotePage({ design, board });
      }}
    >
      <span className="hx-notechip__n">{note.n}</span>
      {note.page && <span className="hx-notechip__page">{note.page}</span>}
      {note.comment ? (
        <span className="hx-notechip__text">{note.comment}</span>
      ) : (
        <span className="hx-notechip__hint">{note.element}</span>
      )}
    </button>
  );
};

/** The notes of a queued message on its one line: «📍1 Главная — крупнее», with the glyph, not the emoji. */
export const NoteLine = ({ notes }: { notes: readonly ISentNote[] }) => (
  <>
    {notes.map((note) => (
      <span key={note.n} className="hx-noteline" title={hintOf(note)}>
        <Icon name="map-pin" size="sm" />
        <span className="hx-noteline__n">{note.n}</span>
        {[note.page, note.comment || note.element].filter(Boolean).join(' — ')}
      </span>
    ))}
  </>
);
