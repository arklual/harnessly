// Messages written while the agent works, over the field (as in Codex): each goes when the agent finishes its turn
// (⌃↵ in the field types one into the turn instead), the pencil takes it back into the field, the bin takes it out.
// A queue paused by a stop waits for «Continue sending»; one held by the limit says so; one that could not go says why
// and offers Retry. Every press waits for the runtime's answer.
// The rows can be put in another order: the grip on the left carries a row (the mouse, or a finger after it rests; ↑ ↓
// with the grip focused do it a place at a time), a line shows where it would land, and the order changes when the
// runtime has said so — the other rows then slide to their new places.
import AttachmentThumb from '@/next/ui/attachment-thumb';
import { memo, useCallback, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { readAnnotated } from '@/next/features/designly/annotation';
import { attachedOf, REF_ICON, refKey } from '@/next/features/chat/attached';
import type { MessageRef } from '@/next/protocol/v2';
import { NoteLine } from '@/next/features/designly/note-chip';
import type { MessageFailure } from '@/next/features/chat/types';
import {
  DIRECTION,
  isMovable,
  movableCount,
  planMove,
  planStep,
  QUEUED,
  QUEUED_KINDS,
  type IMovePlan,
  type TDirection,
} from '@/next/features/composer/queue-order';
import type { QueuePause, QueueRow } from '@/next/features/composer/types';
import { boxOf, markEdit, noteExit, rowStands } from '@/next/ui/flight';
import Icon, { type TIconName } from '@/next/ui/icon';
import { PRESENCE, useLeavingList, usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';
import Tooltip from '@/next/ui/tooltip';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import useDrag from '@/next/ui/use-drag';
import useFlip from '@/next/ui/use-flip';
import { t } from '@/next/i18n';

/** The places of a queued message on its one line, each with its kind's icon. */
const RefLine = ({ refs }: { refs: readonly MessageRef[] }) => (
  <>
    {refs.map((ref) => (
      <span key={refKey(ref)} className="hx-noteline hx-refline" title={ref.title || ref.id}>
        <Icon name={REF_ICON[ref.kind]} size="sm" />
        {ref.title || ref.id}
      </span>
    ))}
  </>
);

/** A row folds away, and the whole stack when its last row goes, as long as hx-fold in composer.css. */
const ROW_EXIT_MS = 320;

type TRowState = 'queued' | 'paused' | 'limit' | 'sending' | 'failed';

const ICONS: Record<TRowState, TIconName> = {
  queued: 'list-end',
  paused: 'pause',
  limit: 'clock',
  sending: 'loader-circle',
  failed: 'circle-alert',
};

const FAILURE_KEYS: Partial<Record<MessageFailure, string>> = {
  'launch-failed': 'next.queue.failLaunch',
  'agent-gone': 'next.queue.failGone',
  'stuck-in-input': 'next.queue.failStuck',
  'returned-to-input': 'next.queue.failReturned',
};

/** A queue held by a launch that failed or an agent that left says so; a pause of the person's is only a pause. */
const PAUSE_KEYS: Partial<Record<QueuePause['reason'], string>> = {
  'launch-failed': 'next.queue.pausedLaunch',
  'agent-gone': 'next.queue.pausedGone',
} as const;

const pauseNote = (pause: QueuePause | null): string => {
  const key = pause ? PAUSE_KEYS[pause.reason] : undefined;
  return t(key ?? 'next.queue.paused');
};

const rowState = (row: QueueRow, paused: boolean, limit: boolean): TRowState => {
  const k = row.state.k;
  if (k === 'failed') return 'failed';
  if (k === 'submitting' || k === 'typing' || k === 'in-input' || k === 'uploading') return 'sending';
  // A message typed into the turn does not wait for its end: it is on its way to the agent, whoever holds it.
  if (row.steer) return 'sending';
  if (paused) return 'paused';
  if (limit) return 'limit';
  return 'queued';
};

interface IQueueProps {
  rows: QueueRow[];
  paused: QueuePause | null;
  /** The subscription's limit holds the queue until it is back. */
  waitingLimit: boolean;
  onRemove: (clientId: string) => Promise<unknown>;
  /** Takes the message out of the queue and its text back into the field. */
  onEdit?: (clientId: string) => Promise<unknown>;
  /** Sends the message right now: into the running turn, or first in line. */
  onSendNow?: (clientId: string) => Promise<unknown>;
  /** Puts the message before another (null: last); without it the rows have no grip. */
  onMove?: (clientId: string, beforeClientId: string | null) => Promise<unknown>;
  onResume: () => Promise<unknown>;
  onRetry: (clientId: string) => Promise<unknown>;
}

/** What a row needs to be carried: the stack holds the move, the row only draws the grip and says what a key asks. */
interface IGrip {
  /** Another row's move waits for the runtime: nothing is picked up meanwhile. */
  locked: boolean;
  /** This row's own move waits. */
  pending: boolean;
  /** Asks for the row one place up or down; settles when the runtime has answered. */
  onStep: (clientId: string, direction: TDirection) => Promise<void>;
}

const STEP_KEYS: Record<string, TDirection> = { ArrowUp: DIRECTION.up, ArrowDown: DIRECTION.down };

const Row = ({
  row,
  state,
  first,
  leaving,
  grip,
  props,
}: {
  row: QueueRow;
  state: TRowState;
  first: boolean;
  leaving: boolean;
  grip: IGrip | null;
  props: IQueueProps;
}) => {
  const action = usePending<'remove' | 'resume' | 'retry' | 'edit' | 'now'>();
  const mainRef = useRef<HTMLSpanElement>(null);
  useLayoutEffect(() => rowStands(row.clientId), [row.clientId]);
  // Where the words stood when the row went: a bubble that takes the message into the feed flies out of this place.
  // A design's notes ride in the text as `<annotation>` blocks for the agent; the row shows the words and a line per note.
  // The places attached to it ride in the text as an `<attached>` block: the row shows a line of their names.
  const placed = useMemo(() => attachedOf(row.text, row.refs), [row.text, row.refs]);
  const { text: words, notes } = useMemo(() => readAnnotated(placed.text), [placed.text]);
  useLayoutEffect(() => {
    if (leaving && mainRef.current) noteExit(row.clientId, boxOf(mainRef.current), words);
  }, [leaving, row.clientId, words]);
  const busy = isBusy(action.phase);
  const { onEdit, onSendNow } = props;
  let note = '';
  if (state === 'paused') note = pauseNote(props.paused);
  if (state === 'limit') note = t('next.queue.limit');
  if (state === 'sending') note = row.handed ? t('next.message.handed') : t('next.queue.sending');
  // Text in the terminal's input line that is not this message holds it back: the person sees why, not a silent wait.
  if (row.state.k === 'queued' && row.state.reason === 'blocked-text') note = t('next.queue.blockedText');
  if (state === 'failed' && row.state.k === 'failed')
    note = t(FAILURE_KEYS[row.state.code] ?? 'next.queue.failed');
  if (action.phase === PENDING.failed) note = t('next.queue.actionFailed');
  if (action.phase === PENDING.slow) note = t('next.queue.slow');
  const carried = grip !== null && isMovable(row);
  const onGripKey = (event: KeyboardEvent<HTMLButtonElement>) => {
    const direction = STEP_KEYS[event.key];
    if (!grip || direction === undefined || event.ctrlKey || event.metaKey || event.shiftKey) return;
    event.preventDefault();
    const handle = event.currentTarget;
    // A row that moved in the DOM drops the focus of its grip: the keyboard keeps carrying the same row.
    void grip.onStep(row.clientId, direction).then(() =>
      requestAnimationFrame(() => {
        if (!document.activeElement || document.activeElement === document.body)
          handle.focus({ preventScroll: true });
      }),
    );
  };
  return (
    <div
      className="hx-queue__row"
      role="listitem"
      data-st={state === 'queued' ? undefined : state}
      data-leaving={leaving || undefined}
      data-busy={busy || undefined}
      data-moving={grip?.pending || undefined}
    >
      {carried ? (
        <Tooltip label={t('next.queue.reorder')} keys={['↑', '↓']}>
          <button
            type="button"
            className="hx-queue__grip"
            data-drag={grip?.locked ? undefined : QUEUED}
            data-drag-id={row.clientId}
            data-drag-box="drop"
            aria-label={t('next.queue.reorder')}
            aria-keyshortcuts="ArrowUp ArrowDown Alt+ArrowUp Alt+ArrowDown"
            onKeyDown={onGripKey}
          >
            <Icon name="grip-vertical" size="sm" />
          </button>
        </Tooltip>
      ) : (
        <Icon name={ICONS[state]} size="sm" className="hx-queue__ic" />
      )}
      <span ref={mainRef} className="hx-queue__main">
        <span className="hx-queue__text">
          {(words || (notes.length === 0 && placed.refs.length === 0)) && (
            <span>{words || t('next.queue.attachmentsOnly')}</span>
          )}
          <NoteLine notes={notes} />
          <RefLine refs={placed.refs} />
        </span>
        {note && <span className="hx-queue__note">{note}</span>}
      </span>
      {row.attachments.length > 0 && (
        <span className="hx-queue__files">
          {row.attachments.map((file) => (
            <span key={file.id} title={file.name}>
              <AttachmentThumb item={file} className="hx-queue__file" />
            </span>
          ))}
        </span>
      )}
      {state === 'paused' && first && (
        <button
          type="button"
          className="hx-queue__steer"
          data-busy={(busy && action.key === 'resume') || undefined}
          disabled={busy}
          onClick={() => action.run('resume', props.onResume)}
        >
          <Icon name="play" size="sm" />
          {t('next.queue.resume')}
          <span className="hx-spin" aria-hidden="true" />
        </button>
      )}
      {state === 'failed' && (
        <button
          type="button"
          className="hx-queue__steer"
          data-busy={(busy && action.key === 'retry') || undefined}
          disabled={busy}
          onClick={() => action.run('retry', () => props.onRetry(row.clientId))}
        >
          <Icon name="rotate-ccw" size="sm" />
          {t('next.queue.retry')}
          <span className="hx-spin" aria-hidden="true" />
        </button>
      )}
      {state === 'queued' && onSendNow && (
        <Tooltip label={t('next.queue.sendNow')}>
          <button
            type="button"
            className="hx-queue__del"
            data-act="now"
            aria-label={t('next.queue.sendNow')}
            data-busy={(busy && action.key === 'now') || undefined}
            disabled={busy}
            onClick={() => action.run('now', () => onSendNow(row.clientId))}
          >
            <Icon name="arrow-up" size="sm" />
            <span className="hx-spin" aria-hidden="true" />
          </button>
        </Tooltip>
      )}
      {state === 'queued' && onEdit && (
        <Tooltip label={t('next.queue.edit')}>
          <button
            type="button"
            className="hx-queue__del"
            data-act="edit"
            aria-label={t('next.queue.edit')}
            data-busy={(busy && action.key === 'edit') || undefined}
            disabled={busy}
            onClick={() => {
              // The words go back down into the field: it is told where they stood.
              if (mainRef.current) markEdit(boxOf(mainRef.current), words);
              action.run('edit', () => onEdit(row.clientId));
            }}
          >
            <Icon name="pencil" size="sm" />
            <span className="hx-spin" aria-hidden="true" />
          </button>
        </Tooltip>
      )}
      {state !== 'sending' && (
        <Tooltip label={t('next.queue.remove')}>
          <button
            type="button"
            className="hx-queue__del"
            aria-label={t('next.queue.remove')}
            data-busy={(busy && action.key === 'remove') || undefined}
            disabled={busy}
            onClick={() => action.run('remove', () => props.onRemove(row.clientId))}
          >
            <Icon name="trash-2" size="sm" />
            <span className="hx-spin" aria-hidden="true" />
          </button>
        </Tooltip>
      )}
    </div>
  );
};

/** The rows as the stack draws them: each in its own fold, so it opens and closes by its height and the rest move. */
const Stack = ({ props, closing }: { props: IQueueProps; closing: boolean }) => {
  const { rows, onMove } = props;
  const listRef = useRef<HTMLDivElement>(null);
  const list = useLeavingList(rows, (row) => row.clientId, ROW_EXIT_MS);
  // The rows that were there when the stack opened come with it; only the ones that join later open by themselves.
  const [together] = useState(() => new Set(rows.map((row) => row.clientId)));

  // A move waits for the runtime (pessimistic): the rows change places when it has said so, not when the drop is made.
  const reorder = onMove !== undefined && movableCount(rows) > 1;
  const [movingId, setMovingId] = useState<string | null>(null);
  const move = useCallback(
    async (plan: IMovePlan) => {
      if (!onMove) return;
      setMovingId(plan.id);
      try {
        await onMove(plan.id, plan.beforeId);
      } catch (error) {
        toast(t('next.queue.moveFailed'), { tone: 'error', description: (error as Error).message });
      } finally {
        setMovingId(null);
      }
    },
    [onMove],
  );
  useDrag({
    listRef,
    dragKinds: QUEUED_KINDS,
    dropKinds: QUEUED_KINDS,
    lined: () => true,
    resolve: (source, target) =>
      reorder && target.id && target.edge ? planMove(rows, source.id, target.id, target.edge) : null,
    onDrop: (plan) => void move(plan),
  });
  const step = useCallback(
    async (clientId: string, direction: TDirection) => {
      const plan = movingId === null ? planStep(rows, clientId, direction) : null;
      if (plan) await move(plan);
    },
    [rows, movingId, move],
  );
  useFlip(listRef, true);

  const grip: IGrip | null = reorder ? { locked: movingId !== null, pending: false, onStep: step } : null;
  return (
    <>
      <div className="hx-qfold" data-state={closing ? PRESENCE.closing : undefined}>
        <div className="hx-qfold__in">
          <div ref={listRef} className="hx-queue" role="list" aria-label={t('next.queue.label')}>
            {list.map(({ item, leaving }, index) => (
              <div
                key={item.clientId}
                className="hx-queue__slot"
                data-flip={item.clientId}
                data-drop={reorder && !leaving && isMovable(item) ? QUEUED : undefined}
                data-drop-id={item.clientId}
                data-fresh={!together.has(item.clientId) || undefined}
                data-leaving={leaving || undefined}
              >
                <div className="hx-queue__clip">
                  <Row
                    row={item}
                    state={rowState(item, props.paused !== null, props.waitingLimit)}
                    first={index === 0}
                    leaving={leaving}
                    grip={grip && { ...grip, pending: movingId === item.clientId }}
                    props={props}
                  />
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </>
  );
};

const Queue = (props: IQueueProps) => {
  // The stack is there while it has rows and folds away when the last one has gone.
  const { value, state } = usePresence(props.rows.length > 0 ? true : null, ROW_EXIT_MS);
  if (value === null) return null;
  return <Stack props={props} closing={state === PRESENCE.closing} />;
};

export default memo(Queue);
