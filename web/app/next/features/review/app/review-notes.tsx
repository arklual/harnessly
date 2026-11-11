// The remarks of the «Ревью кода» app on a chat's work (board «Ревью кода — дифф»): dragging over the line numbers picks
// lines and opens a composer under them — «Комментарий» keeps a draft for the agent, «Спросить агента» asks at once; a
// kept remark is a grey card under its lines («Вы · черновик · агенту · строки 17–20»), changed or removed from its
// corner; one sent waits for «Сделано — закрыть» or «Не то — вернуть». The panel lists them and sends the drafts with
// the person's last words as one message.
import {
  useEffect,
  useLayoutEffect,
  useMemo,
  useState,
  useSyncExternalStore,
  type KeyboardEvent,
} from 'react';
import { createPortal } from 'react-dom';
import {
  draftsOf,
  linesOf,
  newNoteId,
  NOTE_STATE,
  placeNote,
  type IReviewNote,
  type TPlace,
} from '@/next/features/review/app/notes';
import type { ILayerMarks, NotesLayer } from '@/next/features/review/app/notes-layer';
import type { IReviewNotesApi } from '@/next/features/review/app/use-review-notes';
import { LINE_SIDE, type IFileContent, type IReviewFile, type TLineSide } from '@/next/features/review/model';
import { Button, RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { useLeavingList, usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export const EXIT_MS = 160;
export const NEW_KEY = '__new';

export interface IPick {
  side: TLineSide;
  from: number;
  to: number;
  /** Released: the composer is open under the lines. */
  open: boolean;
}

export const linesLabel = (from: number, to: number) =>
  from === to ? t('next.reviewApp.notes.line', { n: from }) : t('next.reviewApp.notes.lines', { from, to });

export const submitKeys = (
  event: KeyboardEvent<HTMLTextAreaElement>,
  submit: () => void,
  cancel: () => void,
) => {
  if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
    event.preventDefault();
    submit();
  } else if (event.key === 'Escape') {
    event.preventDefault();
    event.stopPropagation();
    cancel();
  }
};

export const docsOf = (content: IFileContent): Record<TLineSide, string[]> => ({
  after: content.after.split('\n'),
  before: content.before.split('\n'),
});

// ── a kept remark ──

interface INoteCardProps {
  note: IReviewNote;
  /** Where its lines are now. */
  place: NonNullable<TPlace>;
  api: IReviewNotesApi;
  leaving: boolean;
}

const NoteCard = ({ note, place, api, leaving }: INoteCardProps) => {
  const [editing, setEditing] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const run = async (job: () => Promise<boolean>, after?: () => void) => {
    setBusy(true);
    const ok = await job();
    setBusy(false);
    if (ok) after?.();
    else toast(t('next.reviewApp.notes.failed'));
  };
  const save = () => {
    const text = editing?.trim();
    if (!text) return;
    void run(
      () => api.save({ ...note, text }),
      () => setEditing(null),
    );
  };
  const sent = note.state === NOTE_STATE.sent;
  const stateLabel = sent ? t('next.reviewApp.notes.sent') : t('next.reviewApp.notes.draft');
  return (
    <div className="hx-rvnote" data-state={leaving ? 'closing' : 'open'} data-note={note.state}>
      <div className="hx-rvnote__head">
        <span className="hx-rvnote__meta">
          <span className="hx-rvnote__who">{t('next.reviewApp.notes.you')}</span> · {stateLabel} ·{' '}
          {t('next.reviewApp.notes.toAgent')} · {linesLabel(place.from, place.to)}
        </span>
        {editing === null && !sent && (
          <span className="hx-rvnote__tools">
            <RoundButton
              icon="pencil"
              label={t('next.reviewApp.notes.edit')}
              size="sm"
              variant="ghost"
              onClick={() => setEditing(note.text)}
            />
            <RoundButton
              icon="trash-2"
              label={t('next.reviewApp.notes.delete')}
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => void run(() => api.remove(note.id))}
            />
          </span>
        )}
      </div>
      {editing === null ? (
        <p className="hx-rvnote__text">{note.text}</p>
      ) : (
        <div className="hx-rvnote__edit">
          <textarea
            className="hx-rvnote__field"
            value={editing}
            autoFocus
            rows={3}
            aria-label={t('next.reviewApp.notes.edit')}
            onChange={(event) => setEditing(event.target.value)}
            onKeyDown={(event) => submitKeys(event, save, () => setEditing(null))}
          />
          <div className="hx-rvnote__actions">
            <span className="hx-rvnote__hint">{t('next.reviewApp.notes.keys')}</span>
            <Button size="sm" variant="ghost" onClick={() => setEditing(null)}>
              {t('next.reviewApp.notes.cancel')}
            </Button>
            <Button size="sm" variant="primary" busy={busy} disabled={!editing.trim()} onClick={save}>
              {t('next.reviewApp.notes.save')}
            </Button>
          </div>
        </div>
      )}
      {sent && (
        <div className="hx-rvnote__actions">
          <Button
            size="sm"
            icon="circle-check"
            busy={busy}
            onClick={() => void run(() => api.save({ ...note, state: NOTE_STATE.done }))}
          >
            {t('next.reviewApp.notes.done')}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            icon="undo-2"
            disabled={busy}
            onClick={() => void run(() => api.save({ ...note, state: NOTE_STATE.draft }))}
          >
            {t('next.reviewApp.notes.reopen')}
          </Button>
        </div>
      )}
    </div>
  );
};

// ── a new remark ──

interface IComposerProps {
  pick: IPick;
  closing: boolean;
  text: string;
  onText: (text: string) => void;
  onKeep: () => Promise<void>;
  /** «Спросить агента»: none where no agent stands by. */
  onAsk?: () => void;
  onCancel: () => void;
  /** The keep button's and the field's words: a remark for the agent by default. */
  keepLabel?: string;
  placeholder?: string;
}

export const Composer = ({
  pick,
  closing,
  text,
  onText,
  onKeep,
  onAsk,
  onCancel,
  keepLabel = t('next.reviewApp.notes.comment'),
  placeholder = t('next.reviewApp.notes.placeholder'),
}: IComposerProps) => {
  const [busy, setBusy] = useState(false);
  const keep = () => {
    if (!text.trim() || busy) return;
    setBusy(true);
    void onKeep().finally(() => setBusy(false));
  };
  return (
    <div className="hx-rvnote hx-rvnote--new" data-state={closing ? 'closing' : 'open'}>
      <div className="hx-rvnote__head">
        <span className="hx-rvnote__meta">
          <span className="hx-rvnote__who">{linesLabel(pick.from, pick.to)}</span>
          {pick.side === LINE_SIDE.before && ` · ${t('next.reviewApp.notes.oldVersion')}`}
        </span>
      </div>
      <textarea
        className="hx-rvnote__field"
        value={text}
        autoFocus
        rows={3}
        placeholder={placeholder}
        aria-label={placeholder}
        onChange={(event) => onText(event.target.value)}
        onKeyDown={(event) => submitKeys(event, keep, onCancel)}
      />
      <div className="hx-rvnote__actions">
        <span className="hx-rvnote__hint">{t('next.reviewApp.notes.keys')}</span>
        <Button size="sm" variant="ghost" onClick={onCancel}>
          {t('next.reviewApp.notes.cancel')}
        </Button>
        {onAsk && (
          <Button size="sm" icon="message-square" disabled={!text.trim()} onClick={onAsk}>
            {t('next.reviewApp.notes.ask')}
          </Button>
        )}
        <Button size="sm" variant="primary" busy={busy} disabled={!text.trim()} onClick={keep}>
          {keepLabel}
        </Button>
      </div>
    </div>
  );
};

// ── the layer over one file's diff ──

interface IDiffNotesProps {
  layer: NotesLayer;
  api: IReviewNotesApi;
  notes: readonly IReviewNote[];
  file: IReviewFile;
  content: IFileContent;
  /** The sides drawn: the unified view draws the new text only. */
  sides: readonly TLineSide[];
  worktree: string | undefined;
}

export const DiffNotes = ({ layer, api, notes, file, content, sides, worktree }: IDiffNotesProps) => {
  const slots = useSyncExternalStore(layer.subscribe, layer.getSlots, layer.getSlots);
  const [pick, setPick] = useState<IPick | null>(null);
  const [text, setText] = useState('');
  const composer = usePresence(pick?.open ? pick : null, EXIT_MS);
  const docs = useMemo(() => docsOf(content), [content]);

  useEffect(
    () => layer.listenPick((side, from, to, done) => setPick({ side, from, to, open: done })),
    [layer],
  );

  const mine = useMemo(
    () => notes.filter((n) => n.path === file.path && n.state !== NOTE_STATE.done && sides.includes(n.side)),
    [notes, file.path, sides],
  );
  const shown = useLeavingList(mine, (n) => n.id, EXIT_MS);
  const placed = useMemo(
    () =>
      shown
        .map(({ item, leaving }) => ({ note: item, leaving, place: placeNote(item, docs[item.side]) }))
        .filter(
          (p): p is { note: IReviewNote; leaving: boolean; place: NonNullable<TPlace> } => p.place !== null,
        ),
    [shown, docs],
  );

  useLayoutEffect(() => {
    for (const side of [LINE_SIDE.after, LINE_SIDE.before]) {
      const marks: ILayerMarks = {
        cards: [
          ...placed.filter((p) => p.note.side === side).map((p) => ({ key: p.note.id, line: p.place.to })),
          ...(composer.value?.side === side ? [{ key: NEW_KEY, line: composer.value.to }] : []),
        ],
        ranges: [
          ...placed
            .filter((p) => p.note.side === side)
            .map((p) => ({ from: p.place.from, to: p.place.to, kind: 'note' as const })),
          ...(pick?.side === side ? [{ from: pick.from, to: pick.to, kind: 'pick' as const }] : []),
        ],
      };
      layer.set(side, marks);
    }
  }, [layer, placed, composer.value, pick]);

  const close = () => {
    setPick(null);
    setText('');
  };
  const at = composer.value
    ? {
        path: file.path,
        side: composer.value.side,
        from: composer.value.from,
        to: composer.value.to,
        lines: linesOf(docs[composer.value.side], composer.value.from, composer.value.to),
      }
    : null;
  const keep = async () => {
    if (!at) return;
    const ok = await api.save({
      ...at,
      id: newNoteId(),
      ...(worktree ? { worktree } : {}),
      text: text.trim(),
      state: NOTE_STATE.draft,
      createdAt: 0,
      updatedAt: 0,
    });
    if (ok) close();
    else toast(t('next.reviewApp.notes.failed'));
  };
  const ask = () => {
    if (!at || !text.trim()) return;
    api.ask(at, text);
    toast(t('next.reviewApp.notes.asked'));
    close();
  };

  return (
    <>
      {slots.map((slot) => {
        if (slot.key === NEW_KEY)
          return composer.value && slot.side === composer.value.side
            ? createPortal(
                <Composer
                  pick={composer.value}
                  closing={composer.state === 'closing'}
                  text={text}
                  onText={setText}
                  onKeep={keep}
                  onAsk={ask}
                  onCancel={close}
                />,
                slot.el,
                `${slot.side}:${slot.key}`,
              )
            : null;
        const p = placed.find((x) => x.note.id === slot.key && x.note.side === slot.side);
        return p
          ? createPortal(
              <NoteCard note={p.note} place={p.place} api={api} leaving={p.leaving} />,
              slot.el,
              `${slot.side}:${slot.key}`,
            )
          : null;
      })}
    </>
  );
};

// ── the panel ──

interface INotesListProps {
  notes: readonly IReviewNote[];
  files: readonly IReviewFile[];
  /** What each note's file shows now, when it is open: its place, or null for lines no longer there. */
  placeOf: (note: IReviewNote) => TPlace | undefined;
  onOpen: (file: IReviewFile, line: number) => void;
}

export const NotesList = ({ notes, files, placeOf, onOpen }: INotesListProps) => {
  const open = notes.filter((n) => n.state !== NOTE_STATE.done);
  if (open.length === 0) return null;
  const sorted = [...open].sort((a, b) => a.path.localeCompare(b.path) || a.from - b.from);
  return (
    <section className="hx-rvnotes" aria-label={t('next.reviewApp.notes.title')}>
      <h3 className="hx-rvnotes__title">
        {t('next.reviewApp.notes.title')} <span className="hx-rvnotes__count">{open.length}</span>
      </h3>
      <ol className="hx-rvnotes__list">
        {sorted.map((note) => {
          const file = files.find((f) => f.path === note.path);
          const place = placeOf(note);
          const outdated = place === null;
          return (
            <li key={note.id}>
              <button
                type="button"
                className="hx-rvnotes__row"
                disabled={!file}
                onClick={() => file && onOpen(file, place?.from ?? note.from)}
              >
                <span className="hx-rvnotes__where">
                  <Icon name={note.state === NOTE_STATE.sent ? 'send' : 'message-square'} size="sm" />
                  <span className="hx-rvnotes__path">{note.path.split('/').at(-1)}</span>
                  <span className="hx-rvnotes__muted">
                    {linesLabel(place?.from ?? note.from, place?.to ?? note.to)}
                  </span>
                  {outdated && (
                    <span className="hx-rvnotes__muted">· {t('next.reviewApp.notes.outdated')}</span>
                  )}
                  {note.state === NOTE_STATE.sent && (
                    <span className="hx-rvnotes__muted">· {t('next.reviewApp.notes.sent')}</span>
                  )}
                </span>
                <span className="hx-rvnotes__text">{note.text}</span>
              </button>
            </li>
          );
        })}
      </ol>
    </section>
  );
};

export const NotesFooter = ({ api }: { api: IReviewNotesApi }) => {
  const [extra, setExtra] = useState('');
  const [busy, setBusy] = useState(false);
  const count = api.state.k === 'ready' ? draftsOf(api.state.notes).length : 0;
  const send = async () => {
    if (busy || (count === 0 && !extra.trim())) return;
    setBusy(true);
    const ok = await api.send(extra);
    setBusy(false);
    if (ok) {
      setExtra('');
      toast(t('next.reviewApp.notes.sentToast'));
    } else toast(t('next.reviewApp.notes.failed'));
  };
  if (api.state.k === 'failed')
    return (
      <div className="hx-rvnotes__foot">
        <span className="hx-rvnotes__muted">{t('next.reviewApp.notes.loadFailed')}</span>
        <Button size="sm" variant="ghost" onClick={api.retry}>
          {t('next.reviewApp.notes.retry')}
        </Button>
      </div>
    );
  return (
    <div className="hx-rvnotes__foot">
      <textarea
        className="hx-rvnote__field"
        rows={2}
        value={extra}
        placeholder={t('next.reviewApp.notes.more')}
        aria-label={t('next.reviewApp.notes.more')}
        onChange={(event) => setExtra(event.target.value)}
        onKeyDown={(event) =>
          submitKeys(
            event,
            () => void send(),
            () => setExtra(''),
          )
        }
      />
      <Button
        variant="primary"
        icon="send"
        busy={busy}
        disabled={api.state.k !== 'ready' || (count === 0 && !extra.trim())}
        onClick={() => void send()}
      >
        {count > 0 ? t('next.reviewApp.notes.sendN', { n: count }) : t('next.reviewApp.notes.send')}
      </Button>
    </div>
  );
};
