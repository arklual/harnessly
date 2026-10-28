// `harnessly ask` as a step of the agent's work: the ask's card, live from the machine's list, answerable here.
import { memo, useContext } from 'react';
import { pageText } from '@/next/bindings/outcome';
import { answerAsk, cancelAsk, isAskRefusal, type IAsk, type TAskOutcome } from '@/next/features/asks/api';
import { AskCallCard, AskCard, type IAskCardActions } from '@/next/features/asks/ask-card';
import { AskSource, askOfCall, type IAskCall } from '@/next/features/asks/parse';
import { putAsk, useAsks } from '@/next/features/asks/store';
import type { MachineId } from '@/next/protocol/v2';
import { t } from '@/next/i18n';

const KNOWN = new Set([
  'disabled',
  'no-bot',
  'bot-not-found',
  'unknown-person',
  'bad-person',
  'empty',
  'too-long',
  'rate-limited',
  'not-found',
  'closed',
  'bad-choice',
  'bad-settings',
  'chat-gone',
  'invalid-token',
  'unreachable',
  'rejected',
  'failed',
]);

/** A refusal's words. */
export const askReasonText = (reason: string): string =>
  KNOWN.has(reason) ? t(`next.asks.reason.${reason}`) : t('next.asks.reason.other', { reason });

/** The answer of a change: null when the machine took it (and the list has it), else why not. */
const settle = (machine: MachineId, res: TAskOutcome<IAsk>): string | null => {
  if (!('ok' in res)) return pageText(res);
  if (isAskRefusal(res.value)) return askReasonText(res.value.refused);
  putAsk(machine, res.value);
  return null;
};

/** What a card may do with an ask of a machine. */
export const askActions = (machine: MachineId, id: string, onOpenChat?: () => void): IAskCardActions => ({
  onAnswer: async (answer) => settle(machine, await answerAsk(machine, id, answer)),
  onCancel: async () => settle(machine, await cancelAsk(machine, id)),
  onOpenChat,
});

/** The feed's step: the ask once the list has it, what the command says until then. */
export const AskStep = memo(({ call, ran }: { call: IAskCall; ran: boolean }) => {
  const source = useContext(AskSource);
  const asks = useAsks(source?.machine ?? null);
  const ask = asks && source ? askOfCall(asks, call, source.conversation) : null;
  if (!ask || !source)
    return <AskCallCard kind={call.kind} to={call.to} question={call.question} ran={ran} />;
  return <AskCard ask={ask} actions={askActions(source.machine, ask.id)} />;
});
