// Shapes of what the runtime answers; the runtime itself is Go.

export type TBackgroundKind = 'shell' | 'monitor' | 'agent' | 'wakeup';

export interface IBackgroundTask {
  id: string;
  kind: TBackgroundKind;
  label: string;
  startedAt: number;
  /** Wakeups are pending only until their time. */
  until?: number;
  /** Where Claude Code writes the task output; gone or long silent means the task is over. */
  outputPath?: string;
}
