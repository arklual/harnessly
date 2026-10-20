// Shapes of what the runtime answers; the runtime itself is Go.

export type TProvider = 'claude' | 'codex';

/** [key, tsSec, model, project, input, output, cacheRead, cacheWrite5m, cacheWrite1h] */
export type TMessageRow = [string, number, number, number, number, number, number, number, number];

/** [key, tsSec, project] */
export type TPromptRow = [string, number, number];

/** [key, tsSec, durationMs] */
export type TTurnRow = [string, number, number];

/** [key, tsSec, isError 0|1] */
export type TToolRow = [string, number, number];

/** [key, tsSec] */
export type TEventRow = [string, number];

export interface IFileFacts {
  provider: TProvider;
  sessionId: string;
  /** Subagent transcripts count toward their parent session, never as prompts. */
  sidechain: boolean;
  title: string | null;
  firstPrompt: string | null;
  models: string[];
  projects: string[];
  tasks: string[];
  prs: string[];
  firstTs: number;
  lastTs: number;
  messages: TMessageRow[];
  prompts: TPromptRow[];
  turns: TTurnRow[];
  tools: TToolRow[];
  interrupts: TEventRow[];
  denials: TEventRow[];
  compactions: TEventRow[];
}
