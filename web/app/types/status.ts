// What the runtime's status socket says about tabs (internal/status).
import type { TCliState } from '@/types/timeline';
import type { TAgentProvider, TPanelType } from '@/types/terminal';
import type { IPermissionRequest } from '@/types/codex-permission';

export interface ICurrentAction {
  toolName: string | null;
  summary: string;
}

export interface ILastEvent {
  name: string;
  at: number;
  seq: number;
}

/** Where a subscription's usage numbers come from: the agent's own status line or codexbar. */
export type TRateLimitsSource = 'statusline' | 'codexbar' | 'oauth' | string;

/** codexbar's average pace over a window. */
export interface ICodexbarPace {
  /** How far ahead (+) or behind (−) an even pace through the window the usage is, in percent points. */
  deltaPercent: number;
  willLastToReset: boolean;
  /** Seconds until the limit runs out at the current pace; null when it lasts. */
  etaSeconds: number | null;
}

export interface IStatusEntry {
  cliState: TCliState | null;
  workspaceId: string;
  tabName: string;
  currentProcess?: string;
  paneTitle?: string | null;
  panelType?: TPanelType | null;
  terminalStatus?: 'idle' | 'running' | 'server';
  listeningPorts?: number[];
  agentProviderId?: TAgentProvider | null;
  agentSummary?: string | null;
  lastUserMessage?: string | null;
  lastAssistantMessage?: string | null;
  currentAction?: ICurrentAction | null;
  readyForReviewAt?: number | null;
  busySince?: number | null;
  dismissedAt?: number | null;
  agentSessionId?: string | null;
  compactingSince?: number | null;
  lastEvent?: ILastEvent | null;
  eventSeq?: number;
  launchingSince?: number | null;
  launchFailed?: boolean;
  hooksMissing?: boolean;
  /** False once the runtime saw the agent's process exit. */
  agentProcess?: boolean;
  permissionRequest?: IPermissionRequest | null;
}

export type TStatusServerMessage =
  | { type: 'status:sync'; tabs: Record<string, IStatusEntry> }
  | ({ type: 'status:update'; tabId: string } & IStatusEntry)
  | { type: 'status:hook-event'; tabId: string; event: ILastEvent }
  | { type: 'rate-limits:update'; data: unknown }
  | { type: 'pane-intents'; tabId: string; intents: unknown[] };
