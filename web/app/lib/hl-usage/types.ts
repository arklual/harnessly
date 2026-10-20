export type TUsageProvider = 'claude' | 'codex';

/** [day, provider, model, project, input, output, cacheRead, cacheWrite, costUsd, apiCalls] */
export type TUsageRow = [
  string,
  TUsageProvider,
  string,
  string,
  number,
  number,
  number,
  number,
  number,
  number,
];

export interface IUsageDay {
  prompts: number;
  sessions: number;
  /** Time agents spent working on turns, from Claude turn_duration / Codex task timing. */
  turnMs: number;
  tools: number;
  toolErrors: number;
  interrupts: number;
  denials: number;
  compactions: number;
  prs: number;
  /** Prompts per hour of day, Moscow time. */
  hours: number[];
}

export interface IUsageSession {
  id: string;
  provider: TUsageProvider;
  /** Null when the gateway hides another person's conversation titles. */
  title: string | null;
  project: string | null;
  start: number;
  end: number;
  prompts: number;
  turnMs: number;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  subagentCost: number;
  model: string | null;
  tasks: string[];
  prs: string[];
}

export interface IUsageReport {
  version: 1;
  person: string;
  machine: string;
  generatedAt: string;
  timezone: string;
  days: Record<string, IUsageDay>;
  usage: TUsageRow[];
  sessions: IUsageSession[];
  /** Set while the first pass over the transcripts is still running. */
  collecting: { done: number; total: number } | null;
}

export interface ITeamUsageRuntime {
  person: string;
  machineId: string;
  machineName: string;
  report: IUsageReport | null;
  error?: string;
}

export interface ITeamUsage {
  viewer: string;
  runtimes: ITeamUsageRuntime[];
}
