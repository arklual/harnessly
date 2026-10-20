// Shapes of what the runtime answers; the runtime itself is Go.
import type { TLoadLevel, TMetric, TProcessKind } from '@/lib/hl-machine-load';

export interface IMachineProcess {
  pid: number;
  /** Field 22 of /proc/<pid>/stat: with the pid it identifies one process for its whole life. */
  starttime: number;
  command: string;
  cwd?: string;
  /** Share of one core, like `top`. */
  cpuPct: number;
  rssBytes: number;
  port?: number;
  kind: TProcessKind;
  stoppable: boolean;
  stuck?: boolean;
}

export interface IMachineLoad {
  at: number;
  cpu: { pct: number; cores: number; load1: number };
  /** `swapUsedBytes` is information only; the level follows `swapInPerSec` (null: no baseline yet). */
  mem: { usedPct: number; swapUsedBytes: number; swapInPerSec: number | null };
  disk: { usedPct: number; path: string };
  level: TLoadLevel;
  /** The metric behind `level` (null when ok). */
  reason: TMetric | null;
  top: IMachineProcess[];
}
