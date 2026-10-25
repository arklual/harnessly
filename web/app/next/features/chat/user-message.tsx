// The person's message. Confirmed it is a plain bubble; while it goes out it is pale and says what happens (spec §1:
// sending → «takes longer than usual» after 3 s). Without a network, or while the machine does not answer, it waits
// quietly and goes by itself (Delete only); a red edge, the reason and Delete are for a message the runtime refused.
import AttachmentThumb, { ThumbSet } from '@/next/ui/attachment-thumb';
import { memo } from 'react';
import { MESSAGE_PHASE, useMessagePhase, type TMessagePhase } from '@/next/features/chat/message-phase';
import type { AttachmentRef, Message, MessageFailure } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import { readAnnotated, type ISentNote } from '@/next/features/designly/annotation';
import { NoteChip } from '@/next/features/designly/note-chip';
import { bubbleParts, Pasted, splitPasted } from '@/next/features/chat/pasted';
import { parsePeer } from '@/next/features/chat/peer';
import { parseContextBlock } from '@/next/features/chat/handoff';
import { ContextCard } from '@/next/features/chat/handoff-card';
import { PEER_DELIVERY, PeerCard, type TPeerDelivery } from '@/next/features/chat/peer-card';
import { PictureCards, useNamedPictures, withoutPaths } from '@/next/features/chat/pictures';
import { attachedOf } from '@/next/features/chat/attached';
import { RefChips } from '@/next/features/chat/ref-chips';

export { splitPasted };

const FAILURE_KEYS: Record<MessageFailure, string> = {
  'upload-failed': 'next.message.failUpload',
  refused: 'next.message.failRefused',
  'launch-failed': 'next.message.failLaunch',
  'agent-gone': 'next.message.failGone',
  'type-failed': 'next.message.failType',
  'stuck-in-input': 'next.message.failStuck',
  unconfirmed: 'next.message.failUnconfirmed',
  expired: 'next.message.failExpired',
  'returned-to-input': 'next.message.failReturned',
};

/** «Not sent» and why: for a refusal the runtime's own reason, in the person's words. */
const reasonOf = (message: Message, code: MessageFailure): string =>
  code === 'refused' && message.refusal
    ? t('next.message.failRefused', { reason: t(`next.refuse.${message.refusal}`) })
    : t(FAILURE_KEYS[code]);

/** data-send of the bubble: how pale it is and whether it has the red edge. */
const SEND_LOOK: Record<TMessagePhase, string> = {
  draft: 'sending',
  sending: 'sending',
  slow: 'sending',
  'waiting-net': 'waiting',
  'waiting-machine': 'waiting',
  queued: 'sending',
  handed: 'waiting',
  sent: 'sent',
  failed: 'failed',
  cancelled: 'leaving',
};

/** What a steered message says of itself above its bubble, by how far it got; without a text it says nothing. */
const STEER_TAG: Partial<Record<TMessagePhase, string>> = {
  sending: 'next.message.steer',
  slow: 'next.message.steer',
  sent: 'next.message.steerRead',
};

/** Claude writes «[Image #3]» where a pasted picture was; the picture itself is shown above the bubble. */
const IMAGE_TOKEN = /\[Image #\d+\]\s*/g;

/** Pictures as large previews without a name (styles: the chip goes when the picture loads); files as a chip. */
const Attachments = ({ items }: { items: AttachmentRef[] }) => (
  <ThumbSet.Provider value={items}>
    <div className="hx-msg-user__files">
      {items.map((item) => (
        <span key={item.id} className="hx-attach" data-kind={item.kind} title={item.name}>
          <AttachmentThumb item={item} className="hx-attach__thumb" />
          <span className="hx-attach__name">{item.name}</span>
        </span>
      ))}
    </div>
  </ThumbSet.Provider>
);

/** A design's notes on its blocks that went with the words (features/designly/annotation.ts): chips under the bubble. */
const Notes = ({ notes }: { notes: readonly ISentNote[] }) => (
  <div className="hx-msg-user__notes">
    {notes.map((note) => (
      <NoteChip key={note.n} note={note} />
    ))}
  </div>
);

interface IOutgoingStateProps {
  message: Message;
  phase: TMessagePhase;
  onRetry: (clientId: string) => Promise<unknown>;
  onResend?: (clientId: string) => Promise<unknown>;
  onDelete: (clientId: string) => Promise<unknown>;
}

const OutgoingState = ({ message, phase, onRetry, onResend, onDelete }: IOutgoingStateProps) => {
  const action = usePending<'retry' | 'resend' | 'delete'>();
  const busy = isBusy(action.phase);
  if (phase === MESSAGE_PHASE.handed && message.state.k === 'accepted') {
    return (
      <div className="hx-msg-user__state" data-busy="true" role="status">
        <span className="hx-spin" aria-hidden="true" />
        {t('next.message.queuedAtAgent')}
      </div>
    );
  }
  if (phase === MESSAGE_PHASE.handed) {
    return (
      <div className="hx-msg-user__state" role="status">
        <Icon name="corner-down-right" size="sm" />
        {t('next.message.handed')}
      </div>
    );
  }
  if (phase === MESSAGE_PHASE.slow) {
    return (
      <div className="hx-msg-user__state" role="status">
        <Icon name="clock" size="sm" />
        {t('next.message.slow')}
      </div>
    );
  }
  if (phase === MESSAGE_PHASE.waitingNet || phase === MESSAGE_PHASE.waitingMachine) {
    return (
      <div className="hx-msg-user__state" role="status">
        <Icon name="wifi-off" size="sm" />
        {t(phase === MESSAGE_PHASE.waitingNet ? 'next.message.waitingNet' : 'next.message.waitingMachine')}
        <button
          type="button"
          className="hx-msg-user__act"
          data-busy={busy || undefined}
          disabled={busy}
          onClick={() => action.run('delete', () => onDelete(message.clientId))}
        >
          {t('next.message.delete')}
          <span className="hx-spin" aria-hidden="true" />
        </button>
      </div>
    );
  }
  if (phase !== MESSAGE_PHASE.failed || message.state.k !== 'failed') return null;
  const reason = reasonOf(message, message.state.code);
  const retryable = message.state.retryable;
  const resendable = !!message.resendable && !!onResend;
  return (
    <div className="hx-msg-user__state" data-tone="fail" role="alert">
      <Icon name="circle-alert" size="sm" />
      {action.phase === PENDING.failed ? t('next.message.actionFailed') : reason}
      {retryable && (
        <button
          type="button"
          className="hx-msg-user__act"
          data-busy={(busy && action.key === 'retry') || undefined}
          disabled={busy}
          onClick={() => action.run('retry', () => onRetry(message.clientId))}
        >
          {t('next.message.retry')}
          <span className="hx-spin" aria-hidden="true" />
        </button>
      )}
      {resendable && (
        <button
          type="button"
          className="hx-msg-user__act"
          data-busy={(busy && action.key === 'resend') || undefined}
          disabled={busy}
          onClick={() => action.run('resend', () => onResend(message.clientId))}
        >
          {t('next.message.resend')}
          <span className="hx-spin" aria-hidden="true" />
        </button>
      )}
      <button
        type="button"
        className="hx-msg-user__act"
        data-busy={(busy && action.key === 'delete') || undefined}
        disabled={busy}
        onClick={() => action.run('delete', () => onDelete(message.clientId))}
      >
        {t('next.message.delete')}
        <span className="hx-spin" aria-hidden="true" />
      </button>
    </div>
  );
};

interface IUserMessageProps {
  text: string;
  attachments: AttachmentRef[];
  /** The message while it is not a confirmed entry yet; null for a turn's confirmed prompt. */
  outgoing: Message | null;
  /** The entry is the page's own message (it carries the clientId of ours): its words are never shown as a paste. */
  own?: boolean;
  /** Steered into the running turn and read by the agent: the bubble says so above itself. */
  steered?: boolean;
  offline: boolean;
  onRetry: (clientId: string) => Promise<unknown>;
  /** Sends a refused message again as a new one (only for a refusal whose cause may have passed). */
  onResend?: (clientId: string) => Promise<unknown>;
  onDelete: (clientId: string) => Promise<unknown>;
}

const SENT_STATE = { k: 'done', turnId: '' } as const;

/** How far another agent's message got: the outbox's phase while it goes, the transcript's once it is read. */
const peerDelivery = (phase: TMessagePhase, outgoing: Message | null, steered: boolean): TPeerDelivery => {
  if (outgoing === null) return steered ? PEER_DELIVERY.readMidTurn : PEER_DELIVERY.read;
  if (phase === MESSAGE_PHASE.failed) return PEER_DELIVERY.failed;
  if (phase === MESSAGE_PHASE.handed) return PEER_DELIVERY.handed;
  return PEER_DELIVERY.waiting;
};

const UserMessage = ({
  text,
  attachments,
  outgoing,
  own = false,
  steered = false,
  offline,
  onRetry,
  onResend,
  onDelete,
}: IUserMessageProps) => {
  const phase = useMessagePhase(
    outgoing?.state ?? SENT_STATE,
    outgoing?.stateSince ?? 0,
    offline,
    outgoing?.handed ?? false,
    outgoing?.waitsMachine ?? false,
  );
  // The places the runtime wrote under the words (`<attached>`) are chips, never the block itself.
  const placed = attachedOf(text, outgoing?.refs);
  // A picture's path in the words (a paste of the old view) is the picture itself, without the path.
  const pictures = useNamedPictures(placed.text);
  // Another agent's words are never the person's bubble (internal/peers delivers them as an envelope).
  const peer = parsePeer(text);
  if (peer) return <PeerCard peer={peer} delivery={peerDelivery(phase, outgoing, steered)} />;
  // The context another chat handed over, or a fork's result: a card that says where it comes from.
  const block = parseContextBlock(text);
  if (block)
    return (
      <ContextCard
        block={block}
        state={
          outgoing && (
            <OutgoingState
              message={outgoing}
              phase={phase}
              onRetry={onRetry}
              onResend={onResend}
              onDelete={onDelete}
            />
          )
        }
      />
    );
  let tag: string | undefined;
  if (steered) tag = STEER_TAG.sent;
  else if (outgoing?.mode === 'steer') tag = STEER_TAG[phase];
  const words = pictures.named.length > 0 ? withoutPaths(placed.text, pictures.named) : placed.text;
  const { text: shown, notes } = readAnnotated(words.replace(IMAGE_TOKEN, '').trim());
  return (
    <div className="hx-msg" data-kind="user">
      <div className="hx-msg-user" data-send={SEND_LOOK[phase]}>
        {tag && (
          <div className="hx-msg-user__tag">
            <Icon name="corner-down-right" size="sm" />
            {t(tag)}
          </div>
        )}
        {attachments.length > 0 && <Attachments items={attachments} />}
        {placed.refs.length > 0 && (
          <div className="hx-msg-user__files hx-msg-user__refs">
            <RefChips refs={placed.refs} />
          </div>
        )}
        <PictureCards pictures={pictures.named} srcOf={pictures.srcOf} side="user" />
        {shown && (
          <div className="hx-bubble">
            {bubbleParts(shown, own || outgoing !== null).map((part) =>
              part.pasted ? <Pasted key={part.key} text={part.text} /> : <p key={part.key}>{part.text}</p>,
            )}
          </div>
        )}
        {notes.length > 0 && <Notes notes={notes} />}
        {outgoing && (
          <OutgoingState
            message={outgoing}
            phase={phase}
            onRetry={onRetry}
            onResend={onResend}
            onDelete={onDelete}
          />
        )}
      </div>
    </div>
  );
};

export default memo(UserMessage);
