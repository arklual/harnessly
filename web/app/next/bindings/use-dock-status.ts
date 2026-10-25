// What the server does by itself for the open chat, as the dock over the field shows it: the model change that waits
// for the agent (conversation.pendingModel) and the steps of an agent that starts for a message. Everything comes from
// the stores the runtime fills; nothing here asks the server again.
import { effortLabel, modelName, type IPickerAgent } from '@/next/features/composer/agents';
import {
  pendingLine,
  WHEN,
  type ILaunchStep,
  type IPendingLine,
} from '@/next/features/composer/launch-chain';
import { useLaunchChain } from '@/next/features/composer/use-launch-chain';
import type { Conversation } from '@/next/protocol/v2';
import type { MessageView } from '@/next/stores/messages';
import { t } from '@/next/i18n';

/** Messages the agent has not taken yet: queued, being typed, in its box, or still going to the runtime. */
const NOT_TAKEN: ReadonlySet<string> = new Set(['queued', 'typing', 'in-input', 'submitting']);

export const countWaiting = (messages: readonly MessageView[]): number =>
  messages.filter((m) => NOT_TAKEN.has(m.state.k)).length;

/** «Sonnet 5.5 · effort High», or only «effort High» when the model stays; the depth is named as the picker names it. */
const target = (line: IPendingLine, agent: IPickerAgent | null): string => {
  const parts: string[] = [];
  if (line.model) parts.push(agent ? modelName(agent, line.model) : line.model);
  if (line.effort) parts.push(t('next.modelSwitch.effort', { effort: effortLabel(line.effort) }));
  if (line.ultracode !== null) parts.push(ultracodeWord(line.ultracode));
  return parts.join(' · ');
};

const ultracodeWord = (on: boolean): string =>
  t(on ? 'next.modelSwitch.ultracodeOn' : 'next.modelSwitch.ultracodeOff');

const ULTRACODE_SENTENCE = {
  launch: {
    on: 'next.modelSwitch.pendingLaunchUltracodeOn',
    off: 'next.modelSwitch.pendingLaunchUltracodeOff',
  },
  turn: { on: 'next.modelSwitch.pendingTurnUltracodeOn', off: 'next.modelSwitch.pendingTurnUltracodeOff' },
} as const;

/** The sentence for a change that waits: after the turn, or when the agent starts. */
export const pendingSentence = (line: IPendingLine, agent: IPickerAgent | null): string => {
  const to = target(line, agent);
  const launch = line.when === WHEN.launch;
  if (line.model)
    return t(launch ? 'next.modelSwitch.pendingLaunch' : 'next.modelSwitch.pendingTurn', { to });
  if (!line.effort && line.ultracode !== null)
    return t(ULTRACODE_SENTENCE[launch ? 'launch' : 'turn'][line.ultracode ? 'on' : 'off']);
  return t(launch ? 'next.modelSwitch.pendingLaunchEffort' : 'next.modelSwitch.pendingTurnEffort', {
    effort: line.effort ? effortLabel(line.effort) : '',
  });
};

export interface IDockStatus {
  pending: string | null;
  steps: ILaunchStep[] | null;
}

export const useDockStatus = (
  record: Conversation | undefined,
  messages: readonly MessageView[],
  agent: IPickerAgent | null,
): IDockStatus => {
  const waiting = countWaiting(messages);
  const steps = useLaunchChain({ agent: record?.state.k, pending: record?.pendingModel, waiting });
  const line = pendingLine(record?.pendingModel, record?.state.k, record?.model ?? null);
  const pending = line ? pendingSentence(line, agent) : null;
  return { pending, steps };
};
