// Workspaces, panes and tabs as the runtime keeps them (GET /api/workspace, /api/layout).
import type { TCliState } from '@/types/timeline';

export type TPanelType = 'terminal' | 'claude-code' | 'codex-cli' | 'agent-sessions' | 'web-browser' | 'diff';
export type TAgentProvider = 'claude' | 'codex';

export interface IWorkspace {
  id: string;
  name: string;
  directories: string[];
  groupId?: string | null;
}

export interface IWorkspaceGroup {
  id: string;
  name: string;
  collapsed?: boolean;
}

export interface IAgentState {
  providerId: TAgentProvider;
  sessionId: string | null;
  jsonlPath: string | null;
  summary: string | null;
}

export interface ITab {
  id: string;
  sessionName: string;
  name: string;
  order: number;
  cwd?: string;
  panelType?: TPanelType;
  title?: string;
  lastCommand?: string;
  cliState?: TCliState;
  agentState?: IAgentState;
  lastUserMessage?: string;
  webUrl?: string;
}

export interface IPaneNode {
  type: 'pane';
  id: string;
  tabs: ITab[];
  activeTabId: string | null;
}

export interface ISplitNode {
  type: 'split';
  orientation: 'horizontal' | 'vertical';
  /** Percent of the first child. */
  ratio: number;
  children: [TLayoutNode, TLayoutNode];
}

export type TLayoutNode = IPaneNode | ISplitNode;

export interface ILayoutData {
  root: TLayoutNode;
  activePaneId: string;
  updatedAt?: string;
}
