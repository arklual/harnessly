import { plural as pluralOf, t } from '@/lib/i18n';
import type { ITimelineToolCall } from '@/types/timeline';

export type TToolKind =
  | 'read'
  | 'edit'
  | 'create'
  | 'command'
  | 'search'
  | 'agent'
  | 'web'
  | 'plan'
  | 'skill'
  | 'integration'
  | 'other';

export interface IToolLabel {
  kind: TToolKind;
  verb: string;
  target: string;
  detail?: string;
  mono: boolean;
}

const splitPath = (path: string) => {
  const clean = path.replace(/\s+\(\+\d+, -\d+\)$/, '');
  const parts = clean.split('/').filter(Boolean);
  return { name: parts.at(-1) ?? clean, dir: parts.slice(-3, -1).join('/') };
};

const afterFirstWord = (summary: string) => summary.replace(/^\S+\s*/, '');

/** Human wording for one tool call, from the parser's compact summary. */
export const describeToolCall = (
  call: Pick<ITimelineToolCall, 'toolName' | 'summary' | 'filePath' | 'status'>,
): IToolLabel => {
  const pending = call.status === 'pending';
  const name = call.toolName;
  const summary = call.summary ?? '';
  if (name === 'Read') {
    const { name: file, dir } = splitPath(call.filePath ?? afterFirstWord(summary));
    return {
      kind: 'read',
      verb: t(pending ? 'lib.hlToolLabel.readPending' : 'lib.hlToolLabel.readDone'),
      target: file,
      detail: dir,
      mono: true,
    };
  }
  if (name === 'Edit' || name === 'MultiEdit' || name === 'NotebookEdit') {
    const creating = /^Create /.test(summary);
    const { name: file, dir } = splitPath(call.filePath ?? afterFirstWord(summary));
    const stat = summary.match(/\(\+(\d+), -(\d+)\)$/);
    return {
      kind: creating ? 'create' : 'edit',
      verb: t(
        creating
          ? pending
            ? 'lib.hlToolLabel.createPending'
            : 'lib.hlToolLabel.createDone'
          : pending
            ? 'lib.hlToolLabel.editPending'
            : 'lib.hlToolLabel.editDone',
      ),
      target: file,
      detail: stat ? `+${stat[1]} −${stat[2]}` : dir,
      mono: true,
    };
  }
  if (name === 'Write') {
    const { name: file, dir } = splitPath(call.filePath ?? afterFirstWord(summary));
    return {
      kind: 'create',
      verb: t(pending ? 'lib.hlToolLabel.writePending' : 'lib.hlToolLabel.writeDone'),
      target: file,
      detail: dir,
      mono: true,
    };
  }
  if (name === 'Bash')
    return {
      kind: 'command',
      verb: t(pending ? 'lib.hlToolLabel.runPending' : 'lib.hlToolLabel.runDone'),
      target: summary.replace(/^\$\s*/, ''),
      mono: true,
    };
  if (name === 'Grep')
    return {
      kind: 'search',
      verb: t(pending ? 'lib.hlToolLabel.grepPending' : 'lib.hlToolLabel.grepDone'),
      target: afterFirstWord(summary).replace(/^"|"$/g, ''),
      mono: true,
    };
  if (name === 'Glob')
    return {
      kind: 'search',
      verb: t(pending ? 'lib.hlToolLabel.globPending' : 'lib.hlToolLabel.globDone'),
      target: afterFirstWord(summary).replace(/^"|"$/g, ''),
      mono: true,
    };
  if (name === 'Agent' || name === 'Task')
    return {
      kind: 'agent',
      verb: t(pending ? 'lib.hlToolLabel.agentPending' : 'lib.hlToolLabel.agentDone'),
      target: afterFirstWord(summary),
      mono: false,
    };
  if (name === 'WebSearch')
    return {
      kind: 'web',
      verb: t(pending ? 'lib.hlToolLabel.webSearchPending' : 'lib.hlToolLabel.webSearchDone'),
      target: afterFirstWord(summary),
      mono: false,
    };
  if (name === 'WebFetch')
    return {
      kind: 'web',
      verb: t(pending ? 'lib.hlToolLabel.webFetchPending' : 'lib.hlToolLabel.webFetchDone'),
      target: afterFirstWord(summary),
      mono: false,
    };
  if (name === 'TodoWrite' || name === 'TaskCreate' || name === 'TaskUpdate')
    return { kind: 'plan', verb: t('lib.hlToolLabel.planUpdated'), target: '', mono: false };
  if (name === 'Skill')
    return {
      kind: 'skill',
      verb: t('lib.hlToolLabel.skillLoaded'),
      target: afterFirstWord(summary),
      mono: false,
    };
  if (name === 'ToolSearch')
    return { kind: 'skill', verb: t('lib.hlToolLabel.toolsLoaded'), target: '', mono: false };
  if (name.startsWith('mcp__')) {
    const [, server = '', tool = ''] = name.split('__');
    return {
      kind: 'integration',
      verb: server.replace(/^plugin_[^_]+_/, ''),
      target: tool.replace(/_/g, ' '),
      mono: false,
    };
  }
  return { kind: 'other', verb: name, target: afterFirstWord(summary), mono: false };
};

/** One-line summary of a burst of tool calls: "Read 3 files · 2 commands". */
export const summarizeToolGroup = (
  calls: Pick<ITimelineToolCall, 'toolName' | 'summary' | 'filePath' | 'status'>[],
) => {
  const counts = new Map<TToolKind, number>();
  const edited = new Set<string>();
  const restVerbs = new Set<string>();
  for (const call of calls) {
    const label = describeToolCall(call);
    counts.set(label.kind, (counts.get(label.kind) ?? 0) + 1);
    if (label.kind === 'edit' || label.kind === 'create') edited.add(label.target);
    if (label.kind === 'plan' || label.kind === 'skill')
      restVerbs.add(label.verb.charAt(0).toLowerCase() + label.verb.slice(1));
    if (label.kind === 'other') restVerbs.add(label.verb);
  }
  const parts: string[] = [];
  const n = (k: TToolKind) => counts.get(k) ?? 0;
  if (n('read')) parts.push(pluralOf(n('read'), 'lib.hlToolLabel.filesRead'));
  if (edited.size)
    parts.push(
      edited.size === 1
        ? t('lib.hlToolLabel.editIn', { file: [...edited][0] })
        : pluralOf(edited.size, 'lib.hlToolLabel.editsInFiles'),
    );
  if (n('command')) parts.push(pluralOf(n('command'), 'lib.hlToolLabel.commands'));
  if (n('search')) parts.push(pluralOf(n('search'), 'lib.hlToolLabel.searches'));
  if (n('agent')) parts.push(pluralOf(n('agent'), 'lib.hlToolLabel.subagents'));
  if (n('web')) parts.push(t('lib.hlToolLabel.web'));
  if (n('integration')) parts.push(pluralOf(n('integration'), 'lib.hlToolLabel.integrations'));
  const rest = n('plan') + n('skill') + n('other');
  // The group header already shows the total, so a group of only these names what was done instead of "N more".
  if (rest && !parts.length) {
    const verbs = [...restVerbs];
    const shown = verbs.slice(0, 2).join(' · ');
    parts.push(verbs.length > 2 ? t('lib.hlToolLabel.verbsAndMore', { verbs: shown }) : shown);
  } else if (rest) parts.push(t('lib.hlToolLabel.more', { n: rest }));
  const text = parts.join(' · ');
  return text.charAt(0).toUpperCase() + text.slice(1);
};
