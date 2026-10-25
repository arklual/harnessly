// A dynamic workflow in the feed (.hx-flow): the Workflow call's step drawn as one quiet row — «Workflow · name», how
// far its phases are («Review 3/5 · Fix 1/4») and where it is — that opens by height into its agents grouped by phase.
// An agent's row is its label, the model and time muted, its state, and «Открыть» on hover, which shows it in the right
// pane like a subagent. The run comes from the chat's background tasks (WorkflowRuns); a call whose run is no longer
// listed keeps its name and how the call went.
import { memo, useContext, useState } from 'react';
import type { ToolEntry } from '@/next/features/chat/types';
import { openSubagent } from '@/next/features/subagents/open';
import {
  agentLine,
  agentMeta,
  progressOf,
  STATUS_LOOK,
  STATUS_WORD,
  WorkflowRuns,
  workflowName,
  type TWorkflowTask,
} from '@/next/features/workflows/run';
import type { BackgroundStatus, WorkflowAgent, WorkflowPhase } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/workflows.css';

/** How long the list of agents takes to fold back. */
const FOLD_MS = 280;

/** The call's own state, when its run is not listed: running until its result, then done or failed. */
const callStatus = (entry: ToolEntry): BackgroundStatus => {
  if (entry.ok === null) return 'running';
  return entry.ok ? 'done' : 'failed';
};

/** The name the call gave the workflow: its step reads «Workflow <name>». */
const callName = (entry: ToolEntry): string => entry.summary.replace(/^Workflow\s*/, '');

const AgentRow = ({ agent }: { agent: WorkflowAgent }) => {
  const line = agentLine(agent);
  return (
    <div className="hx-flow__agent" data-status={STATUS_LOOK[agent.status]}>
      <div className="hx-flow__agent-row">
        <span className="hx-flow__agent-name">{agent.label}</span>
        <span className="hx-flow__agent-meta">{agentMeta(agent)}</span>
        <button type="button" className="hx-flow__open" onClick={() => openSubagent(agent.id)}>
          {t('next.subs.open')}
          <Icon name="chevron-right" size="sm" />
        </button>
        <span
          className="hx-flow__dot"
          title={t(STATUS_WORD[agent.status])}
          role="img"
          aria-label={t(STATUS_WORD[agent.status])}
        />
      </div>
      {line && <div className="hx-flow__agent-line">{line}</div>}
    </div>
  );
};

const PhaseBlock = ({ phase }: { phase: WorkflowPhase }) => (
  <div className="hx-flow__phase" data-empty={phase.agents.length === 0}>
    <div className="hx-flow__phase-head">
      <span className="hx-flow__phase-title">{phase.title || t('next.flow.noPhase')}</span>
      {phase.agents.length > 0 && (
        <span className="hx-flow__phase-count">
          {phase.agents.filter((agent) => agent.status !== 'running').length}/{phase.agents.length}
        </span>
      )}
    </div>
    {phase.agents.map((agent) => (
      <AgentRow key={agent.id} agent={agent} />
    ))}
  </div>
);

const Agents = ({ run, open }: { run: TWorkflowTask; open: boolean }) => {
  const { mounted, state } = usePresentFlag(open, FOLD_MS);
  if (!mounted) return null;
  return (
    <div className="hx-flow__body" data-state={state}>
      <div className="hx-flow__fold">
        <div className="hx-flow__phases">
          {run.workflow.phases.map((phase, i) => (
            <PhaseBlock key={`${i}:${phase.title}`} phase={phase} />
          ))}
        </div>
      </div>
    </div>
  );
};

/** The Workflow call's step; its run, when the chat still lists it, opens into the agents. */
const WorkflowCard = ({ entry }: { entry: ToolEntry }) => {
  const run = useContext(WorkflowRuns).get(entry.toolUseId) ?? null;
  const [open, setOpen] = useState(false);
  const status = run ? run.status : callStatus(entry);
  const name = run ? workflowName(run) : callName(entry);
  const progress = run ? progressOf(run.workflow) : '';
  const word = t(STATUS_WORD[status]);
  const title = (
    <>
      <Icon name="layers" size="sm" className="hx-flow__icon" />
      <span className="hx-flow__title">
        <span className="hx-flow__kind">{t('next.flow.kind')}</span>
        {name && <span className="hx-flow__name"> · {name}</span>}
      </span>
    </>
  );
  return (
    <div className="hx-flow" data-status={STATUS_LOOK[status]} data-open={open}>
      <div className="hx-flow__row">
        {run ? (
          <button
            type="button"
            className="hx-flow__main"
            aria-expanded={open}
            onClick={() => setOpen((v) => !v)}
          >
            {title}
            <Icon name="chevron-down" size="sm" className="hx-flow__chev" />
          </button>
        ) : (
          <div className="hx-flow__main">{title}</div>
        )}
        {progress && <span className="hx-flow__progress">{progress}</span>}
        <span className="hx-flow__dot" title={word} role="img" aria-label={word} />
      </div>
      {run && <Agents run={run} open={open} />}
    </div>
  );
};

export default memo(WorkflowCard);
