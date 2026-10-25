// What a permission asks, as the card's title says it: by the tool the agent wants to use, so a read of a file is not
// «Allow the command?». A tool the page does not know asks «Allow the action?».
const TITLE_KEY: Readonly<Record<string, string>> = {
  Bash: 'next.gate.permTitleCommand',
  BashOutput: 'next.gate.permTitleCommand',
  KillShell: 'next.gate.permTitleCommand',
  exec: 'next.gate.permTitleCommand',
  Read: 'next.gate.permTitleRead',
  NotebookRead: 'next.gate.permTitleRead',
  Edit: 'next.gate.permTitleEdit',
  MultiEdit: 'next.gate.permTitleEdit',
  NotebookEdit: 'next.gate.permTitleEdit',
  Write: 'next.gate.permTitleWrite',
  'apply-patch': 'next.gate.permTitlePatch',
  Glob: 'next.gate.permTitleFind',
  Grep: 'next.gate.permTitleFind',
  WebFetch: 'next.gate.permTitleWeb',
  WebSearch: 'next.gate.permTitleWebSearch',
  Task: 'next.gate.permTitleAgent',
  Agent: 'next.gate.permTitleAgent',
};

const MCP_PREFIX = 'mcp__';

/** The key of the text a permission card is titled with; a permission with no tool named but a command is a command. */
export const permTitleKey = (tool: string | null, command: string | null = null): string => {
  if (tool === null) return command ? 'next.gate.permTitleCommand' : 'next.gate.permTitle';
  if (tool.startsWith(MCP_PREFIX)) return 'next.gate.permTitleMcp';
  return TITLE_KEY[tool] ?? 'next.gate.permTitle';
};
