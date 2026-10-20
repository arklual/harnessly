/** Pure parts of the machine load monitor: /proc parsers, CPU delta, level hysteresis, process kinds. */

export type TLoadLevel = 'ok' | 'warn' | 'over';
export type TProcessKind = 'dev' | 'agent' | 'service' | 'other';
export type TMetric = 'ram' | 'swap' | 'load' | 'disk';

/** Enter at `>= warn|over`, leave below `warnExit|overExit`. `swap` is the swap-in rate in pages per second, never swap used. */
export const THRESHOLDS: Record<TMetric, { warn: number; over: number; warnExit: number; overExit: number }> =
  {
    ram: { warn: 85, over: 93, warnExit: 82, overExit: 90 },
    swap: { warn: 50, over: 500, warnExit: 30, overExit: 350 },
    load: { warn: 1.5, over: 3, warnExit: 1.35, overExit: 2.85 },
    disk: { warn: 90, over: 97, warnExit: 87, overExit: 94 },
  };

const METRICS = Object.keys(THRESHOLDS) as TMetric[];
const LEVEL_RANK: Record<TLoadLevel, number> = { ok: 0, warn: 1, over: 2 };

/** A dev server that listens but stays silent this many scans in a row counts as stuck. */
export const STUCK_SCANS = 3;
const COMMAND_MAX = 160;

export interface ICpuTicks {
  idle: number;
  total: number;
}

export const parseCpuStat = (text: string): ICpuTicks | null => {
  const line = text.split('\n').find((item) => item.startsWith('cpu '));
  if (!line) return null;
  const fields = line.trim().split(/\s+/).slice(1).map(Number);
  if (fields.length < 4 || fields.some((value) => !Number.isFinite(value))) return null;
  const [, , , idle, iowait = 0] = fields;
  // guest and guest_nice (fields 9 and 10) are already part of user and nice.
  return { idle: idle + iowait, total: fields.slice(0, 8).reduce((sum, value) => sum + value, 0) };
};

export const cpuPctFromDelta = (prev: ICpuTicks, cur: ICpuTicks): number => {
  const total = cur.total - prev.total;
  const idle = cur.idle - prev.idle;
  if (total <= 0 || idle < 0) return 0;
  return Math.min(100, Math.max(0, ((total - idle) / total) * 100));
};

export interface IMeminfo {
  totalBytes: number;
  usedPct: number;
  swapUsedBytes: number;
}

export const parseMeminfo = (text: string): IMeminfo | null => {
  const kb = (key: string): number | null => {
    const match = text.match(new RegExp(`^${key}:\\s+(\\d+)\\s*kB`, 'm'));
    return match ? Number(match[1]) : null;
  };
  const total = kb('MemTotal');
  const available = kb('MemAvailable');
  if (!total || available === null) return null;
  const swapTotal = kb('SwapTotal') ?? 0;
  const swapFree = kb('SwapFree') ?? swapTotal;
  return {
    totalBytes: total * 1024,
    usedPct: Math.min(100, Math.max(0, (1 - available / total) * 100)),
    swapUsedBytes: Math.max(0, swapTotal - swapFree) * 1024,
  };
};

export const parseLoadavg = (text: string): number | null => {
  const load = Number(text.trim().split(/\s+/)[0]);
  return text.trim() !== '' && Number.isFinite(load) ? load : null;
};

export interface IProcStat {
  pid: number;
  comm: string;
  ppid: number;
  /** utime + stime in clock ticks. */
  ticks: number;
  starttime: number;
}

/** `comm` is the text between the first `(` and the LAST `)`: it may hold spaces and parentheses. */
export const parseProcStat = (text: string): IProcStat | null => {
  const open = text.indexOf('(');
  const close = text.lastIndexOf(')');
  if (open < 0 || close < open) return null;
  const pid = Number(text.slice(0, open));
  // After comm: state(3) ppid(4) ... utime(14) stime(15) ... starttime(22).
  const rest = text
    .slice(close + 2)
    .trim()
    .split(/\s+/);
  const ppid = Number(rest[1]);
  const utime = Number(rest[11]);
  const stime = Number(rest[12]);
  const starttime = Number(rest[19]);
  if (![pid, ppid, utime, stime, starttime].every(Number.isFinite) || rest.length < 20) return null;
  return { pid, comm: text.slice(open + 1, close), ppid, ticks: utime + stime, starttime };
};

/** Resident set in bytes from VmRSS of /proc/<pid>/status (kB, so no page size is needed); null when the line is absent. */
export const parseVmRssBytes = (text: string): number | null => {
  const match = text.match(/^VmRSS:\s+(\d+)\s*kB/m);
  return match ? Number(match[1]) * 1024 : null;
};

/** Cumulative pages swapped in since boot. */
export const parseVmstatSwapIn = (text: string): number | null => {
  const match = text.match(/^pswpin\s+(\d+)/m);
  return match ? Number(match[1]) : null;
};

/** Real and effective uid from /proc/<pid>/status: a setuid program runs as the user but acts as its owner. */
export const parseProcUids = (text: string): { real: number; effective: number } | null => {
  const match = text.match(/^Uid:\s+(\d+)\s+(\d+)/m);
  return match ? { real: Number(match[1]), effective: Number(match[2]) } : null;
};

export interface IMetricSample {
  ramPct: number;
  /** Pages swapped in per second; null without a baseline, and the swap metric then keeps its previous level. */
  swapInPerSec: number | null;
  loadPerCore: number;
  diskPct: number;
}

export type TMetricLevels = Record<TMetric, TLoadLevel>;

export const initialMetricLevels = (): TMetricLevels => ({ ram: 'ok', swap: 'ok', load: 'ok', disk: 'ok' });

const metricLevel = (metric: TMetric, value: number, prev: TLoadLevel): TLoadLevel => {
  const { warn, over, warnExit, overExit } = THRESHOLDS[metric];
  if (value >= over || (prev === 'over' && value >= overExit)) return 'over';
  if (value >= warn || (prev !== 'ok' && value >= warnExit)) return 'warn';
  return 'ok';
};

export interface ILevelState {
  level: TLoadLevel;
  metrics: TMetricLevels;
  /** The metric that drives `level`; null when ok. */
  reason: TMetric | null;
}

/** Each metric keeps its own hysteresis, so one stuck metric never drags another across its threshold. */
export const classifyLevel = (sample: IMetricSample, prev: TMetricLevels): ILevelState => {
  const values: Record<TMetric, number | null> = {
    ram: sample.ramPct,
    swap: sample.swapInPerSec,
    load: sample.loadPerCore,
    disk: sample.diskPct,
  };
  const metrics = initialMetricLevels();
  let level: TLoadLevel = 'ok';
  let reason: TMetric | null = null;
  for (const metric of METRICS) {
    const value = values[metric];
    metrics[metric] = value === null ? prev[metric] : metricLevel(metric, value, prev[metric]);
    if (LEVEL_RANK[metrics[metric]] > LEVEL_RANK[level]) {
      level = metrics[metric];
      reason = metric;
    }
  }
  return { level, metrics, reason };
};

const SERVICE_COMMS = new Set(['netbird', 'sshd', 'sshd-session', 'tbot', 'teleport']);
const AGENT_BINARIES = new Set(['claude', 'codex']);
const SHELL_COMMS = new Set(['sh', 'bash', 'zsh', 'dash', 'fish']);
const LAUNCHERS = new Set(['node', 'nodejs', 'npx', 'npm', 'pnpm', 'pnpx', 'bun', 'bunx', 'yarn']);
const AGENT_PACKAGE =
  /@openai\/codex|@anthropic-ai\/claude-code|(^|\/)codex\.js$|(^|\/)claude(-code)?\/cli\.js$/;

const basename = (value: string): string => value.slice(value.lastIndexOf('/') + 1);

/** tmux server and clients (`tmux: client` is the comm of an attach), ssh sessions and access agents, systemd and dbus units. */
const isServiceComm = (comm: string): boolean =>
  SERVICE_COMMS.has(comm) || isTmuxComm(comm) || comm.startsWith('systemd') || comm.startsWith('dbus');

export const isShellComm = (comm: string): boolean => SHELL_COMMS.has(comm);

/** `claude` / `codex` as the program itself, the script behind `node`, or an npm launcher; never as a path segment of a project. */
export const isAgentProcess = (comm: string, args: string): boolean => {
  if (AGENT_BINARIES.has(comm)) return true;
  const tokens = args.split(' ');
  const [program, script] = tokens;
  // The native installer puts the binary at `<...>/claude/versions/<version>`.
  if (
    [program, script].some(
      (token) =>
        token !== undefined &&
        (AGENT_BINARIES.has(basename(token)) || /\/(claude|codex)\/versions\/[^/]+$/.test(token)),
    )
  )
    return true;
  return (
    program !== undefined &&
    LAUNCHERS.has(basename(program)) &&
    tokens.slice(1).some((token) => AGENT_PACKAGE.test(token))
  );
};

/** Where the runtime is installed: anything run from here is part of the platform, not the user's work. */
export const RELEASE_ROOT = '/opt/hl-ai/';

const SERVICE_UNIT = /^(\/system\.slice(?:\/[^/]+)*?\/[^/]+\.service)(?:\/|$)/;

/**
 * The systemd system unit of a process from /proc/<pid>/cgroup (the v2 `0::` line, or the v1 `name=systemd` hierarchy);
 * null outside `system.slice`, e.g. in user sessions (`user.slice/.../session-*.scope`, `user@.service`).
 */
export const systemServiceUnit = (cgroup: string | null): string | null => {
  if (!cgroup) return null;
  for (const line of cgroup.split('\n')) {
    const match = line.match(/^(\d+):([^:]*):(.*)$/);
    if (!match || !((match[1] === '0' && match[2] === '') || match[2].split(',').includes('name=systemd')))
      continue;
    const unit = match[3].match(SERVICE_UNIT);
    if (unit) return unit[1];
  }
  return null;
};

const programAndScript = (args: string): string[] => {
  const [program, ...rest] = args.split(' ');
  const script = rest.find((token) => token !== '' && !token.startsWith('-'));
  return [program, script].filter((token): token is string => token !== undefined && token !== '');
};

export interface IServiceInput {
  cgroup: string | null;
  /** Target of /proc/<pid>/exe; null when unreadable. */
  exe: string | null;
  args: string;
  /** Target of /proc/<pid>/cwd: the kernel resolves it, so it is already the real path. */
  cwd: string | null;
  /** The runtime's own unit: agent dev servers live in it through tmux, so it does not make a process a service. */
  runtimeUnit: string | null;
}

/**
 * Production services that run as the runtime user (the ingest agent, the gateway): a systemd system unit other than the
 * runtime's own, or a program from the release root. Systemd counts a SIGTERM death as clean and does not restart them.
 */
const isUnderReleaseRoot = (value: string): boolean =>
  value.startsWith(RELEASE_ROOT) || value === RELEASE_ROOT.slice(0, -1);

/**
 * The cwd catches a prod runtime seen from a dev instance in the same unit: its exe is /usr/bin/node and its argv is
 * rewritten to `next-server (vX)`, but it runs from `/opt/hl-ai/releases/<v>/.next/standalone`.
 */
export const isSystemService = ({ cgroup, exe, args, cwd, runtimeUnit }: IServiceInput): boolean => {
  const unit = systemServiceUnit(cgroup);
  if (unit !== null && unit !== runtimeUnit) return true;
  return [exe ?? '', cwd ?? '', ...programAndScript(args)].some(isUnderReleaseRoot);
};

const isTmuxComm = (comm: string): boolean => comm === 'tmux' || comm.startsWith('tmux:');

/**
 * Anything a system service spawned (the prod runtime's Chrome of a design check, ffmpeg of voice input) is part of it. The walk stops at a tmux
 * server: whatever runs in a pane is the user's work, even when tmux itself was started from the release root.
 */
export const hasServiceAncestor = async (
  ancestors: { pid: number; comm: string }[],
  isServicePid: (pid: number) => Promise<boolean>,
): Promise<boolean> => {
  for (const { pid, comm } of ancestors) {
    if (isTmuxComm(comm)) return false;
    if (await isServicePid(pid)) return true;
  }
  return false;
};

/** The runtime, anything above it (killing which takes it down) and anything it spawned (Chrome of a design check, ffmpeg). */
export const isRuntimeRelative = (
  pid: number,
  ancestorPids: number[],
  runtimeLineage: Set<number>,
  runtimePid: number,
): boolean => runtimeLineage.has(pid) || ancestorPids.includes(runtimePid);

export interface IKindInput {
  pid: number;
  comm: string;
  args: string;
  hasPort: boolean;
  /** The shell tmux started in a pane. */
  isPanePid: boolean;
  /** One of the nearest ancestors (own uid) is an agent. */
  hasAgentAncestor: boolean;
  /** `isRuntimeRelative`. */
  isRuntime: boolean;
  /** `isSystemService`. */
  isSystemService: boolean;
}

/** A portless child of an agent is its tool (an MCP server, a helper): stopping it breaks the agent. A dev server it started listens. */
export const classifyKind = ({
  comm,
  args,
  hasPort,
  isPanePid,
  hasAgentAncestor,
  isRuntime,
  isSystemService: systemService,
}: IKindInput): TProcessKind => {
  if (isRuntime || systemService || isServiceComm(comm)) return 'service';
  if (isPanePid || isAgentProcess(comm, args) || (hasAgentAncestor && (isShellComm(comm) || !hasPort)))
    return 'agent';
  return hasPort ? 'dev' : 'other';
};

export const isStoppableKind = (kind: TProcessKind): boolean => kind === 'dev' || kind === 'other';

export interface IStuckResult {
  misses: Map<number, number>;
  stuck: Set<number>;
}

/** Consecutive silent scans per port. Ports that are no longer listed are forgotten. */
/** `responding: null` is a port without a fresh probe result this scan: its count stays as it was. */
export const stuckAfter = (
  prev: Map<number, number>,
  ports: { port: number; responding: boolean | null }[],
): IStuckResult => {
  const misses = new Map<number, number>();
  const stuck = new Set<number>();
  for (const { port, responding } of ports) {
    const previous = prev.get(port) ?? 0;
    const count = responding === null ? previous : responding ? 0 : previous + 1;
    misses.set(port, count);
    if (count >= STUCK_SCANS) stuck.add(port);
  }
  return { misses, stuck };
};

const SECRET_WORDS = '(?:token|secret|password|passwd|api[_-]?key|key|auth(?!or))';
/** A name holding a secret word, or `pat` as a whole segment (never `path`), or a DSN-style name. */
const SECRET_NAME = `(?:[\\w-]*${SECRET_WORDS}[\\w-]*|(?:[\\w-]*[_-])?pat(?:[_-][\\w-]*)?|database_url|[\\w-]*_dsn)`;

/** Command lines may carry credentials: hide the value of anything named like a secret, then cut. */
export const redactCommand = (args: string): string =>
  args
    .replace(/(authorization[:=]\s*)(?:(?:bearer|basic|token)\s+)?[^\s"']+/gi, '$1***')
    .replace(/\b(bearer\s+)[\w.~+/=-]+/gi, '$1***')
    .replace(/([a-z][a-z0-9+.-]*:\/\/[^\s:/@]*):[^\s@/]+@/gi, '$1:***@')
    .replace(new RegExp(`(--?${SECRET_NAME})(=|\\s+)[^\\s"']+`, 'gi'), '$1$2***')
    .replace(new RegExp(`(\\b${SECRET_NAME}=)[^\\s"']+`, 'gi'), '$1***')
    .replace(/(\s-p)(?=[^\s-])[^\s"']+/g, '$1***')
    .slice(0, COMMAND_MAX);
