// The chat's dynamic workflows in the pane's tab of subagents: each one its name, how far its phases are and, under the
// title of each phase, a card for every agent it started — its label, where it is, the model and time, and what it
// returned or its last step. The card asked for (an agent's «Открыть» in the feed) or the workflow asked for (its row
// in the right column) comes into view.
import {
  agentMeta,
  progressOf,
  STATUS_LOOK,
  STATUS_WORD,
  workflowName,
  type TWorkflowTask,
} from '@/next/features/workflows/run';
import type { WorkflowAgent } from '@/next/protocol/v2';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import '@/next/styles/workflows.css';

const toView = (node: HTMLElement | null) => node?.scrollIntoView({ block: 'nearest' });

const AgentCard = ({ agent, focused }: { agent: WorkflowAgent; focused: boolean }) => {
  const said = agent.result ?? agent.last;
  return (
    <div
      ref={focused ? toView : undefined}
      className="hx-subcard"
      data-status={STATUS_LOOK[agent.status]}
      data-focus={focused}
    >
      <div className="hx-subcard__head">
        <span className="hx-subcard__dot" />
        <span className="hx-tv__name">{agent.label}</span>
        <span className="hx-spacer" />
        <span className="hx-tv__muted">{t(STATUS_WORD[agent.status])}</span>
      </div>
      {agentMeta(agent) && <div className="hx-tv__sub">{agentMeta(agent)}</div>}
      <div className="hx-subcard__last">
        {said ?? t(agent.status === 'running' ? 'next.subs.waiting' : 'next.subs.noAnswer')}
      </div>
    </div>
  );
};

export interface IWorkflowPaneProps {
  workflows: readonly TWorkflowTask[];
  /** The workflow or the agent asked for: its id. */
  focus: string | null;
}

const WorkflowPane = ({ workflows, focus }: IWorkflowPaneProps) => (
  <>
    {workflows.map((run) => (
      <section
        key={run.id}
        ref={run.id === focus ? toView : undefined}
        className="hx-flowpane"
        data-status={STATUS_LOOK[run.status]}
        data-focus={run.id === focus}
      >
        <div className="hx-flowpane__head">
          <Icon name="layers" size="sm" />
          <span className="hx-tv__name">
            {t('next.flow.kind')} · {workflowName(run)}
          </span>
          <span className="hx-spacer" />
          <span className="hx-tv__muted">{t(STATUS_WORD[run.status])}</span>
        </div>
        {run.workflow.description && <div className="hx-tv__sub">{run.workflow.description}</div>}
        {progressOf(run.workflow) && <div className="hx-flowpane__progress">{progressOf(run.workflow)}</div>}
        {run.workflow.phases
          .filter((phase) => phase.agents.length > 0)
          .map((phase, i) => (
            <div key={`${i}:${phase.title}`} className="hx-flowpane__phase">
              <div className="hx-flowpane__phase-title">{phase.title || t('next.flow.noPhase')}</div>
              {phase.agents.map((agent) => (
                <AgentCard key={agent.id} agent={agent} focused={agent.id === focus} />
              ))}
            </div>
          ))}
      </section>
    ))}
  </>
);

export default WorkflowPane;
