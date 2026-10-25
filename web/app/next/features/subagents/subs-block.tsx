// The subagents a turn started (.hx-subs): one quiet list, a row for each agent. A row is its provider's glyph and its
// topic as the title, the model and the time in muted type, where it is, and «Открыть» on hover. Under it, once the
// agent has returned, a two-line plain summary of what it said; a click opens the whole report by height.
import { memo, useState } from 'react';
import Md from '@/next/features/chat/md';
import type { SubagentEntry } from '@/next/features/chat/types';
import { summaryOf } from '@/next/features/subagents/excerpt';
import { openSubagent } from '@/next/features/subagents/open';
import { duration } from '@/next/i18n/format';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag } from '@/next/ui/presence';
import '@/next/styles/subagents.css';

/** How long the report takes to fold back. */
const FOLD_MS = 280;

const STATE_LABELS = {
  running: 'next.subs.stateRun',
  done: 'next.subs.stateDone',
  failed: 'next.subs.stateFailed',
} as const satisfies Record<SubagentEntry['status'], string>;

/** What a row says besides the agent's name: its topic, the model it runs on and, once done, how long it took. */
export const subagentNote = (entry: SubagentEntry): string =>
  [entry.description, entry.model, entry.durationMs === null ? '' : duration(entry.durationMs)]
    .filter(Boolean)
    .join(' · ');

/** The agent's kind or its own name; an agent that gave neither is the general one. */
export const subagentName = (entry: SubagentEntry): string => entry.agentType || t('next.subs.general');

/** What a card shows as its title: the topic the agent was given, else what it is called. */
export const subagentTitle = (entry: SubagentEntry): string =>
  entry.description.trim() || subagentName(entry);

/** The model and the time, the muted part of a row. */
export const subagentMeta = (entry: SubagentEntry): string =>
  [entry.model, entry.durationMs === null ? '' : duration(entry.durationMs)].filter(Boolean).join(' · ');

/** The glyph of the provider the agent runs on: Codex's models are the only others the feed knows. */
export const providerOf = (...hints: (string | null)[]): 'claude' | 'codex' =>
  /codex|gpt/i.test(hints.join(' ')) ? 'codex' : 'claude';

export const subagentProvider = (entry: SubagentEntry): 'claude' | 'codex' =>
  providerOf(entry.model, entry.agentType);

const Report = ({ entry, open }: { entry: SubagentEntry; open: boolean }) => {
  const { mounted, state } = usePresentFlag(open, FOLD_MS);
  const output = entry.output;
  if (!mounted || !output) return null;
  return (
    <div className="hx-sub__body" data-state={state}>
      <div className="hx-sub__fold">
        <div className="hx-sub__report">
          <Md md={output.text} />
          {output.truncated && (
            <div className="hx-sub__cut">
              {t('next.tool.truncated', { shown: output.truncated.shown, total: output.truncated.total })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

const SubItem = ({ entry }: { entry: SubagentEntry }) => {
  const [open, setOpen] = useState(false);
  const text = entry.output?.text.trim() ?? '';
  const summary = summaryOf(text);
  const toggle = () => setOpen((value) => !value);
  const title = (
    <>
      <i className="hx-agent" data-agent={subagentProvider(entry)} aria-hidden="true" />
      <span className="hx-sub__title">{subagentTitle(entry)}</span>
    </>
  );
  return (
    <div className="hx-sub" data-status={entry.status} data-open={open}>
      <div className="hx-sub__row">
        {text ? (
          <button type="button" className="hx-sub__main" aria-expanded={open} onClick={toggle}>
            {title}
          </button>
        ) : (
          <div className="hx-sub__main">{title}</div>
        )}
        <span className="hx-sub__meta">{subagentMeta(entry)}</span>
        <button type="button" className="hx-sub__open" onClick={() => openSubagent(entry.agentId)}>
          {t('next.subs.open')}
          <Icon name="chevron-right" size="sm" />
        </button>
        <span
          className="hx-sub__state"
          title={t(STATE_LABELS[entry.status])}
          role="img"
          aria-label={t(STATE_LABELS[entry.status])}
        />
      </div>
      {summary && (
        <button type="button" className="hx-sub__sum" tabIndex={-1} aria-hidden="true" onClick={toggle}>
          <span className="hx-sub__sum-in">{summary}</span>
        </button>
      )}
      <Report entry={entry} open={open} />
    </div>
  );
};

/** Subagents that follow each other in a turn are one list. */
const SubsBlock = ({ agents }: { agents: SubagentEntry[] }) => (
  <div className="hx-subs">
    {agents.map((entry) => (
      <SubItem key={entry.id} entry={entry} />
    ))}
  </div>
);

export default memo(SubsBlock);
