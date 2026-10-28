// «Продолжить здесь»: a chat of another machine continues on the one the person works on in one press. What
// the press can decide by itself it does (the chat's own agent and model, its files when they can come); what needs
// the person's word (going on without the files) opens the sheet on this machine; what cannot be done says why.
import type { IMoveAgent, IMoveModel, IMoveTarget, IMoveTargets } from '@/next/features/chat-move/api';
import { reasonOf } from '@/next/features/chat-move/move-sheet';
import { t } from '@/next/i18n';

export const HERE = {
  move: 'move',
  ask: 'ask',
  refused: 'refused',
} as const;

export type THere =
  | { k: typeof HERE.move; target: IMoveTarget; agent: IMoveAgent; model: IMoveModel }
  | { k: typeof HERE.ask }
  | { k: typeof HERE.refused; reason: string };

/** The chat's agent as it runs now: its provider and model, when the record says. */
export interface IChatAgent {
  provider: string | null;
  model: string | null;
}

const agentFor = (target: IMoveTarget, chat: IChatAgent): IMoveAgent | null =>
  target.agents.find((a) => a.provider === chat.provider && a.profile === '') ??
  target.agents.find((a) => a.provider === chat.provider) ??
  target.agents[0] ??
  null;

const modelFor = (agent: IMoveAgent, chat: IChatAgent): IMoveModel | null =>
  agent.models.find((m) => m.id === chat.model) ??
  agent.models.find((m) => m.default) ??
  agent.models[0] ??
  null;

/** What one press of «Продолжить здесь» does with what the gateway said about the chat and the machines. */
export const pickHere = (data: IMoveTargets, here: string, chat: IChatAgent): THere => {
  const { source } = data;
  if (source.busy) return { k: HERE.refused, reason: t('next.moveVm.sourceBusy') };
  if (!source.ok) return { k: HERE.refused, reason: t('next.moveVm.noHistory') };
  const target = data.targets.find((x) => x.machineId === here);
  if (!target) return { k: HERE.refused, reason: t('next.moveVm.here.notTarget') };
  if (!target.ok) return { k: HERE.refused, reason: reasonOf(target) ?? t('next.moveVm.failed') };
  if (!target.filesOk || source.dirty) return { k: HERE.ask };
  const agent = agentFor(target, chat);
  const model = agent ? modelFor(agent, chat) : null;
  if (!agent || !model) return { k: HERE.ask };
  return { k: HERE.move, target, agent, model };
};
