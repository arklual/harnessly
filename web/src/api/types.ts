// Shapes the runtime's API answers with (only the fields this page reads).

export type PanelType = 'terminal' | 'claude-code' | 'codex-cli' | 'agent-sessions' | 'web-browser' | 'diff';
export type AgentPanel = 'claude-code' | 'codex-cli';
export type Provider = 'claude' | 'codex';

export interface Workspace {
  id: string;
  name: string;
  directories: string[];
  groupId?: string | null;
}

export interface WorkspaceList {
  workspaces: Workspace[];
  groups?: { id: string; name: string; collapsed?: boolean }[];
  activeWorkspaceId?: string;
  sidebarCollapsed?: boolean;
  sidebarWidth?: number;
}

export interface AgentState {
  providerId: Provider;
  sessionId: string | null;
  jsonlPath: string | null;
  summary: string | null;
}

export interface Tab {
  id: string;
  sessionName: string;
  name: string;
  order: number;
  cwd?: string;
  panelType?: PanelType;
  title?: string;
  lastCommand?: string;
  cliState?: CliState;
  agentState?: AgentState;
  lastUserMessage?: string;
}

export interface PaneNode {
  type: 'pane';
  id: string;
  tabs: Tab[];
  activeTabId: string | null;
}

export interface SplitNode {
  type: 'split';
  orientation: 'horizontal' | 'vertical';
  /** Percent of the first child. */
  ratio: number;
  children: [LayoutNode, LayoutNode];
}

export type LayoutNode = PaneNode | SplitNode;

export interface Layout {
  root: LayoutNode;
  activePaneId: string;
  updatedAt?: string;
}

export type CliState =
  'inactive' | 'idle' | 'busy' | 'needs-input' | 'ready-for-review' | 'cancelled' | 'unknown';

export interface StatusEntry {
  cliState: CliState | null;
  workspaceId: string;
  tabName: string;
  currentProcess?: string;
  paneTitle?: string | null;
  panelType?: PanelType | null;
  terminalStatus?: 'idle' | 'running' | 'server';
  listeningPorts?: number[];
  agentProviderId?: Provider | null;
  agentSummary?: string | null;
  lastUserMessage?: string | null;
  lastAssistantMessage?: string | null;
  currentAction?: { toolName: string | null; summary: string } | null;
  readyForReviewAt?: number | null;
  busySince?: number | null;
  dismissedAt?: number | null;
  agentSessionId?: string | null;
  compactingSince?: number | null;
  lastEvent?: { name: string; at: number; seq: number } | null;
  eventSeq?: number;
  launchingSince?: number | null;
  launchFailed?: boolean;
  hooksMissing?: boolean;
}

export interface Conversation {
  id: string;
  title?: string;
  provider: Provider;
  workspaceId: string;
  workspaceName?: string;
}

export interface MachineProcess {
  pid: number;
  starttime: number;
  command: string;
  cwd?: string;
  cpuPct: number;
  rssBytes: number;
  port?: number;
  stuck?: boolean;
  kind: 'agent' | 'dev' | 'service' | 'other';
  stoppable: boolean;
}

export interface MachineLoad {
  at: number;
  cpu: { pct: number; cores: number; load1: number };
  mem: { usedPct: number; swapUsedBytes: number; swapInPerSec: number | null };
  disk: { usedPct: number; path: string };
  level: 'ok' | 'warn' | 'over';
  reason: 'load' | 'ram' | 'swap' | 'disk' | null;
  top: MachineProcess[];
}

export interface LimitWindow {
  usedPct: number | null;
  resetsAt: number | null;
  forecast?: { status?: string; exhaustAt?: number | null } | null;
}

export interface ProviderLimits {
  fiveHour?: LimitWindow | null;
  sevenDay?: LimitWindow | null;
  plan?: string | null;
  error?: string | null;
  updatedAt?: number;
}

export interface MachineSnapshot {
  id: string;
  name: string;
  host: string;
  user: string;
  cpuPct: number;
  memUsedPct: number;
  memTotalGb: number;
  uptimeSec: number;
  limits: { claude: ProviderLimits | null; codex: ProviderLimits | null };
  release?: { version: string; commit: string | null } | null;
}
