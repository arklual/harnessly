// What kind of action a tool call is, for its icon and colour, and the verb the timeline shows for it.
import { t } from '@/lib/i18n';
import type { ITimelineToolCall } from '@/types/timeline';

export type TToolKind = 'read' | 'edit' | 'create' | 'command' | 'search' | 'agent' | 'integration' | 'other';

const BY_NAME: Record<string, TToolKind> = {
  Read: 'read',
  NotebookRead: 'read',
  view_image: 'read',
  Edit: 'edit',
  MultiEdit: 'edit',
  NotebookEdit: 'edit',
  apply_patch: 'edit',
  Write: 'create',
  Bash: 'command',
  BashOutput: 'command',
  KillShell: 'command',
  shell: 'command',
  exec_command: 'command',
  local_shell: 'command',
  Grep: 'search',
  Glob: 'search',
  LS: 'search',
  WebSearch: 'search',
  WebFetch: 'search',
  web_search: 'search',
  Task: 'agent',
  Agent: 'agent',
};

export const toolKindOf = (name: string): TToolKind =>
  BY_NAME[name] ?? (name.startsWith('mcp__') ? 'integration' : 'other');

/** Dictionary keys of the verb for each kind. */
const VERB: Record<TToolKind, string> = {
  read: 'lib.toolKind.read',
  edit: 'lib.toolKind.edit',
  create: 'lib.toolKind.create',
  command: 'lib.toolKind.command',
  search: 'lib.toolKind.search',
  agent: 'lib.toolKind.agent',
  integration: 'lib.toolKind.integration',
  other: 'lib.toolKind.other',
};

export const toolVerbOf = (call: Pick<ITimelineToolCall, 'toolName'>): string =>
  t(VERB[toolKindOf(call.toolName)]);

/** `mcp__server__tool` reads as `server · tool`. */
export const toolDisplayName = (name: string): string => {
  if (!name.startsWith('mcp__')) return name;
  const [, server = '', tool = ''] = name.split('__');
  return `${server} · ${tool}`;
};
