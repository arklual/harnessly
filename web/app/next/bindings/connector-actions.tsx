// The actions outward an agent proposes through a connector (internal/connectors: send a letter, a message, create a
// meeting), for the chat on screen: the card stands over the field of the chat whose agent asked, shows exactly what
// will go and where, and only the person's yes runs it. The list follows the machine by a long wait on its revision
// (<platform>/connector-actions?rev=), not by asking every few seconds; a hidden page stops waiting.
import { useCallback, useEffect, useState } from 'react';
import { pageText } from '@/next/bindings/outcome';
import {
  ANSWER_VERB,
  answerConnectorProposal,
  isConnectorRefusal,
  loadConnectorProposals,
  type IConnectorProposal,
  type TAnswerVerb,
} from '@/next/features/connectors/api';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import { usePresence } from '@/next/ui/presence';
import '@/next/styles/settings-changes.css';
import '@/next/styles/connectors.css';

/** The card's exit, as long as hx-setask-out in settings-changes.css. */
const CARD_EXIT_MS = 180;
/** A failed wait tries again after this long. */
const RETRY_MS = 5_000;

const NONE: readonly IConnectorProposal[] = [];

/** The proposals waiting on a machine, kept in step by waiting on its revision while the page is visible. */
const useConnectorProposals = (machine: MachineId) => {
  const [read, setRead] = useState<{ machine: MachineId; list: readonly IConnectorProposal[] } | null>(null);
  const [nudge, setNudge] = useState(0);
  useEffect(() => {
    let live = true;
    let running = false;
    let rev = '';
    let timer = 0;
    const step = async () => {
      if (!live || running || document.visibilityState !== 'visible') return;
      running = true;
      const res = await loadConnectorProposals(machine, rev);
      running = false;
      if (!live) return;
      if (!('ok' in res) || isConnectorRefusal(res.value)) {
        timer = window.setTimeout(() => void step(), RETRY_MS);
        return;
      }
      rev = res.value.rev;
      setRead({ machine, list: res.value.proposals });
      void step();
    };
    const onVisible = () => {
      if (document.visibilityState === 'visible') void step();
    };
    void step();
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      live = false;
      window.clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, [machine, nudge]);
  const settle = useCallback(
    (id: string) => {
      setRead((was) =>
        was?.machine === machine ? { machine, list: was.list.filter((p) => p.id !== id) } : was,
      );
      setNudge((n) => n + 1);
    },
    [machine],
  );
  return { list: read?.machine === machine ? read.list : NONE, settle };
};

interface ICardProps {
  proposal: IConnectorProposal;
  closing: boolean;
  onAnswer: (proposal: IConnectorProposal, verb: TAnswerVerb) => Promise<string | null>;
}

const ConnectorActionCard = ({ proposal, closing, onAnswer }: ICardProps) => {
  const [busy, setBusy] = useState<TAnswerVerb | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const answer = async (verb: TAnswerVerb) => {
    if (busy !== null) return;
    setBusy(verb);
    setFailure(null);
    const refused = await onAnswer(proposal, verb);
    setBusy(null);
    setFailure(refused);
  };
  return (
    <section
      className="hx-setask"
      data-state={closing ? 'closing' : undefined}
      aria-label={t('next.connectors.ask.aria')}
    >
      <div className="hx-setask__text">
        <span className="hx-setask__lead">{t('next.connectors.ask.lead')}</span>
        <strong className="hx-setask__title">
          {proposal.title} · {proposal.name}
        </strong>
        <dl className="hx-cx-args">
          {proposal.args.map((arg) => (
            <div key={arg.name} className="hx-cx-args__row">
              <dt>{arg.name}</dt>
              <dd>{arg.value}</dd>
            </div>
          ))}
        </dl>
        {proposal.why !== '' && <span className="hx-setask__why">{proposal.why}</span>}
      </div>
      {failure && (
        <p className="hx-setask__fail" role="alert">
          {failure}
        </p>
      )}
      <div className="hx-setask__acts">
        <Button
          size="sm"
          variant="ghost"
          busy={busy === ANSWER_VERB.decline}
          disabled={busy !== null}
          onClick={() => void answer(ANSWER_VERB.decline)}
        >
          {t('next.connectors.ask.no')}
        </Button>
        <Button
          size="sm"
          variant="primary"
          busy={busy === ANSWER_VERB.approve}
          disabled={busy !== null}
          onClick={() => void answer(ANSWER_VERB.approve)}
        >
          {t('next.connectors.ask.yes')}
        </Button>
      </div>
    </section>
  );
};

interface IConnectorActionsProps {
  machine: MachineId;
  /** The chat: only what its agent proposed is asked here. */
  conversation: string;
}

export const ConnectorActions = ({ machine, conversation }: IConnectorActionsProps) => {
  const proposals = useConnectorProposals(machine);
  const proposal = proposals.list.find((p) => p.conv === conversation) ?? null;
  const card = usePresence(proposal, CARD_EXIT_MS);

  const onAnswer = async (asked: IConnectorProposal, verb: TAnswerVerb): Promise<string | null> => {
    const res = await answerConnectorProposal(machine, asked.id, verb);
    if (!('ok' in res)) return pageText(res);
    if (isConnectorRefusal(res.value)) {
      // Answered elsewhere or out of time: the list read again takes the card away.
      proposals.settle(asked.id);
      return null;
    }
    proposals.settle(asked.id);
    return res.value.state === 'failed' ? res.value.result : null;
  };

  if (card.value === null) return null;
  return (
    <ConnectorActionCard
      key={card.value.id}
      proposal={card.value}
      closing={card.state === 'closing'}
      onAnswer={onAnswer}
    />
  );
};
