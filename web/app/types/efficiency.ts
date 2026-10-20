/**
 * The Efficiency section: what the cluster answers under /agents/api/platform/efficiency/*.
 * Every number is computed by the ingest efficiency scanner from transcripts in Postgres; the
 * person's own figures are answered only to that person, team figures are aggregates.
 */

export type TEffScope = 'me' | 'team';

export type TEffMarkerKind = 'model' | 'claude' | 'plugins' | 'memory' | 'rule' | 'eval';

export interface IEffMarker {
  at: string;
  kind: TEffMarkerKind;
  title: string;
  detail: string | null;
}

/** One calendar day (Moscow time). A metric that has no data for the day is null, never 0. */
export interface IEffDay {
  day: string;
  costUsd: number;
  sessions: number;
  modelCalls: number;
  mrs: number;
  avgCtx: number | null;
  firstCtxP50: number | null;
  carryShare: number | null;
  /** Failed tool calls, without guard refusals, Claude Code's blocks and red tests, lint and builds. */
  toolErrorRate: number | null;
  /** Calls a hook refused or Claude Code blocked. */
  guardStopRate: number | null;
  /** Tests, lint and builds that ran and failed. */
  redGateRate: number | null;
  subagentSonnetShare: number | null;
  gateVisibleShare: number | null;
  humanReplyP90Min: number | null;
  sleepHours: number;
  ciPollsPerMr: number | null;
}

export interface IEffChannel {
  channel: 'remote-control' | 'tab' | 'sdk' | 'eval' | 'automation';
  sessions: number;
  costUsd: number;
  mrs: number;
}

export interface IEffOverview {
  scope: TEffScope;
  from: string;
  to: string;
  days: IEffDay[];
  markers: IEffMarker[];
  channels: IEffChannel[];
  totals: {
    costUsd: number;
    sessions: number;
    mrs: number;
    carryTokens: number;
    resultTokens: number;
    readingShare: number | null;
  };
  /** When the scanner last finished a pass; the screen says how fresh the numbers are. */
  scannedAt: string | null;
}

export interface IEffFamily {
  id: string;
  name: string;
  desc: string;
  calls: number;
  errors: number;
  errRate: number;
  resultTokens: number;
  carryTokens: number;
  carryShare: number;
  sessions: number;
  /** Skill bodies loaded (only for families that hold skills). */
  loads: number;
  perDay: number[];
  hyps: string[];
}

export interface IEffVerb {
  family: string;
  verb: string;
  calls: number;
  errors: number;
  retries: number;
  resultTokens: number;
  carryTokens: number;
  latAvgMs: number | null;
  slowShare: number | null;
  sessions: number;
}

export interface IEffSkill {
  skill: string;
  loads: number;
  bodyTokens: number;
  carryTokens: number;
  /** What the agent did in the 30 calls after the load: [«family · verb», calls]. */
  followups: [string, number][];
  /** [verb, calls after the load, error rate after, error rate of the same verb elsewhere]. */
  errVsBase: [string, number, number, number | null][];
}

export interface IEffListedSkill {
  skill: string;
  plugin: string | null;
  /** Sessions whose startup listing held the skill. */
  sessionsShown: number;
  /** Loads of a skill with this name (the plugin prefix is not kept in loads, so same-named skills of two plugins share them). */
  loads: number;
  lastLoadedDay: string | null;
  /** The latest day a session was shown the skill: a plugin switched off stops appearing. */
  lastShownDay: string;
  avgListingChars: number;
  descriptionShare: number;
}

export interface IEffSkillInventory {
  skills: IEffListedSkill[];
  totals: {
    listed: number;
    loaded: number;
    neverLoadedShare: number;
    /** Listing characters per session spent on skills nobody loaded in the period. */
    neverLoadedCharsPerSession: number;
    sessions: number;
  };
}

export interface IEffPanelRow {
  label: string;
  value: number;
  text: string;
  tone: 'bad' | 'warn' | 'ok' | null;
}

export interface IEffPanel {
  title: string;
  rows: IEffPanelRow[];
  note: string | null;
}

export interface IEffComponents {
  scope: TEffScope;
  from: string;
  to: string;
  days: string[];
  totalCarry: number;
  families: IEffFamily[];
}

export interface IEffFamilyDetail {
  family: IEffFamily;
  days: string[];
  verbs: IEffVerb[];
  skills: IEffSkill[];
  panels: IEffPanel[];
}

export interface IEffSession {
  fileId: string;
  sessionUuid: string | null;
  title: string | null;
  channel: IEffChannel['channel'];
  startedAt: string | null;
  endedAt: string | null;
  costUsd: number;
  mrs: number;
  modelCalls: number;
  peakCtx: number | null;
  avgCtx: number | null;
  /** Context of each model call, sampled to at most 160 points. */
  cardio: number[];
  /** Share of re-read tokens above 250K: what handing the work to a fresh session would have saved (hypothesis H1). */
  over250kShare: number | null;
}

export interface IEffMe {
  person: string;
  from: string;
  to: string;
  attention: {
    replyP50Min: number | null;
    replyP90Min: number | null;
    autonomyP90Min: number | null;
    askWaitP90Min: number | null;
    parallelP90: number | null;
    questions: number;
    interrupts: number;
  };
  limit: { weekUsedPct: number | null; evalWeekPct: number; resetsAt: string | null };
  sessions: IEffSession[];
}

export interface IEffHypothesis {
  id: string;
  num: string;
  group: string;
  name: string;
  metric: string;
  unit: string;
  better: 'up' | 'down';
  base: number | null;
  target: number | null;
  current: number | null;
  state: string;
  exp: string | null;
}

export type TEvalRunState =
  | 'queued'
  | 'claimed'
  | 'preparing'
  | 'running'
  | 'grading'
  | 'done'
  | 'failed'
  | 'timeout'
  | 'stopped'
  | 'skipped';

export interface IEvalSchedule {
  id: string;
  suite: string;
  title: string;
  when: string;
  runsPerWeek: number;
  weekPct: number;
  enabled: boolean;
  nextAt: string | null;
}

export interface IEvalRunRow {
  id: string;
  task: string;
  arm: string;
  runNo: number;
  state: TEvalRunState;
  passed: boolean | null;
  costUsd: number | null;
  wallS: number | null;
  firstCtx: number | null;
  peakCtx: number | null;
  checks: { name: string; passed: boolean; exitCode: number | null; ms: number | null }[];
  error: string | null;
}

export interface IEvalBatch {
  id: string;
  suite: string;
  reason: string;
  createdAt: string;
  state: string;
  arms: string[];
  passed: number;
  total: number;
  costUsd: number;
  wallMin: number | null;
  runs: IEvalRunRow[];
}

export interface IEvalAlert {
  id: string;
  createdAt: string;
  metric: string;
  baseline: number | null;
  observed: number | null;
  band: number | null;
  diff: string[];
  state: string;
  addressee: string | null;
  mine: boolean;
}

export interface IEvalCompareArm {
  arm: string;
  fingerprint: string;
  runs: number;
  passRate: number | null;
  usdPerTask: number | null;
  firstCtx: number | null;
}

/** One arm of the arms suite against its baseline arm, on the tasks both finished. */
export interface IEvalArmRow {
  arm: string;
  title: string;
  base: string | null;
  hyp: string | null;
  tasks: number;
  runs: number;
  /** Share of tasks that passed in every run. */
  pass3: number | null;
  passRate: number | null;
  usdPerTask: number | null;
  tokensPerTask: number | null;
  wallS: number | null;
  deltaUsdPct: number | null;
  deltaTokensPct: number | null;
  deltaWallPct: number | null;
  deltaPass3Pp: number | null;
}

/** Hypothesis H8: each dangerous scenario held when it was stopped in every run of the latest batch. */
export interface IEvalDanger {
  at: string | null;
  scenarios: number;
  checked: number;
  held: number;
  rows: {
    key: string;
    scenario: number;
    title: string;
    runs: number;
    held: number;
    state: 'held' | 'broken' | 'pending';
  }[];
}

export interface IEvalRuns {
  enabled: boolean;
  weekCapPct: number;
  weekUsedPct: number;
  weekLimitPct: number | null;
  schedules: IEvalSchedule[];
  batches: IEvalBatch[];
  /** One point per smoke batch: tasks passed, cost per task. */
  smoke: { at: string; passed: number; total: number; usdPerTask: number | null; fingerprint: string }[];
  band: [number, number] | null;
  compare: IEvalCompareArm[];
  arms: { at: string | null; rows: IEvalArmRow[] };
  danger: IEvalDanger;
  alerts: IEvalAlert[];
}

/** The first screen — where we stand and what to fix first. */
export type TEffSummaryTab = 'changes' | 'components' | 'hypotheses' | 'sessions' | 'runs' | 'skills';

export interface IEffSummaryMetric {
  id: string;
  title: string;
  /** What the number means, in a sentence a person reads once. */
  what: string;
  /** How it is computed, for the curious. */
  how: string;
  unit: 'usd' | 'pct' | 'tokens' | 'min' | 'count';
  better: 'up' | 'down';
  /** Last 7 Moscow days. */
  value: number | null;
  /** The 7 days before. */
  prev: number | null;
  target: number | null;
  trendPct: number | null;
  verdict: 'good' | 'bad' | 'flat' | 'nodata';
  link: { tab: TEffSummaryTab; id?: string };
}

export interface IEffSummaryFix {
  id: string;
  title: string;
  evidence: string;
  action: string;
  /** How far from the target, relative; the list is sorted by it. */
  gap: number;
  link: { tab: TEffSummaryTab; id?: string };
}

export interface IEffSummaryChange {
  at: string;
  kind: string;
  title: string;
  effects: {
    title: string;
    unit: IEffSummaryMetric['unit'];
    before: number | null;
    after: number | null;
    deltaPct: number | null;
    verdict: 'better' | 'worse' | 'noise' | 'early';
  }[];
}

export interface IEffSkillTotals {
  listed: number;
  loaded: number;
  /** Listing characters per session spent on skills nobody loaded. */
  deadCharsPerSession: number | null;
}

export interface IEffSummary {
  scope: TEffScope;
  from: string;
  to: string;
  totals: {
    costUsd: number;
    sessions: number;
    mrs: number;
    usdPerMr: number | null;
    readingShare: number | null;
  };
  metrics: IEffSummaryMetric[];
  fixes: IEffSummaryFix[];
  changes: IEffSummaryChange[];
  scannedAt: string | null;
}

/** Hypothesis H19: a person's own weekly pulse; nobody else sees the answers. */
export interface IEffPulse {
  /** Monday of the current Moscow week. */
  week: string;
  answered: boolean;
  answers: { week: string; speed: number; quality: number; friction: number }[];
}
