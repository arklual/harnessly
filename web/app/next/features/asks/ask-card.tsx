// An agent's question to a person, as one card: whom it asks and where, the question (the largest text), how it
// stands and the answer, every reply after it with what the agent has read, and — while it waits — the person's way to
// answer right here. The feed shows it in place of the `harnessly ask` command; «People» opens it in a dialog.
import { memo, useEffect, useState } from 'react';
import type { IAsk, IAskPerson, IAskReply } from '@/next/features/asks/api';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/asks.css';

/** The reply box's exit, as long as hx-ask-box-out in asks.css. */
const BOX_EXIT_MS = 160;

export const personLabel = (p: IAskPerson | null): string => {
  if (!p) return t('next.asks.wholeChat');
  if (p.username) return `@${p.username}`;
  return p.name;
};

const minutesLeft = (deadline: number, now: number) => Math.max(1, Math.ceil((deadline - now) / 60_000));

/** The line of how the ask stands, with its tone. */
export const askState = (
  ask: IAsk,
  now: number,
): { tone: 'run' | 'ok' | 'warn' | 'fail' | 'off'; text: string } => {
  const who = ask.answer ? personLabel(ask.answer.from) : '';
  switch (ask.status) {
    case 'waiting':
      return { tone: 'run', text: t('next.asks.state.waiting', { min: minutesLeft(ask.deadline, now) }) };
    case 'answered':
      if (ask.decision === 'approved') return { tone: 'ok', text: t('next.asks.state.approved', { who }) };
      if (ask.decision === 'changes') return { tone: 'warn', text: t('next.asks.state.changes', { who }) };
      if (ask.decision === 'comment') return { tone: 'warn', text: t('next.asks.state.comment', { who }) };
      return { tone: 'ok', text: t('next.asks.state.answered', { who }) };
    case 'timeout':
      return { tone: 'off', text: t('next.asks.state.timeout') };
    case 'cancelled':
      return { tone: 'off', text: t('next.asks.state.cancelled') };
    default:
      return { tone: 'fail', text: t('next.asks.state.failed') };
  }
};

const STATE_ICON = {
  run: 'clock',
  ok: 'circle-check',
  warn: 'pencil',
  fail: 'circle-alert',
  off: 'circle-x',
} as const;

/** Re-renders every half minute while the ask waits, so «N min left» moves. */
const useNow = (live: boolean) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!live) return;
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, [live]);
  return now;
};

const replyText = (r: IAskReply): string => {
  if (r.text) return r.text;
  if (r.choice === 'approved') return t('next.asks.approve');
  if (r.choice === 'changes') return t('next.asks.changes');
  return r.choice;
};

const Replies = ({ ask }: { ask: IAsk }) => {
  if (ask.replies.length === 0) return null;
  const agentDone = ask.seenAt > 0;
  return (
    <ol className="hx-ask__replies">
      {ask.replies.map((r, i) => {
        const read = agentDone && i < ask.seenReplies;
        return (
          <li key={`${r.at}-${i}`} className="hx-ask__reply" data-read={read || undefined}>
            <span className="hx-ask__reply-who">{personLabel(r.from)}</span>
            <span className="hx-ask__reply-text">{replyText(r)}</span>
            <span
              className="hx-ask__reply-mark"
              role="img"
              title={read ? t('next.asks.readByAgent') : t('next.asks.notRead')}
              aria-label={read ? t('next.asks.readByAgent') : t('next.asks.notRead')}
            >
              <Icon name={read ? 'check' : 'circle-dot'} size="sm" />
            </span>
          </li>
        );
      })}
    </ol>
  );
};

export interface IAskCardActions {
  /** Resolves to an error's words, or null when the machine took the answer. */
  onAnswer: (answer: { choice?: string; text?: string }) => Promise<string | null>;
  onCancel: () => Promise<string | null>;
  /** Opens the asking agent's chat; absent where the card stands in that chat. */
  onOpenChat?: () => void;
}

interface IAskCardProps {
  ask: IAsk;
  actions: IAskCardActions | null;
}

const AnswerBox = ({ ask, actions }: { ask: IAsk; actions: IAskCardActions }) => {
  const [text, setText] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [writing, setWriting] = useState(ask.kind === 'question' && ask.options.length === 0);
  const box = usePresentFlag(writing, BOX_EXIT_MS);
  const run = (p: Promise<string | null>) => {
    setBusy(true);
    setError(null);
    void p.then((err) => {
      setBusy(false);
      setError(err);
    });
  };
  const send = () => {
    if (!text.trim() || busy) return;
    run(actions.onAnswer({ text: text.trim() }));
  };
  return (
    <div className="hx-ask__answer">
      <div className="hx-ask__acts">
        {ask.kind === 'approval' && (
          <>
            <button
              type="button"
              className="hx-ask__btn"
              data-tone="primary"
              disabled={busy}
              onClick={() => run(actions.onAnswer({ choice: 'approved', text: text.trim() || undefined }))}
            >
              <Icon name="check" size="sm" />
              {t('next.asks.approve')}
            </button>
            <button
              type="button"
              className="hx-ask__btn"
              disabled={busy}
              onClick={() => run(actions.onAnswer({ choice: 'changes', text: text.trim() || undefined }))}
            >
              <Icon name="pencil" size="sm" />
              {t('next.asks.changes')}
            </button>
          </>
        )}
        {ask.options.map((o, i) => (
          <button
            key={o}
            type="button"
            className="hx-ask__btn"
            disabled={busy}
            onClick={() => run(actions.onAnswer({ choice: `o${i}` }))}
          >
            {o}
          </button>
        ))}
        {!writing && (
          <button
            type="button"
            className="hx-ask__btn"
            data-tone="ghost"
            disabled={busy}
            onClick={() => setWriting(true)}
          >
            <Icon name="corner-down-left" size="sm" />
            {ask.kind === 'approval' ? t('next.asks.comment') : t('next.asks.reply')}
          </button>
        )}
        <span className="hx-spacer" />
        <button
          type="button"
          className="hx-ask__btn"
          data-tone="ghost"
          disabled={busy}
          onClick={() => run(actions.onCancel())}
        >
          {t('next.asks.cancel')}
        </button>
      </div>
      {box.mounted && (
        <div className="hx-ask__box" data-state={box.state}>
          <textarea
            className="hx-ask__input"
            rows={2}
            value={text}
            disabled={busy}
            placeholder={ask.kind === 'approval' ? t('next.asks.commentHint') : t('next.asks.replyHint')}
            aria-label={ask.kind === 'approval' ? t('next.asks.commentHint') : t('next.asks.replyHint')}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
                event.preventDefault();
                if (ask.kind === 'question') send();
              }
            }}
          />
          {ask.kind === 'question' && (
            <button
              type="button"
              className="hx-ask__btn"
              data-tone="primary"
              data-busy={busy || undefined}
              disabled={busy || !text.trim()}
              onClick={send}
            >
              <Icon name="send" size="sm" />
              {t('next.asks.send')}
            </button>
          )}
        </div>
      )}
      {error && (
        <div className="hx-ask__err" role="alert">
          <Icon name="circle-alert" size="sm" />
          {error}
        </div>
      )}
    </div>
  );
};

/** The whole ask. */
export const AskCard = memo(({ ask, actions }: IAskCardProps) => {
  const now = useNow(ask.status === 'waiting');
  const state = askState(ask, now);
  const answer = ask.answer;
  // The words of the answer: a question's (an option's text too) or an approval's comment; a decision is the state line.
  const said = answer && (ask.kind === 'question' || ask.decision === 'comment') ? answer.text : '';
  return (
    <article className="hx-ask" data-tone={state.tone} data-kind={ask.kind}>
      <div className="hx-ask__head">
        <Icon
          name={ask.kind === 'approval' ? 'shield-check' : 'circle-question-mark'}
          size="sm"
          className="hx-ask__ic"
        />
        <span className="hx-ask__to">{personLabel(ask.to)}</span>
        <span className="hx-ask__where">{ask.chat}</span>
        {actions?.onOpenChat && ask.agent && (
          <button type="button" className="hx-ask__chat" onClick={actions.onOpenChat}>
            {ask.agent}
          </button>
        )}
      </div>
      <p className="hx-ask__q">{ask.question}</p>
      {ask.details && <p className="hx-ask__details">{ask.details}</p>}
      {ask.link && (
        <a className="hx-ask__link" href={ask.link} target="_blank" rel="noreferrer">
          <Icon name="external-link" size="sm" />
          {ask.link}
        </a>
      )}
      <div className="hx-ask__state" role="status">
        <Icon name={STATE_ICON[state.tone]} size="sm" />
        {state.text}
      </div>
      {said && <blockquote className="hx-ask__said">{said}</blockquote>}
      {ask.error && <p className="hx-ask__details">{ask.error}</p>}
      {ask.replies.length > 1 && <Replies ask={ask} />}
      {ask.status === 'waiting' && actions && <AnswerBox ask={ask} actions={actions} />}
    </article>
  );
});

/** The card of a `harnessly ask` whose ask the list does not hold (yet, or any more): what the command says. */
export const AskCallCard = memo(
  ({ kind, to, question, ran }: { kind: IAsk['kind']; to: string; question: string; ran: boolean }) => (
    <article className="hx-ask" data-tone={ran ? 'run' : 'off'} data-kind={kind}>
      <div className="hx-ask__head">
        <Icon
          name={kind === 'approval' ? 'shield-check' : 'circle-question-mark'}
          size="sm"
          className="hx-ask__ic"
        />
        <span className="hx-ask__to">{to || t('next.asks.wholeChat')}</span>
      </div>
      {question && <p className="hx-ask__q">{question}</p>}
      <div className="hx-ask__state" role="status">
        <Icon name={ran ? 'clock' : 'circle-x'} size="sm" />
        {ran ? t('next.asks.state.sending') : t('next.asks.state.unknown')}
      </div>
    </article>
  ),
);
