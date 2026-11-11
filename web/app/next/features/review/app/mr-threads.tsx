// The review of a merge request on the «Ревью кода» screen: GitLab's discussions under their lines (author, replies,
// «изменено»), the person's own notes edited or deleted from their corner, «Решено»; a reply goes into the draft
// review or at once; lines picked by the numbers get a draft note of the review. The panel lists the discussions and
// sends the review, approves and takes the draft mark off. Every write goes through the person's own GitLab
// connection; without one the actions give way to «Подключить».
import { useEffect, useLayoutEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import {
  deletableIn,
  draftableIn,
  draftAt,
  draftsAt,
  MR_ACT,
  repliesOf,
  threadsOf,
  type IMrAct,
  type IMrDraft,
  type IMrNote,
  type IMrReview,
  type IMrThread,
  type TForge,
  type TMrRefusal,
} from '@/next/features/review/app/mr-review';
import type { ILayerMarks, NotesLayer } from '@/next/features/review/app/notes-layer';
import {
  Composer,
  docsOf,
  EXIT_MS,
  NEW_KEY,
  submitKeys,
  type IPick,
} from '@/next/features/review/app/review-notes';
import type { IMrReviewApi } from '@/next/features/review/app/use-mr-review';
import { AnalysisBox } from '@/next/features/review/app/analysis-box';
import { linesOf } from '@/next/features/review/app/notes';
import { LINE_SIDE, type IFileContent, type IReviewFile, type TLineSide } from '@/next/features/review/model';
import { Button, RoundButton } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { useLeavingList, usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

export const refusalText = (refusal: TMrRefusal, forge: TForge): string => {
  switch (refusal.k) {
    case 'no-connection':
      return t('next.reviewApp.mr.noConnection', { forge });
    case 'no-personal':
      return t('next.reviewApp.mr.noPersonal', { forge });
    case 'denied':
      return refusal.scope
        ? t('next.reviewApp.mr.noScope', { scope: refusal.scope })
        : t('next.reviewApp.mr.denied', { forge });
    default:
      return refusal.text;
  }
};

export interface IMrContext {
  mr: IMrReviewApi;
  review: IMrReview;
  /** Writes go through the person's own connection; false when only the deployment's reads. */
  writable: boolean;
  /** Where the review lives: GitLab or GitHub. */
  forge: TForge;
  onConnect: () => void;
  /** Asks the agent about lines; none where no agent can be asked. */
  ask?: (text: string) => void;
  /** The agent beside weighs a comment or a discussion in its side chat (seen by the person only) and resolves with
   *  its whole answer; none without a chat beside. */
  analyze?: (question: string, signal: AbortSignal) => Promise<string>;
  /** A comment put into the agent's field as a quote, or taken out of it again; none without a chat beside. */
  quoted?: IQuoted;
  /** The change's link, for what the agent is asked. */
  where?: string;
}

export interface IQuoted {
  has: (id: string) => boolean;
  toggle: (id: string, text: string, note: string) => void;
}

/** Runs an act with its busy state; a refusal is said in a toast. */
const useAct = (ctx: IMrContext) => {
  const [busy, setBusy] = useState(false);
  const run = async (act: IMrAct): Promise<boolean> => {
    if (busy) return false;
    setBusy(true);
    const refusal = await ctx.mr.act(act);
    setBusy(false);
    if (refusal) toast(refusalText(refusal, ctx.forge), { tone: 'error' });
    return refusal === null;
  };
  return { busy, run };
};

const ConnectNote = ({ ctx }: { ctx: IMrContext }) => (
  <div className="hx-rvnote__actions">
    <span className="hx-rvnote__hint">{t('next.reviewApp.mr.connectToWrite', { forge: ctx.forge })}</span>
    <Button size="sm" icon="plug" onClick={ctx.onConnect}>
      {t('next.reviewApp.connect')}
    </Button>
  </div>
);

// ── one note of a thread ──

interface INoteRowProps {
  ctx: IMrContext;
  who: string;
  label?: string;
  body: string;
  /** Saves a new text; none for a note the person may not change. */
  onSave?: (text: string) => Promise<boolean>;
  onDelete?: () => Promise<boolean>;
  /** More tools on hover (a colleague's comment: «Разобрать», «Отправить агенту»). */
  tools?: ReactNode;
  /** Under the note (its analysis). */
  below?: ReactNode;
}

const NoteRow = ({ who, label, body, onSave, onDelete, tools, below }: INoteRowProps) => {
  const [editing, setEditing] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const save = async () => {
    const text = editing?.trim();
    if (!text || !onSave) return;
    setBusy(true);
    const ok = await onSave(text);
    setBusy(false);
    if (ok) setEditing(null);
  };
  return (
    <div className="hx-rvnote__entry">
      <div className="hx-rvnote__head">
        <span className="hx-rvnote__meta">
          <span className="hx-rvnote__who">{who}</span>
          {label && ` · ${label}`}
        </span>
        {editing === null && (onSave || onDelete || tools) && (
          <span className="hx-rvnote__tools">
            {tools}
            {onSave && (
              <RoundButton
                icon="pencil"
                label={t('next.reviewApp.notes.edit')}
                size="sm"
                variant="ghost"
                onClick={() => setEditing(body)}
              />
            )}
            {onDelete && (
              <RoundButton
                icon="trash-2"
                label={t('next.reviewApp.notes.delete')}
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={() => {
                  setBusy(true);
                  void onDelete().finally(() => setBusy(false));
                }}
              />
            )}
          </span>
        )}
      </div>
      {editing === null ? (
        <p className="hx-rvnote__text">{body}</p>
      ) : (
        <div className="hx-rvnote__edit">
          <textarea
            className="hx-rvnote__field"
            value={editing}
            autoFocus
            rows={3}
            aria-label={t('next.reviewApp.notes.edit')}
            onChange={(event) => setEditing(event.target.value)}
            onKeyDown={(event) =>
              submitKeys(
                event,
                () => void save(),
                () => setEditing(null),
              )
            }
          />
          <div className="hx-rvnote__actions">
            <span className="hx-rvnote__hint">{t('next.reviewApp.notes.keys')}</span>
            <Button size="sm" variant="ghost" onClick={() => setEditing(null)}>
              {t('next.reviewApp.notes.cancel')}
            </Button>
            <Button
              size="sm"
              variant="primary"
              busy={busy}
              disabled={!editing.trim()}
              onClick={() => void save()}
            >
              {t('next.reviewApp.notes.save')}
            </Button>
          </div>
        </div>
      )}
      {below}
    </div>
  );
};

const whoOf = (note: IMrNote) =>
  note.mine ? t('next.reviewApp.notes.you') : (note.author?.name ?? t('next.reviewApp.someone'));

const placeLabel = (path: string | undefined, line: number | undefined) =>
  path ? `${path.split('/').at(-1) ?? path}${line ? `:${line}` : ''}` : t('next.reviewApp.mr.general');

// ── a discussion ──

const WHOLE = 'whole' as const;

export const ThreadCard = ({
  thread,
  ctx,
  leaving = false,
  withPlace = false,
}: {
  thread: IMrThread;
  ctx: IMrContext;
  leaving?: boolean;
  /** Says where it is (in the panel, away from its lines). */
  withPlace?: boolean;
}) => {
  const { busy, run } = useAct(ctx);
  const [reply, setReply] = useState<string | null>(null);
  const replies = repliesOf(ctx.review, thread.id);
  // One analysis open at a time: of a comment (its index) or of the whole discussion.
  const [analysis, setAnalysis] = useState<number | typeof WHOLE | null>(null);
  const box = usePresence(analysis, EXIT_MS);
  const talk = thread.notes.map((n) => ({ who: whoOf(n), body: n.body }));
  const where = `${ctx.where ?? ''}${thread.path ? ` · ${thread.path}${thread.line ? `:${thread.line}` : ''}` : ''}`;
  const analyze = ctx.analyze;
  const boxOf = (key: number | typeof WHOLE) =>
    analyze && box.value === key ? (
      <AnalysisBox
        key={String(key)}
        notes={talk}
        index={key === WHOLE ? null : key}
        where={where}
        forge={ctx.forge}
        analyze={analyze}
        onApply={ctx.writable ? (body) => run({ op: MR_ACT.reply, thread: thread.id, body }) : undefined}
        onConnect={ctx.onConnect}
        onClose={() => setAnalysis(null)}
        closing={box.state === 'closing'}
      />
    ) : null;
  const toolsOf = (note: IMrNote, i: number) => {
    if (note.mine) return undefined;
    const id = `mr:${thread.id}:${note.id}`;
    const quoted = ctx.quoted;
    const inField = quoted?.has(id) ?? false;
    return (
      <>
        {analyze && (
          <RoundButton
            icon="sparkles"
            label={t('next.reviewApp.ai.one')}
            size="sm"
            variant="ghost"
            aria-pressed={analysis === i}
            onClick={() => setAnalysis(analysis === i ? null : i)}
          />
        )}
        {quoted && (
          <RoundButton
            icon="send"
            label={inField ? t('next.reviewApp.ai.unquote') : t('next.reviewApp.ai.quote')}
            size="sm"
            variant="ghost"
            aria-pressed={inField}
            onClick={() =>
              quoted.toggle(
                id,
                note.body,
                t('next.reviewApp.ai.quoteNote', {
                  who: whoOf(note),
                  where: placeLabel(thread.path, thread.line),
                }),
              )
            }
          />
        )}
      </>
    );
  };
  const draftable = draftableIn(ctx.forge, thread);
  const answer = async (op: typeof MR_ACT.reply | typeof MR_ACT.draft) => {
    const body = reply?.trim();
    if (!body) return;
    if (await run({ op, thread: thread.id, body })) setReply(null);
  };
  return (
    <div
      className="hx-rvnote hx-rvnote--thread"
      data-state={leaving ? 'closing' : 'open'}
      data-resolved={thread.resolved || undefined}
    >
      {(withPlace || thread.resolved || thread.outdated) && (
        <div className="hx-rvnote__head">
          <span className="hx-rvnote__meta">
            {withPlace && <span className="hx-rvnote__who">{placeLabel(thread.path, thread.line)}</span>}
            {thread.resolved && ` · ${t('next.reviewApp.resolved')}`}
            {thread.outdated && ` · ${t('next.reviewApp.outdated')}`}
          </span>
        </div>
      )}
      {thread.notes.map((note, i) => (
        <NoteRow
          key={note.id}
          ctx={ctx}
          who={whoOf(note)}
          label={note.edited ? t('next.reviewApp.mr.edited') : undefined}
          body={note.body}
          tools={toolsOf(note, i)}
          below={boxOf(i)}
          onSave={
            note.mine && ctx.writable
              ? (body) => run({ op: MR_ACT.edit, thread: thread.id, note: note.id, body })
              : undefined
          }
          onDelete={
            note.mine && ctx.writable && deletableIn(ctx.forge, thread)
              ? () => run({ op: MR_ACT.delete, thread: thread.id, note: note.id })
              : undefined
          }
        />
      ))}
      {replies.map((draft) => (
        <NoteRow
          key={`d${draft.id}`}
          ctx={ctx}
          who={t('next.reviewApp.notes.you')}
          label={t('next.reviewApp.mr.draft')}
          body={draft.body}
          onSave={(body) => run({ op: MR_ACT.draftEdit, draft: draft.id, body })}
          onDelete={() => run({ op: MR_ACT.draftDelete, draft: draft.id })}
        />
      ))}
      {boxOf(WHOLE)}
      {!ctx.writable && <ConnectNote ctx={ctx} />}
      {!ctx.writable && analyze && (
        <div className="hx-rvnote__actions">
          <Button
            size="sm"
            variant="ghost"
            icon="sparkles"
            aria-pressed={analysis === WHOLE}
            onClick={() => setAnalysis(analysis === WHOLE ? null : WHOLE)}
          >
            {t('next.reviewApp.ai.all')}
          </Button>
        </div>
      )}
      {ctx.writable && reply === null && (
        <div className="hx-rvnote__actions">
          {analyze && (
            <Button
              size="sm"
              variant="ghost"
              icon="sparkles"
              title={t('next.reviewApp.ai.allHint')}
              aria-pressed={analysis === WHOLE}
              onClick={() => setAnalysis(analysis === WHOLE ? null : WHOLE)}
            >
              {t('next.reviewApp.ai.all')}
            </Button>
          )}
          {thread.resolvable && (
            <Button
              size="sm"
              variant="ghost"
              icon={thread.resolved ? 'undo-2' : 'circle-check'}
              disabled={busy}
              onClick={() => void run({ op: MR_ACT.resolve, thread: thread.id, resolved: !thread.resolved })}
            >
              {thread.resolved ? t('next.reviewApp.mr.unresolve') : t('next.reviewApp.mr.resolve')}
            </Button>
          )}
          <Button size="sm" icon="corner-down-left" onClick={() => setReply('')}>
            {t('next.reviewApp.mr.reply')}
          </Button>
        </div>
      )}
      {ctx.writable && reply !== null && (
        <div className="hx-rvnote__edit">
          <textarea
            className="hx-rvnote__field"
            value={reply}
            autoFocus
            rows={3}
            placeholder={t('next.reviewApp.mr.replyPlaceholder')}
            aria-label={t('next.reviewApp.mr.reply')}
            onChange={(event) => setReply(event.target.value)}
            onKeyDown={(event) =>
              submitKeys(
                event,
                () => void answer(draftable ? MR_ACT.draft : MR_ACT.reply),
                () => setReply(null),
              )
            }
          />
          <div className="hx-rvnote__actions">
            <Button size="sm" variant="ghost" onClick={() => setReply(null)}>
              {t('next.reviewApp.notes.cancel')}
            </Button>
            <Button
              size="sm"
              variant={draftable ? 'default' : 'primary'}
              busy={busy && !draftable}
              disabled={!reply.trim() || (busy && draftable)}
              onClick={() => void answer(MR_ACT.reply)}
            >
              {t('next.reviewApp.mr.replyNow')}
            </Button>
            {draftable && (
              <Button
                size="sm"
                variant="primary"
                busy={busy}
                disabled={!reply.trim()}
                onClick={() => void answer(MR_ACT.draft)}
              >
                {t('next.reviewApp.mr.toDraft')}
              </Button>
            )}
          </div>
        </div>
      )}
    </div>
  );
};

// ── a draft note of the review, starting a thread at a line ──

const DraftCard = ({ draft, ctx, leaving }: { draft: IMrDraft; ctx: IMrContext; leaving: boolean }) => {
  const { run } = useAct(ctx);
  return (
    <div className="hx-rvnote" data-state={leaving ? 'closing' : 'open'}>
      <NoteRow
        ctx={ctx}
        who={t('next.reviewApp.notes.you')}
        label={`${t('next.reviewApp.mr.draft')} · ${t('next.reviewApp.notes.line', { n: draft.line ?? 0 })}`}
        body={draft.body}
        onSave={(body) => run({ op: MR_ACT.draftEdit, draft: draft.id, body })}
        onDelete={() => run({ op: MR_ACT.draftDelete, draft: draft.id })}
      />
    </div>
  );
};

// ── the layer over one file's diff ──

type TItem = { key: string; side: TLineSide; line: number } & (
  { k: 'thread'; thread: IMrThread } | { k: 'draft'; draft: IMrDraft }
);

interface IMrDiffThreadsProps {
  layer: NotesLayer;
  ctx: IMrContext;
  file: IReviewFile;
  content: IFileContent;
  sides: readonly TLineSide[];
}

export const MrDiffThreads = ({ layer, ctx, file, content, sides }: IMrDiffThreadsProps) => {
  const slots = useSyncExternalStore(layer.subscribe, layer.getSlots, layer.getSlots);
  const [pick, setPick] = useState<IPick | null>(null);
  const [text, setText] = useState('');
  const composer = usePresence(pick?.open ? pick : null, EXIT_MS);
  const docs = useMemo(() => docsOf(content), [content]);
  const { run } = useAct(ctx);

  useEffect(
    () => layer.listenPick((side, from, to, done) => setPick({ side, from, to, open: done })),
    [layer],
  );

  const items = useMemo<TItem[]>(
    () => [
      ...threadsOf(ctx.review, file.path)
        .filter((thread) => thread.side && sides.includes(thread.side))
        .map((thread) => ({
          key: `t:${thread.id}`,
          side: thread.side ?? LINE_SIDE.after,
          line: thread.line ?? 1,
          k: 'thread' as const,
          thread,
        })),
      ...draftsAt(ctx.review, file.path)
        .filter((draft) => draft.side && sides.includes(draft.side))
        .map((draft) => ({
          key: `d:${draft.id}`,
          side: draft.side ?? LINE_SIDE.after,
          line: draft.line ?? 1,
          k: 'draft' as const,
          draft,
        })),
    ],
    [ctx.review, file.path, sides],
  );
  const shown = useLeavingList(items, (item) => item.key, EXIT_MS);

  useLayoutEffect(() => {
    for (const side of [LINE_SIDE.after, LINE_SIDE.before]) {
      const mine = shown.filter(({ item }) => item.side === side);
      const marks: ILayerMarks = {
        cards: [
          ...mine.map(({ item }) => ({ key: item.key, line: item.line })),
          ...(composer.value?.side === side ? [{ key: NEW_KEY, line: composer.value.to }] : []),
        ],
        ranges: [
          ...mine.map(({ item }) => ({ from: item.line, to: item.line, kind: 'note' as const })),
          ...(pick?.side === side ? [{ from: pick.from, to: pick.to, kind: 'pick' as const }] : []),
        ],
      };
      layer.set(side, marks);
    }
  }, [layer, shown, composer.value, pick]);

  const close = () => {
    setPick(null);
    setText('');
  };
  const keep = async () => {
    const at = composer.value;
    if (!at) return;
    // GitLab keeps a note on one line: the last of the picked ones, with the rest quoted when there are several.
    const quoted =
      at.from === at.to
        ? text
        : `${t('next.reviewApp.notes.lines', { from: at.from, to: at.to })}\n\n${text}`;
    if (await run(draftAt(file, at.side, at.to, ctx.review.refs, quoted))) close();
  };
  const ask = ctx.ask
    ? () => {
        const at = composer.value;
        if (!at || !text.trim() || !ctx.ask) return;
        const fence = '```';
        ctx.ask(
          `${file.path}:${at.from === at.to ? at.from : `${at.from}-${at.to}`}\n${fence}\n${linesOf(docs[at.side], at.from, at.to)}\n${fence}\n\n${text.trim()}`,
        );
        toast(t('next.reviewApp.notes.asked'));
        close();
      }
    : undefined;

  return (
    <>
      {slots.map((slot) => {
        const key = `${slot.side}:${slot.key}`;
        if (slot.key === NEW_KEY) {
          if (!composer.value || slot.side !== composer.value.side) return null;
          return createPortal(
            ctx.writable ? (
              <Composer
                pick={composer.value}
                closing={composer.state === 'closing'}
                text={text}
                onText={setText}
                onKeep={keep}
                onAsk={ask}
                onCancel={close}
                keepLabel={t('next.reviewApp.mr.toDraft')}
                placeholder={t('next.reviewApp.mr.notePlaceholder')}
              />
            ) : (
              <div className="hx-rvnote hx-rvnote--new" data-state={composer.state}>
                <ConnectNote ctx={ctx} />
              </div>
            ),
            slot.el,
            key,
          );
        }
        const entry = shown.find(({ item }) => item.key === slot.key && item.side === slot.side);
        if (!entry) return null;
        const { item, leaving } = entry;
        return createPortal(
          item.k === 'thread' ? (
            <ThreadCard thread={item.thread} ctx={ctx} leaving={leaving} />
          ) : (
            <DraftCard draft={item.draft} ctx={ctx} leaving={leaving} />
          ),
          slot.el,
          key,
        );
      })}
    </>
  );
};

// ── the panel ──

interface IMrThreadsPanelProps {
  ctx: IMrContext;
  files: readonly IReviewFile[];
  /** Whether the diff shows the thread at its line now (else its card is in the panel). */
  inDiff: (thread: IMrThread) => boolean;
  onOpen: (file: IReviewFile, line: number) => void;
}

export const MrThreadsPanel = ({ ctx, files, inDiff, onOpen }: IMrThreadsPanelProps) => {
  const threads = [...ctx.review.threads].sort((a, b) => Number(a.resolved) - Number(b.resolved));
  if (threads.length === 0) return <p className="hx-rvapp__empty">{t('next.reviewApp.mr.noThreads')}</p>;
  return (
    <ol className="hx-rvnotes__list hx-rvmr__threads">
      {threads.map((thread) => {
        const file = thread.path ? files.find((f) => f.path === thread.path) : undefined;
        const first = thread.notes[0];
        if (file && thread.line && inDiff(thread))
          return (
            <li key={thread.id}>
              <button
                type="button"
                className="hx-rvnotes__row"
                onClick={() => onOpen(file, thread.line ?? 1)}
              >
                <span className="hx-rvnotes__where">
                  <Icon name={thread.resolved ? 'circle-check' : 'message-square'} size="sm" />
                  <span className="hx-rvnotes__path">{placeLabel(thread.path, thread.line)}</span>
                  <span className="hx-rvnotes__muted">
                    · {first ? whoOf(first) : ''}
                    {thread.notes.length > 1 &&
                      ` · ${t('next.reviewApp.replies', { n: thread.notes.length - 1 })}`}
                    {thread.resolved && ` · ${t('next.reviewApp.resolved')}`}
                  </span>
                </span>
                <span className="hx-rvnotes__text">{first?.body}</span>
              </button>
            </li>
          );
        return (
          <li key={thread.id}>
            <ThreadCard thread={thread} ctx={ctx} withPlace />
          </li>
        );
      })}
    </ol>
  );
};

/** The foot of the panel: send the review, approve, take the draft mark off; «Подключить» without a connection. */
export const MrFooter = ({ ctx, showPublish = true }: { ctx: IMrContext; showPublish?: boolean }) => {
  const { busy, run } = useAct(ctx);
  const [doing, setDoing] = useState<string | null>(null);
  const go = async (op: IMrAct['op'], done: string) => {
    setDoing(op);
    if (await run({ op })) toast(done);
    setDoing(null);
  };
  if (!ctx.writable)
    return (
      <div className="hx-rvmr__foot">
        <p className="hx-rvnotes__muted">{t('next.reviewApp.mr.connectText', { forge: ctx.forge })}</p>
        <Button icon="plug" onClick={ctx.onConnect}>
          {t('next.reviewApp.mr.connectOwn', { forge: ctx.forge })}
        </Button>
      </div>
    );
  const { review } = ctx;
  const drafts = review.drafts.length;
  return (
    <div className="hx-rvmr__foot">
      {review.mine && review.draft && (
        <Button
          icon="git-pull-request"
          busy={busy && doing === MR_ACT.ready}
          disabled={busy}
          onClick={() => void go(MR_ACT.ready, t('next.reviewApp.mr.readyDone'))}
        >
          {t('next.reviewApp.mr.ready')}
        </Button>
      )}
      {(review.canApprove || review.approved) && (
        <Button
          icon={review.approved ? 'undo-2' : 'check'}
          busy={busy && (doing === MR_ACT.approve || doing === MR_ACT.unapprove)}
          disabled={busy}
          onClick={() =>
            void (review.approved
              ? go(MR_ACT.unapprove, t('next.reviewApp.mr.unapproveDone'))
              : go(MR_ACT.approve, t('next.reviewApp.mr.approveDone')))
          }
        >
          {review.approved ? t('next.reviewApp.mr.unapprove') : t('next.reviewApp.mr.approve')}
        </Button>
      )}
      {showPublish && (
        <Button
          variant="primary"
          icon="send"
          busy={busy && doing === MR_ACT.publish}
          disabled={busy || drafts === 0}
          onClick={() => void go(MR_ACT.publish, t('next.reviewApp.mr.publishDone', { forge: ctx.forge }))}
        >
          {drafts > 0 ? t('next.reviewApp.mr.publishN', { n: drafts }) : t('next.reviewApp.mr.publish')}
        </Button>
      )}
    </div>
  );
};
