// What the server does with a change of model that cannot go in at once, and the steps of an agent that starts for a
// message, as plain data for the line over the field. The server owns the truth (conversation.pendingModel, the agent's
// state, the messages' states); this only reads it: nothing here sends or retries anything.
import type { AgentStateKind, Effort, Model, PendingModel, PermissionMode } from '@/next/protocol/v2';

export const STEP = { launch: 'launch', model: 'model', send: 'send' } as const;
export type TStep = (typeof STEP)[keyof typeof STEP];

export const STEP_STATE = { todo: 'todo', active: 'active', done: 'done' } as const;
export type TStepState = (typeof STEP_STATE)[keyof typeof STEP_STATE];

export interface ILaunchStep {
  id: TStep;
  state: TStepState;
}

/** When a waiting change goes in: at the end of the turn, or right after the agent is started. */
export const WHEN = { turn: 'turn', launch: 'launch' } as const;
export type TWhen = (typeof WHEN)[keyof typeof WHEN];

export interface IPendingLine {
  when: TWhen;
  /** The model it changes to; null when only the effort changes. */
  model: string | null;
  effort: Effort | null;
  /** Ultracode turned on or off with the change; null when the mode stays as it is. */
  ultracode: boolean | null;
  /** The same of the fast mode, and the permission mode it turns to; absent or null when they stay. */
  fast?: boolean | null;
  permission?: PermissionMode | null;
}

/** The states in which the agent is at work or waiting for the person: the change waits for the end of the turn. */
const AT_WORK: ReadonlySet<AgentStateKind> = new Set(['working', 'waiting-question', 'waiting-permission']);
/** The states with no agent running: the change waits for the launch a message brings about. */
const NOT_STARTED: ReadonlySet<AgentStateKind> = new Set(['stopped', 'gone', 'launch-failed']);

/**
 * The quiet line over the field for a change that waits. None while the agent starts or is about to take it (the steps
 * show that), nor for a state not checked yet: the line would flash and go.
 */
export const pendingLine = (
  pending: PendingModel | undefined,
  agent: AgentStateKind | undefined,
  current: Model | null,
): IPendingLine | null => {
  if (!pending || !agent) return null;
  let when: TWhen;
  if (AT_WORK.has(agent)) when = WHEN.turn;
  else if (NOT_STARTED.has(agent)) when = WHEN.launch;
  else return null;
  const sameModel = current?.id === pending.id;
  const ultra = pending.ultracode === true;
  const ultracode = ultra === (current?.ultracode === true) ? null : ultra;
  const quick = pending.fast === true;
  const fast = quick === (current?.fast === true) ? null : quick;
  const asked = pending.permission ?? null;
  const permission = asked === null || asked === (current?.permission ?? null) ? null : asked;
  const modes = ultracode !== null || fast !== null || permission !== null;
  // A mode alone changes: the depth the chat keeps is no news.
  const onlyMode = sameModel && modes && pending.effort === (current?.effort ?? null);
  const line: IPendingLine = {
    when,
    model: sameModel ? null : pending.id,
    effort: onlyMode ? null : pending.effort,
    ultracode,
  };
  if (fast !== null) line.fast = fast;
  if (permission !== null) line.permission = permission;
  return line;
};

export interface IChainInput {
  agent: AgentStateKind | undefined;
  pending: PendingModel | undefined;
  /** Messages not taken by the agent yet: queued, being typed, in its box. */
  waiting: number;
}

/** Which steps a chain has, fixed when it begins so that a step does not vanish as it is done. */
export interface IChainPlan {
  launch: boolean;
  model: boolean;
  send: boolean;
}

/**
 * A chain begins when the agent starts, or when the runtime types a waiting change into it. The launch is in it only
 * when the chain began at the start; the model only when a change waits; the message only when one is waiting.
 */
export const chainPlan = ({ agent, pending, waiting }: IChainInput): IChainPlan | null => {
  if (agent === 'starting') return { launch: true, model: !!pending, send: waiting > 0 };
  if (pending?.applying) return { launch: false, model: true, send: waiting > 0 };
  return null;
};

/**
 * The agent at work has taken the message the chain was for: what still waits then is what the person queued for the
 * next turn, not this start's message.
 */
const messageTaken = (agent: AgentStateKind | undefined, waiting: number): boolean =>
  waiting === 0 || (agent !== undefined && AT_WORK.has(agent));

/** The chain is over: the agent is up, nothing waits to be applied and the message has been taken. */
export const chainOver = ({ agent, pending, waiting }: IChainInput): boolean => {
  if (agent === 'starting') return false;
  if (agent !== undefined && AT_WORK.has(agent)) return !pending?.applying;
  return !pending && waiting === 0;
};

/** The steps of a plan, each as the stores say it stands now. */
export const chainSteps = (plan: IChainPlan, input: IChainInput): ILaunchStep[] => {
  const starting = input.agent === 'starting';
  const modelWaits = !!input.pending;
  const steps: ILaunchStep[] = [];
  if (plan.launch) steps.push({ id: STEP.launch, state: starting ? STEP_STATE.active : STEP_STATE.done });
  if (plan.model) {
    let state: TStepState = STEP_STATE.done;
    if (starting) state = STEP_STATE.todo;
    else if (modelWaits) state = STEP_STATE.active;
    steps.push({ id: STEP.model, state });
  }
  if (plan.send) {
    let state: TStepState = STEP_STATE.done;
    if (starting || modelWaits) state = STEP_STATE.todo;
    else if (!messageTaken(input.agent, input.waiting)) state = STEP_STATE.active;
    steps.push({ id: STEP.send, state });
  }
  return steps;
};

/** A key of the steps' states: the same while nothing about them changed, so the view is not redrawn for nothing. */
export const stepsKey = (steps: readonly ILaunchStep[]): string =>
  steps.map((step) => `${step.id}:${step.state}`).join(' ');
