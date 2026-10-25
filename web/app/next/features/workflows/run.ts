// A dynamic workflow as the page reads it: a background task of kind `workflow` whose run lists its phases and the
// agents each started. Shared by the feed's card, the right column's row and the pane's list of agents.
import { createContext } from 'react';
import { shortModelName } from '@/next/features/composer/agents';
import type {
  BackgroundStatus,
  BackgroundTask,
  WorkflowAgent,
  WorkflowPhase,
  WorkflowRun,
} from '@/next/protocol/v2';
import { duration } from '@/next/i18n/format';

export type TWorkflowTask = BackgroundTask & { kind: 'workflow'; workflow: WorkflowRun };

export const isWorkflow = (task: BackgroundTask): task is TWorkflowTask =>
  task.kind === 'workflow' && task.workflow !== undefined;

/** The workflows among a chat's background tasks, in their order. */
export const workflowsOf = (tasks: readonly BackgroundTask[]): TWorkflowTask[] => tasks.filter(isWorkflow);

/** An agent that is over, however it ended. */
const over = (agent: WorkflowAgent): boolean => agent.status !== 'running';

/** «Review 3/5»: a phase and how many of the agents it started are over. */
export const phaseProgress = (phase: WorkflowPhase): string =>
  `${phase.title} ${phase.agents.filter(over).length}/${phase.agents.length}`;

/** «Review 3/5 · Verify 1/4»: the phases that have started agents; the ones still ahead are not named. */
export const progressOf = (run: WorkflowRun): string =>
  run.phases
    .filter((phase) => phase.agents.length > 0)
    .map(phaseProgress)
    .join(' · ');

/** What a workflow is called: its script's name, else the summary its launch gave. */
export const workflowName = (task: TWorkflowTask): string => task.workflow.name || task.title;

/** The phase that is at work now: the last one with an agent running, else the last that started any. */
export const currentPhase = (run: WorkflowRun): WorkflowPhase | null =>
  run.phases.findLast((phase) => phase.agents.some((agent) => agent.status === 'running')) ??
  run.phases.findLast((phase) => phase.agents.length > 0) ??
  null;

/** The muted part of an agent's row: the model it runs on and, once it is over, how long it took. */
export const agentMeta = (agent: WorkflowAgent): string => {
  const parts: string[] = [];
  if (agent.model) parts.push(shortModelName(agent.model));
  if (agent.endedAt !== null && agent.startedAt !== null && agent.endedAt > agent.startedAt)
    parts.push(duration(agent.endedAt - agent.startedAt));
  return parts.join(' · ');
};

/** What a row says under the label: what the agent returned once it has, else its last step. */
export const agentLine = (agent: WorkflowAgent): string => agent.result ?? agent.last ?? '';

/** The design's state of a workflow or of one of its agents: at work, done, failed or stopped (never idle). */
export const STATUS_LOOK = {
  running: 'run',
  done: 'done',
  failed: 'failed',
  stopped: 'stopped',
  idle: 'stopped',
} as const satisfies Record<BackgroundStatus, string>;

export const STATUS_WORD = {
  running: 'next.flow.stateRun',
  done: 'next.flow.stateDone',
  failed: 'next.flow.stateFailed',
  stopped: 'next.flow.stateStopped',
  idle: 'next.flow.stateStopped',
} as const satisfies Record<BackgroundStatus, string>;

/** The chat's workflows by the Workflow call that started them: the feed finds a call's run here. */
export const WorkflowRuns = createContext<ReadonlyMap<string, TWorkflowTask>>(new Map());

export const runsByCall = (tasks: readonly BackgroundTask[]): ReadonlyMap<string, TWorkflowTask> => {
  const out = new Map<string, TWorkflowTask>();
  for (const task of workflowsOf(tasks)) if (task.workflow.toolUseId) out.set(task.workflow.toolUseId, task);
  return out;
};
