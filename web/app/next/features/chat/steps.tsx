// The agent's work inside a turn, compact (not a log): a thought, one line per tool with its output
// on a click, a run of tools folded into «K actions», subagents, dynamic workflows, a compaction divider and notices.
import { memo, useState, type ReactNode } from 'react';
import Md from '@/next/features/chat/md';
import type {
  AnswersEntry,
  CompactionEntry,
  Entry,
  NoticeEntry,
  SubagentEntry,
  ThinkEntry,
  ToolEntry,
} from '@/next/features/chat/types';
import SubsBlock from '@/next/features/subagents/subs-block';
import WorkflowCard from '@/next/features/workflows/workflow-card';
import PeerNotes from '@/next/features/chat/peer-note';
import SystemNote, { SYSTEM_NOTE } from '@/next/features/chat/system-note';
import { parsePeerOut } from '@/next/features/chat/peer';
import { PeerOutStep } from '@/next/features/chat/peer-card';
import { parseAskCall } from '@/next/features/asks/parse';
import { AskStep } from '@/next/features/asks/ask-step';
import Icon, { type TIconName } from '@/next/ui/icon';
import { useSeen } from '@/next/ui/presence';
import { plural, t } from '@/next/i18n';

/** How a tool reads in the feed: an icon and a verb, by the tool's name in the agent (Claude and Codex). */
const TOOL_KINDS = [
  // Delegating a chat and following it up (the runtime's MCP tools): before «read», which would take read_conversation.
  {
    re: /^(delegate_task|read_conversation|send_to_conversation|wait_conversation|list_children)$/,
    icon: 'workflow',
    key: 'next.tool.delegated',
  },
  { re: /^(bash|shell|exec|local_shell|run)/i, icon: 'terminal', key: 'next.tool.run' },
  { re: /^(read|view|cat|notebookread)/i, icon: 'eye', key: 'next.tool.read' },
  {
    re: /^(edit|write|multiedit|apply_patch|notebookedit|str_replace)/i,
    icon: 'file-pen',
    key: 'next.tool.edit',
  },
  { re: /^(grep|glob|search|ls|find)/i, icon: 'search', key: 'next.tool.search' },
  { re: /^(web|fetch)/i, icon: 'globe', key: 'next.tool.web' },
  { re: /^(task|agent)/i, icon: 'workflow', key: 'next.tool.agent' },
  { re: /^(todo|plan|update_plan)/i, icon: 'list-checks', key: 'next.tool.plan' },
] as const satisfies readonly { re: RegExp; icon: TIconName; key: string }[];

export const toolView = (tool: string): { icon: TIconName; label: string } => {
  const kind = TOOL_KINDS.find((item) => item.re.test(tool.replace(/^mcp__\w+__/, '')));
  if (kind) return { icon: kind.icon, label: t(kind.key) };
  return { icon: 'square-terminal', label: tool.replace(/^mcp__(\w+)__/, '$1 · ') };
};

const ToolStep = memo(({ entry }: { entry: ToolEntry }) => {
  const [open, setOpen] = useState(false);
  const seen = useSeen(open);
  const { icon, label } = toolView(entry.tool);
  const has = Boolean(entry.output?.text);
  let meta = '';
  if (entry.ok === null) meta = t('next.tool.running');
  else if (entry.ok === false) meta = t('next.tool.failed');
  return (
    <div
      className="hx-tool"
      data-ok={entry.ok === false ? 'false' : undefined}
      data-has={has || undefined}
      aria-expanded={open}
    >
      <button
        type="button"
        className="hx-tool__head"
        disabled={!has}
        onClick={() => setOpen((value) => !value)}
      >
        {entry.ok === null ? (
          <span className="hx-tool__spin" aria-hidden="true" />
        ) : (
          <Icon name={icon} size="sm" className="hx-tool__ico" />
        )}
        <span className="hx-tool__label">{label}</span>
        <code className="hx-tool__detail">{entry.summary}</code>
        <span className="hx-spacer" />
        {meta && <span className="hx-tool__meta">{meta}</span>}
      </button>
      {has && (
        <div className="hx-tool__body">
          <div>
            {seen && (
              <div className="hx-tool__out">
                {entry.output?.text.split('\n').map((line, index) => (
                  <div key={index} className="hx-cmd__ln">
                    {line}
                  </div>
                ))}
                {entry.output?.truncated && (
                  <div className="hx-cmd__ln" data-tone="dim">
                    {t('next.tool.truncated', {
                      shown: entry.output.truncated.shown,
                      total: entry.output.truncated.total,
                    })}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
});

const ToolGroup = ({ tools }: { tools: ToolEntry[] }) => {
  const [open, setOpen] = useState(false);
  const seen = useSeen(open);
  const icons = [...new Set(tools.map((tool) => toolView(tool.tool).icon))].slice(0, 3);
  const failed = tools.filter((tool) => tool.ok === false).length;
  return (
    <div className="hx-tools" aria-expanded={open}>
      <button type="button" className="hx-tools__head" onClick={() => setOpen((value) => !value)}>
        <span className="hx-tools__icons">
          {icons.map((icon) => (
            <Icon key={icon} name={icon} size="sm" />
          ))}
        </span>
        <span>
          {plural(tools.length, 'next.work.actions')}
          {failed > 0 && ` · ${plural(failed, 'next.work.failedActions')}`}
        </span>
        <Icon name="chevron-right" size="sm" className="hx-tools__chev" />
      </button>
      <div className="hx-tools__body">
        <div className="hx-tools__list">
          {seen && tools.map((tool) => <ToolStep key={tool.id} entry={tool} />)}
        </div>
      </div>
    </div>
  );
};

const Think = ({ entry }: { entry: ThinkEntry }) => (
  <div className="hx-think">
    <Icon name="lightbulb" size="sm" />
    <span>{entry.text}</span>
  </div>
);

export const Compaction = ({ entry, live = false }: { entry: CompactionEntry; live?: boolean }) => {
  const [open, setOpen] = useState(false);
  const before = entry.tokensBefore === null ? null : Math.round(entry.tokensBefore / 1000);
  const after = entry.tokensAfter === null ? null : Math.round(entry.tokensAfter / 1000);
  let meta = '';
  if (before !== null && after !== null) meta = t('next.compaction.meta', { before, after });
  return (
    <div className="hx-compact" data-live={String(live)} aria-expanded={open}>
      <button
        type="button"
        className="hx-compact__line"
        disabled={live || entry.keep.length === 0}
        onClick={() => setOpen((value) => !value)}
      >
        <span className="hx-compact__rule" />
        <span className="hx-compact__pill">
          <Icon name="fold-vertical" size="sm" />
          <span className="hx-compact__label">
            {live ? t('next.compaction.live') : t('next.compaction.done')}
          </span>
          {meta && <span className="hx-compact__meta">{meta}</span>}
          {!live && entry.keep.length > 0 && (
            <Icon name="chevron-down" size="sm" className="hx-compact__chev" />
          )}
        </span>
        <span className="hx-compact__rule" />
      </button>
      <div className="hx-compact__body">
        <div className="hx-compact__clip">
          <div className="hx-compact__card">
            <div className="hx-compact__title">{t('next.compaction.keepTitle')}</div>
            {entry.keep.map((item, index) => (
              <div key={index} className="hx-compact__item">
                <Icon name="check" size="sm" />
                <span>{item}</span>
              </div>
            ))}
            <div className="hx-compact__foot">{t('next.compaction.foot')}</div>
          </div>
        </div>
      </div>
    </div>
  );
};

const NOTICE_ICONS: Record<NoticeEntry['level'], TIconName> = {
  info: 'info',
  warn: 'triangle-alert',
  error: 'circle-alert',
};

const Notice = ({ entry }: { entry: NoticeEntry }) => (
  <div className="hx-notice" data-level={entry.level}>
    <Icon name={NOTICE_ICONS[entry.level]} size="sm" />
    <span>{entry.detail ?? entry.code}</span>
  </div>
);

/** The answered question: a card of stacked question → answer pairs; a multi-choice answer is one line per choice. */
export const Answers = ({ entry, via }: { entry: AnswersEntry; via?: string | null }) => (
  <div className="hx-msg" data-kind="answers">
    <div className="hx-qa">
      <div className="hx-qa__head">
        <Icon name="message-square-text" size="sm" />
        {plural(entry.pairs.length, 'next.answers.title')}
        {via && (
          <span className="hx-qa__via">
            <Icon name="laptop" size="sm" />
            {t('next.answers.via', { device: via })}
          </span>
        )}
      </div>
      {entry.pairs.map((pair, index) => (
        <div key={index} className="hx-qa__pair">
          <div className="hx-qa__q">{pair.q}</div>
          {pair.a
            .split('\n')
            .filter((line) => line.trim() !== '')
            .map((line, lineIndex) => (
              <div key={lineIndex} className="hx-qa__a">
                <Icon name="check" size="sm" />
                <span>{line}</span>
              </div>
            ))}
        </div>
      ))}
    </div>
  </div>
);

/** Three or more tools in a row read as one line; a live turn keeps its last run open to show progress. */
const GROUP_FROM = 3;

type TStepNode =
  | { kind: 'entry'; entry: Entry }
  | { kind: 'tools'; tools: ToolEntry[] }
  | { kind: 'subs'; agents: SubagentEntry[] }
  | { kind: 'workflow'; entry: ToolEntry };

/** Claude's Workflow call: a dynamic workflow, drawn as its own card and never folded with the tools. */
export const isWorkflowCall = (entry: Entry): entry is ToolEntry & { tool: 'Workflow' } =>
  entry.t === 'tool' && entry.tool === 'Workflow';

/** Our agent's message to another agent, read off its tool step; null for any other step. */
const peerOutOf = (entry: ToolEntry) =>
  parsePeerOut(entry.tool, entry.summary, entry.ok, entry.output?.text ?? null);

/** A question of the agent to a person (`harnessly ask`): its card, not a line among the tools. */
const askCallOf = (entry: ToolEntry) => parseAskCall(entry.tool, entry.summary, entry.output?.text ?? null);

/**
 * One tool call is one step, however many entries carry its id (a transcript that logs the call again): the step stays
 * where the call first stood and takes what the later entry knows (the result), so no copy keeps spinning.
 */
export const dedupeTools = (steps: Entry[]): Entry[] => {
  const first = new Map<string, number>();
  const out: Entry[] = [];
  for (const entry of steps) {
    if (entry.t !== 'tool' || entry.toolUseId === '') {
      out.push(entry);
      continue;
    }
    const at = first.get(entry.toolUseId);
    const kept = at === undefined ? undefined : out[at];
    if (at === undefined || kept?.t !== 'tool') {
      first.set(entry.toolUseId, out.length);
      out.push(entry);
      continue;
    }
    out[at] = {
      ...kept,
      ok: entry.ok ?? kept.ok,
      output: entry.output ?? kept.output,
      summary: kept.summary || entry.summary,
    };
  }
  return out;
};

export const groupSteps = (rawSteps: Entry[], live: boolean): TStepNode[] => {
  const steps = dedupeTools(rawSteps);
  const out: TStepNode[] = [];
  let run: ToolEntry[] = [];
  const flush = (last: boolean) => {
    if (run.length >= GROUP_FROM && !(live && last)) out.push({ kind: 'tools', tools: run });
    else run.forEach((entry) => out.push({ kind: 'entry', entry }));
    run = [];
  };
  steps.forEach((entry) => {
    if (isWorkflowCall(entry)) {
      flush(false);
      out.push({ kind: 'workflow', entry });
      return;
    }
    // A message to another agent stays in view: it is not folded into «K actions».
    if (entry.t === 'tool' && !peerOutOf(entry) && !askCallOf(entry)) {
      run.push(entry);
      return;
    }
    flush(false);
    if (entry.t !== 'subagent') {
      out.push({ kind: 'entry', entry });
      return;
    }
    // Subagents that follow each other are one block.
    const last = out.at(-1);
    if (last?.kind === 'subs') last.agents.push(entry);
    else out.push({ kind: 'subs', agents: [entry] });
  });
  flush(true);
  return out;
};

interface IStepsProps {
  steps: Entry[];
  live: boolean;
  renderGate?: (permissionId: string) => ReactNode;
}

/** One step of the work, by kind; answers and the messages steered into the turn are drawn by the turn (see splitAtMessages). */
const Step = ({ entry, renderGate }: { entry: Entry; renderGate?: (permissionId: string) => ReactNode }) => {
  switch (entry.t) {
    case 'say':
      return <Md md={entry.md} />;
    case 'think':
      return <Think entry={entry} />;
    case 'tool': {
      const out = peerOutOf(entry);
      if (out) return <PeerOutStep out={out} />;
      const ask = askCallOf(entry);
      return ask ? <AskStep call={ask} ran={entry.ok !== false} /> : <ToolStep entry={entry} />;
    }
    case 'compaction':
      return <Compaction entry={entry} />;
    case 'notice':
      if (entry.code === SYSTEM_NOTE) return <SystemNote entry={entry} />;
      return <Notice entry={entry} />;
    case 'answers':
      return <Answers entry={entry} />;
    case 'peer':
      // Another agent's message delivered into the running turn stands where the agent read it.
      return <PeerNotes entry={entry} />;
    case 'gate':
      return renderGate ? <>{renderGate(entry.permissionId)}</> : null;
    default:
      return null;
  }
};

const Steps = ({ steps, live, renderGate }: IStepsProps) => (
  <>
    {groupSteps(steps, live).map((node) => {
      if (node.kind === 'tools') return <ToolGroup key={node.tools[0].id} tools={node.tools} />;
      if (node.kind === 'subs') return <SubsBlock key={node.agents[0].id} agents={node.agents} />;
      if (node.kind === 'workflow') return <WorkflowCard key={node.entry.id} entry={node.entry} />;
      return <Step key={node.entry.id} entry={node.entry} renderGate={renderGate} />;
    })}
  </>
);

export default memo(Steps);
