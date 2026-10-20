// Shapes of what the runtime answers; the runtime itself is Go.

export type TSubagentStatus = 'running' | 'done' | 'stalled';

export interface ISubagentSummary {
  id: string;
  agentType: string;
  description: string;
  toolUseId: string | null;
  background: boolean;
  status: TSubagentStatus;
  startedAt: number | null;
  updatedAt: number;
  toolUses: number;
  lastAction: string | null;
  prompt: string;
  result: string | null;
}
