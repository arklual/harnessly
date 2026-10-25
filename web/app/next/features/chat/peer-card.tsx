// Another agent's message in the feed: not the person's bubble but a card on the left with a line down its side (the
// design's .hx-msg[data-peer]): who wrote (the owner, the machine, their chat), the text with pastes folded, how far it
// got (a quiet line), and «Открыть чат» / «Ответить» when the sender's chat can be reached. And the other way: our
// agent's message to another agent, as a step of the work with its addressee and what came of it.
import { memo, useContext, useState } from 'react';
import { Pasted, splitPasted } from '@/next/features/chat/pasted';
import {
  PEER_KIND,
  PeerActions,
  type IPeerMessage,
  type IPeerOut,
  type TPeerOutcome,
} from '@/next/features/chat/peer';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/peers.css';

/** How far an incoming message got, by the outbox and the transcript. */
export const PEER_DELIVERY = {
  /** In the conversation's queue, not typed yet. */
  waiting: 'waiting',
  /** Typed into the working agent: it reads it after the current step. */
  handed: 'handed',
  /** Read by the agent inside its running turn. */
  readMidTurn: 'readMidTurn',
  /** It opened a turn of its own. */
  read: 'read',
  /** The conversation could not take it. */
  failed: 'failed',
} as const;
export type TPeerDelivery = (typeof PEER_DELIVERY)[keyof typeof PEER_DELIVERY];

const DELIVERY_ICON: Record<TPeerDelivery, string> = {
  waiting: 'clock',
  handed: 'corner-down-right',
  readMidTurn: 'corner-down-right',
  read: 'check',
  failed: 'circle-alert',
};

/** The reply box's exit, as long as hx-peer-reply-out in chat.css. */
const REPLY_EXIT_MS = 180;
/** A body longer than this many lines or characters starts folded. */
const FOLD_LINES = 8;
const FOLD_CHARS = 640;

/** «Иван · dev-department · Модель по умолчанию» without the parts it does not know. */
export const peerLine = (peer: Pick<IPeerMessage, 'person' | 'machine' | 'chat'>): string =>
  [peer.person, peer.machine, peer.chat].filter((part) => part && part.length > 0).join(' · ');

const Body = ({ text }: { text: string }) => {
  const [open, setOpen] = useState(false);
  const long = text.split('\n').length > FOLD_LINES || text.length > FOLD_CHARS;
  return (
    <div className="hx-peer-msg__body" data-open={open || !long || undefined}>
      <div className="hx-peer-msg__text">
        {splitPasted(text).map((part) =>
          part.pasted ? <Pasted key={part.key} text={part.text} /> : <p key={part.key}>{part.text}</p>,
        )}
      </div>
      {long && (
        <button type="button" className="hx-peer-msg__more" onClick={() => setOpen((value) => !value)}>
          {open ? t('next.message.pastedLess') : t('next.message.pastedMore')}
        </button>
      )}
    </div>
  );
};

const SEND_PHASE = { idle: 'idle', sending: 'sending', failed: 'failed' } as const;
type TSendPhase = (typeof SEND_PHASE)[keyof typeof SEND_PHASE];

const outcomeText = (outcome: TPeerOutcome): string => {
  if (outcome.ok) return t('next.peers.delivered');
  if (outcome.reason) return t(`next.peers.reason.${outcome.reason}`);
  return t('next.peers.notDelivered');
};

interface IReplyProps {
  to: string;
  state: 'open' | 'closing';
  onSend: (text: string) => Promise<TPeerOutcome>;
  onDone: (outcome: TPeerOutcome) => void;
  onCancel: () => void;
}

/** The person's answer to the sender, sent back through the same channel; the box stays until it is taken. */
const Reply = ({ to, state, onSend, onDone, onCancel }: IReplyProps) => {
  const [text, setText] = useState('');
  const [phase, setPhase] = useState<TSendPhase>(SEND_PHASE.idle);
  const [refusal, setRefusal] = useState<string | null>(null);
  const send = () => {
    if (!text.trim() || phase === SEND_PHASE.sending) return;
    setPhase(SEND_PHASE.sending);
    setRefusal(null);
    void onSend(text.trim()).then((outcome) => {
      if (outcome.ok) {
        onDone(outcome);
        return;
      }
      setPhase(SEND_PHASE.failed);
      setRefusal(outcomeText(outcome));
    });
  };
  const busy = phase === SEND_PHASE.sending;
  return (
    <div className="hx-peer-reply" data-state={state}>
      <textarea
        className="hx-peer-reply__input"
        rows={2}
        value={text}
        placeholder={t('next.peers.replyTo', { to })}
        aria-label={t('next.peers.replyTo', { to })}
        disabled={busy}
        onChange={(event) => setText(event.target.value)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
            event.preventDefault();
            send();
          }
          if (event.key === 'Escape') onCancel();
        }}
      />
      {refusal && (
        <div className="hx-peer-reply__err" role="alert">
          <Icon name="circle-alert" size="sm" />
          {refusal}
        </div>
      )}
      <div className="hx-peer-reply__foot">
        <button type="button" className="hx-peer-msg__act" onClick={onCancel} disabled={busy}>
          {t('next.peers.cancel')}
        </button>
        <button
          type="button"
          className="hx-peer-msg__act"
          data-tone="primary"
          data-busy={busy || undefined}
          disabled={busy || !text.trim()}
          onClick={send}
        >
          <Icon name="send" size="sm" />
          {t('next.peers.send')}
          <span className="hx-spin" aria-hidden="true" />
        </button>
      </div>
    </div>
  );
};

interface IPeerCardProps {
  peer: IPeerMessage;
  delivery: TPeerDelivery;
}

/** Another agent's message. */
export const PeerCard = memo(({ peer, delivery }: IPeerCardProps) => {
  const actions = useContext(PeerActions);
  const [replying, setReplying] = useState(false);
  const [answered, setAnswered] = useState<TPeerOutcome | null>(null);
  const box = usePresentFlag(replying, REPLY_EXIT_MS);
  const chat = peer.ref && actions ? actions.chatOf(peer.ref) : null;
  const canReply = peer.kind === PEER_KIND.fleet && peer.ref !== null && actions !== null;
  const ref = peer.ref ?? '';
  let who = peer.person;
  if (!who) who = t(`next.peers.kind.${peer.kind}`);
  const where = [peer.machine, peer.chat].filter((part) => part && part.length > 0).join(' · ');
  return (
    <div className="hx-msg" data-kind="peer">
      <div className="hx-peer-msg" data-delivery={delivery}>
        <div className="hx-peer-msg__head">
          <Icon name="messages-square" size="sm" className="hx-peer-msg__ic" />
          <span className="hx-peer-msg__who">{who}</span>
          {where && <span className="hx-peer-msg__where">{where}</span>}
          {peer.byPerson && <span className="hx-peer-msg__by">{t('next.peers.byPerson')}</span>}
        </div>
        <Body text={peer.text} />
        <div className="hx-peer-msg__foot">
          <span className="hx-peer-msg__state" role="status">
            <Icon name={DELIVERY_ICON[delivery]} size="sm" />
            {t(`next.peers.delivery.${delivery}`)}
          </span>
          {answered && (
            <span className="hx-peer-msg__state" data-tone="ok">
              <Icon name="check" size="sm" />
              {t('next.peers.replied')}
            </span>
          )}
          <span className="hx-spacer" />
          {chat !== null && actions && (
            <button type="button" className="hx-peer-msg__act" onClick={() => actions.onOpen(chat)}>
              <Icon name="external-link" size="sm" />
              {t('next.peers.openChat')}
            </button>
          )}
          {canReply && !replying && (
            <button type="button" className="hx-peer-msg__act" onClick={() => setReplying(true)}>
              <Icon name="corner-down-left" size="sm" />
              {t('next.peers.reply')}
            </button>
          )}
        </div>
        {box.mounted && actions && (
          <Reply
            to={who}
            state={box.state}
            onSend={(text) => actions.onReply(ref, text)}
            onDone={(outcome) => {
              setAnswered(outcome);
              setReplying(false);
            }}
            onCancel={() => setReplying(false)}
          />
        )}
      </div>
    </div>
  );
});

const outState = (out: IPeerOut): { tone: 'run' | 'ok' | 'fail'; text: string } => {
  if (out.outcome === null) return { tone: 'run', text: t('next.peers.sending') };
  if (out.outcome.ok) return { tone: 'ok', text: t('next.peers.delivered') };
  return { tone: 'fail', text: outcomeText(out.outcome) };
};

/** Our agent's message to another agent: a step of the work, with the addressee and what came of it. */
export const PeerOutStep = memo(({ out }: { out: IPeerOut }) => {
  const [open, setOpen] = useState(false);
  const state = outState(out);
  let target = out.to;
  if (out.outcome?.ok && out.outcome.to) target = out.outcome.to;
  return (
    <div className="hx-peer-out" data-tone={state.tone} aria-expanded={open}>
      <button
        type="button"
        className="hx-peer-out__head"
        disabled={!out.text}
        onClick={() => setOpen((value) => !value)}
      >
        {state.tone === 'run' ? (
          <span className="hx-tool__spin" aria-hidden="true" />
        ) : (
          <Icon name="arrow-up-right" size="sm" className="hx-peer-out__ic" />
        )}
        <span className="hx-peer-out__label">{t('next.peers.wroteTo')}</span>
        <span className="hx-peer-out__to">{target}</span>
        <span className="hx-spacer" />
        <span className="hx-peer-out__state">{state.text}</span>
      </button>
      {out.text && <div className="hx-peer-out__text">{out.text}</div>}
    </div>
  );
});
