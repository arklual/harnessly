import { t } from '@/lib/i18n';
import type { ICurrentAction } from '@/types/status';

/** Dictionary keys of the verb for each tool. */
const VERBS: Record<string, string> = {
  Read: 'lib.hlActionLabel.reading',
  Edit: 'lib.hlActionLabel.editing',
  MultiEdit: 'lib.hlActionLabel.editing',
  Write: 'lib.hlActionLabel.writing',
  NotebookEdit: 'lib.hlActionLabel.editing',
  Bash: 'lib.hlActionLabel.running',
  Grep: 'lib.hlActionLabel.searching',
  Glob: 'lib.hlActionLabel.findingFiles',
  Agent: 'lib.hlActionLabel.startingSubagent',
  Task: 'lib.hlActionLabel.startingSubagent',
  WebFetch: 'lib.hlActionLabel.openingPage',
  WebSearch: 'lib.hlActionLabel.searchingWeb',
  TodoWrite: 'lib.hlActionLabel.updatingPlan',
  Skill: 'lib.hlActionLabel.loadingSkill',
};

export const describeAction = (action: ICurrentAction | null | undefined): string | null => {
  if (!action) return null;
  const name = action.toolName ?? '';
  const key = VERBS[name] ?? (name.startsWith('mcp__') ? 'lib.hlActionLabel.callingIntegration' : null);
  const verb = key ? t(key) : null;
  const summary = action.summary.trim();
  if (!verb) return summary || null;
  return summary ? `${verb} · ${summary}` : verb;
};
