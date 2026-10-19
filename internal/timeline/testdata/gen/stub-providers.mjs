// The two providers as the previous runtime's registry builds them, on its real session detection; the resume line
// names the session and the workspace (the launch line itself is tested with the launch code).
import path from 'node:path';
import { detectActiveSession as detectClaude, isClaudeRunning, watchSessionsDir as watchClaude } from '@/lib/providers/claude/session-detection';
import { detectActiveSession as detectCodex, isCodexRunning, watchSessionsDir as watchCodex } from '@/lib/providers/codex/session-detection';

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const CLAUDE_TITLE_RE = /^[✳⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⠐⠈]\s+/;

const claude = {
  id: 'claude',
  displayName: 'Claude Code',
  panelType: 'claude-code',
  isValidSessionId: (id) => typeof id === 'string' && UUID_RE.test(id),
  detectActiveSession: (pid, kids) => detectClaude(pid, kids),
  isAgentRunning: (pid, kids) => isClaudeRunning(pid, kids),
  watchSessions: (pid, onChange, options) => watchClaude(pid, onChange, options),
  buildResumeCommand: async (sessionId, { workspaceId }) => `claude --resume ${sessionId}${workspaceId ? ` --workspace ${workspaceId}` : ''}`,
  parsePaneTitle: (title) => {
    if (!title || !CLAUDE_TITLE_RE.test(title)) return null;
    return title.replace(CLAUDE_TITLE_RE, '').trim() || null;
  },
  sessionIdFromJsonlPath: (p) => (p ? path.basename(p, '.jsonl') : null),
};

const codex = {
  id: 'codex',
  displayName: 'Codex',
  panelType: 'codex-cli',
  isValidSessionId: (id) => typeof id === 'string' && UUID_RE.test(id),
  detectActiveSession: (pid, kids, options) => detectCodex(pid, kids, options),
  isAgentRunning: (pid, kids) => isCodexRunning(pid, kids),
  watchSessions: (pid, onChange, options) => watchCodex(pid, onChange, options),
  buildResumeCommand: async (sessionId, { workspaceId }) => `codex resume ${sessionId}${workspaceId ? ` --workspace ${workspaceId}` : ''}`,
  parsePaneTitle: () => null,
  sessionIdFromJsonlPath: (p) => {
    if (!p) return null;
    const m = path.basename(p, '.jsonl').match(/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/i);
    return m ? m[1] : null;
  },
};

export const getProviderByPanelType = (panelType) => (panelType === 'claude-code' ? claude : panelType === 'codex-cli' ? codex : null);
