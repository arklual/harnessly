// The subagents of the chat (.hx-subcard): one card for each agent the open chat started — where it is, its topic and
// model, and what it returned. The card asked for (the feed's «Открыть») comes open and into view. The agents'
// own steps are not shown: the runtime does not keep them in the chat's transcript. The agents of its dynamic workflows
// follow, grouped by workflow and phase (workflow-pane.tsx).
import { useState } from 'react';
import Md from '@/next/features/chat/md';
import type { SubagentEntry } from '@/next/features/chat/types';
import { excerptOf } from '@/next/features/subagents/excerpt';
import { subagentName, subagentNote } from '@/next/features/subagents/subs-block';
import type { TWorkflowTask } from '@/next/features/workflows/run';
import WorkflowPane from '@/next/features/workflows/workflow-pane';
import { t } from '@/next/i18n';

const CARD_STATUS = { running: 'run', done: 'done', failed: 'failed' } as const satisfies Record<
  SubagentEntry['status'],
  string
>;

const STATE_LABELS = {
  running: 'next.subs.stateRun',
  done: 'next.subs.stateDone',
  failed: 'next.subs.stateFailed',
} as const satisfies Record<SubagentEntry['status'], string>;

const toView = (card: HTMLElement | null) => card?.scrollIntoView({ block: 'nearest' });

const Result = ({ entry, focused }: { entry: SubagentEntry; focused: boolean }) => {
  // The person's own choice wins over the card being the one asked for.
  const [chosen, setChosen] = useState<boolean | null>(null);
  const full = chosen ?? focused;
  const text = entry.output?.text.trim() ?? '';
  if (!text)
    return (
      <div className="hx-subcard__last">
        {entry.status === 'running' ? t('next.subs.waiting') : t('next.subs.noAnswer')}
      </div>
    );
  const excerpt = excerptOf(text);
  return (
    <>
      <div className="hx-subcard__last" data-full={full}>
        <Md md={full ? text : excerpt.text} />
        {full && entry.output?.truncated && (
          <div className="hx-tv__muted">
            {t('next.tool.truncated', {
              shown: entry.output.truncated.shown,
              total: entry.output.truncated.total,
            })}
          </div>
        )}
      </div>
      {excerpt.more && (
        <button type="button" className="hx-tv__link" onClick={() => setChosen(!full)}>
          {full ? t('next.subs.collapse') : t('next.subs.showAll')}
        </button>
      )}
    </>
  );
};

export interface ISubagentViewProps {
  agents: readonly SubagentEntry[];
  /** The chat's dynamic workflows, whose agents are listed after its subagents. */
  workflows?: readonly TWorkflowTask[];
  /** The agent asked for: its id (or a workflow's). */
  focus: string | null;
}

const SubagentView = ({ agents, workflows = [], focus }: ISubagentViewProps) => {
  if (agents.length === 0 && workflows.length === 0)
    return (
      <div className="hx-tv" data-tool="subagents">
        <div className="hx-tv__empty">{t('next.subs.empty')}</div>
      </div>
    );
  return (
    <div className="hx-tv" data-tool="subagents">
      {agents.map((entry) => {
        const focused = entry.agentId === focus;
        return (
          <div
            key={entry.agentId}
            ref={focused ? toView : undefined}
            className="hx-subcard"
            data-status={CARD_STATUS[entry.status]}
            data-focus={focused}
          >
            <div className="hx-subcard__head">
              <span className="hx-subcard__dot" />
              <span className="hx-tv__name">{subagentName(entry)}</span>
              <span className="hx-spacer" />
              <span className="hx-tv__muted">{t(STATE_LABELS[entry.status])}</span>
            </div>
            <div className="hx-tv__sub">{subagentNote(entry)}</div>
            <Result key={String(focused)} entry={entry} focused={focused} />
          </div>
        );
      })}
      <WorkflowPane workflows={workflows} focus={focus} />
    </div>
  );
};

export default SubagentView;
