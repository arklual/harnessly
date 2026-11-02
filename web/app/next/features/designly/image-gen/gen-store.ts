// The pictures being drawn into elements of boards. A run belongs to an element (`<artifact>|<board>|<tpl>`), not to
// the card or the toolbar that started it: closing those does not lose it, and the board keeps its veil on the element
// until the picture is written (a mutation is shown after the machine confirmed it). One picture is written as soon as it
// is drawn; with several, the person picks one and the others stay in the project (`data-gen-job` finds them again).
import { useSyncExternalStore } from 'react';
import { pageClock } from '@/next/core/clock';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import {
  JOB_STATE,
  readGenJob,
  startDesignImage,
  type IGenPic,
  type IGenRequest,
} from '@/next/features/designly/image-gen/api';

/** How often a running job is asked about. */
export const GEN_POLL_MS = 1500;

export const GEN_PHASE = { drawing: 'drawing', choose: 'choose', failed: 'failed' } as const;
export type TGenPhase = (typeof GEN_PHASE)[keyof typeof GEN_PHASE];

export interface IGenRun {
  key: string;
  artifact: string;
  tpl: number;
  prompt: string;
  count: number;
  phase: TGenPhase;
  /** The pictures drawn so far, in the order they came. */
  pics: IGenPic[];
  /** A picture is being written into the board. */
  applying: boolean;
  error: string | null;
}

/** What an element was last drawn from in this session: the board's own attributes only reach a card at the next pick. */
export interface IGenDone {
  prompt: string;
  job: string;
}

/** The machine confirmed the write: the element's fingerprint now, when it said it. */
export interface IGenApplied {
  was: string | null;
}

/** How a picture gets into the board: what the machine confirmed, or null when it refused (said already). */
export type TGenApply = (pic: IGenPic, drawn: IGenDone) => Promise<IGenApplied | null>;

/** What an element was drawn from in this session, with its fingerprint after the write. */
export interface IGenDoneNow extends IGenDone {
  was: string | null;
}

interface IState {
  runs: ReadonlyMap<string, IGenRun>;
  done: ReadonlyMap<string, IGenDoneNow>;
}

let state: IState = { runs: new Map(), done: new Map() };
const listeners = new Set<() => void>();
const applies = new Map<string, TGenApply>();
const seen = new Map<string, string>();
/** The run each key is on; a poll that finds another (or none) has been dismissed and stops. */
const tokens = new Map<string, number>();
let tokenSeq = 0;

const emit = () => {
  for (const fn of listeners) fn();
};
const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => listeners.delete(fn);
};

export const genKey = (artifact: string, board: string, tpl: number) => `${artifact}|${board}|${tpl}`;

const setRun = (run: IGenRun) => {
  state = { ...state, runs: new Map(state.runs).set(run.key, run) };
  emit();
};
const dropRun = (key: string) => {
  tokens.delete(key);
  applies.delete(key);
  const runs = new Map(state.runs);
  runs.delete(key);
  state = { ...state, runs };
  emit();
};
const patchRun = (key: string, token: number, change: Partial<IGenRun>) => {
  const run = state.runs.get(key);
  if (run && tokens.get(key) === token) setRun({ ...run, ...change });
};

const wait = (ms: number) => new Promise<void>((resolve) => pageClock().setTimeout(resolve, ms));

const failureOf = (failure: unknown) => (failure instanceof Error ? failure.message : t('next.failure.http'));

/** Writes a picture into the board; the run ends when the machine confirmed it. */
const write = async (key: string, token: number, pic: IGenPic, name: string): Promise<void> => {
  const run = state.runs.get(key);
  const apply = applies.get(key);
  if (!run || !apply || tokens.get(key) !== token || run.applying) return;
  setRun({ ...run, applying: true });
  const drawn: IGenDone = { prompt: run.prompt, job: name };
  const applied = await apply(pic, drawn);
  if (tokens.get(key) !== token) return;
  if (applied === null) {
    patchRun(key, token, { applying: false });
    return;
  }
  if (applied.was) seen.set(key, applied.was);
  state = { ...state, done: new Map(state.done).set(key, { ...drawn, was: applied.was }) };
  dropRun(key);
};

/** The names of the jobs by run: the choice writes a picture with its job's name. */
const names = new Map<string, string>();

const follow = async (machine: MachineId, key: string, token: number, jobId: string, name: string) => {
  for (;;) {
    await wait(GEN_POLL_MS);
    if (tokens.get(key) !== token) return;
    const run = state.runs.get(key);
    if (!run) return;
    let job;
    try {
      job = await readGenJob(machine, jobId, name);
    } catch (failure) {
      patchRun(key, token, { phase: GEN_PHASE.failed, error: failureOf(failure) });
      return;
    }
    if (tokens.get(key) !== token) return;
    if (run.count === 1 && job.pics.length > 0) {
      patchRun(key, token, { pics: job.pics });
      await write(key, token, job.pics[0] as IGenPic, name);
      return;
    }
    if (job.state !== JOB_STATE.running && job.pics.length === 0) {
      patchRun(key, token, { phase: GEN_PHASE.failed, error: job.error ?? t('next.imagegen.failed') });
      return;
    }
    const phase = job.pics.length > 0 ? GEN_PHASE.choose : GEN_PHASE.drawing;
    patchRun(key, token, { pics: job.pics, phase });
    if (job.state !== JOB_STATE.running) return;
  }
};

export const startGen = async (
  machine: MachineId,
  key: string,
  request: IGenRequest,
  tpl: number,
  apply: TGenApply,
): Promise<void> => {
  const token = (tokenSeq += 1);
  tokens.set(key, token);
  applies.set(key, apply);
  setRun({
    key,
    artifact: request.artifact,
    tpl,
    prompt: request.prompt.trim(),
    count: request.count,
    phase: GEN_PHASE.drawing,
    pics: [],
    applying: false,
    error: null,
  });
  try {
    const started = await startDesignImage(machine, request);
    if (tokens.get(key) !== token) return;
    names.set(key, started.name);
    await follow(machine, key, token, started.job.id, started.name);
  } catch (failure) {
    patchRun(key, token, { phase: GEN_PHASE.failed, error: failureOf(failure) });
  }
};

/**
 * The veil over an element while the editor draws a change of its picture (a minute or more): the page blurs the block as it
 * does for a picture drawn from a brief. It follows the job itself, so it ends with the job even when the editor is closed.
 */
export const veilEdit = (
  machine: MachineId,
  key: string,
  artifact: string,
  tpl: number,
  jobId: string,
  name: string,
): void => {
  const token = (tokenSeq += 1);
  tokens.set(key, token);
  setRun({
    key,
    artifact,
    tpl,
    prompt: '',
    count: 1,
    phase: GEN_PHASE.drawing,
    pics: [],
    applying: false,
    error: null,
  });
  const end = () => {
    if (tokens.get(key) === token) dropRun(key);
  };
  const watch = async () => {
    for (;;) {
      await wait(GEN_POLL_MS);
      if (tokens.get(key) !== token) return;
      try {
        const job = await readGenJob(machine, jobId, name);
        if (job.state !== JOB_STATE.running) return end();
      } catch {
        return end();
      }
    }
  };
  void watch();
};

/** Writes a picture the person chose; with the run's own job, or (a run of «Другие варианты») the one it names. */
export const chooseGen = (key: string, pic: IGenPic): Promise<void> => {
  const token = tokens.get(key);
  const name = names.get(key);
  if (token === undefined || name === undefined) return Promise.resolve();
  return write(key, token, pic, name);
};

/** Shows the variants of an element drawn earlier as a run to choose from; nothing is drawn. */
export const offerGen = (
  key: string,
  artifact: string,
  tpl: number,
  prompt: string,
  name: string,
  pics: IGenPic[],
  apply: TGenApply,
): void => {
  const token = (tokenSeq += 1);
  tokens.set(key, token);
  applies.set(key, apply);
  names.set(key, name);
  setRun({
    key,
    artifact,
    tpl,
    prompt,
    count: pics.length,
    phase: GEN_PHASE.choose,
    pics,
    applying: false,
    error: null,
  });
};

export const dismissGen = (key: string) => {
  names.delete(key);
  dropRun(key);
};

const EMPTY: readonly IGenRun[] = [];
let cachedRuns: { source: ReadonlyMap<string, IGenRun>; list: readonly IGenRun[] } = {
  source: state.runs,
  list: EMPTY,
};
const runsList = (): readonly IGenRun[] => {
  if (cachedRuns.source !== state.runs) cachedRuns = { source: state.runs, list: [...state.runs.values()] };
  return cachedRuns.list;
};

export const useGenRun = (key: string): IGenRun | null =>
  useSyncExternalStore(
    subscribe,
    () => state.runs.get(key) ?? null,
    () => null,
  );

export const useGenRuns = (): readonly IGenRun[] => useSyncExternalStore(subscribe, runsList, () => EMPTY);

/** The latest fingerprint the board or the machine gave an element (a write changes it); null when none is known. */
export const latestGenWas = (key: string): string | null => seen.get(key) ?? null;

/** Notes the fingerprint the board gave an element (hover, select): a picture is written against the latest one. */
export const noteGenWas = (key: string, was: string): void => {
  seen.set(key, was);
};

/** An element was drawn or changed from outside a run (the editor's choice): what it remembers and its fingerprint now. */
export const recordGenDone = (key: string, drawn: IGenDone, was: string | null): void => {
  if (was) seen.set(key, was);
  state = { ...state, done: new Map(state.done).set(key, { ...drawn, was }) };
  emit();
};

export const useGenDone = (key: string): IGenDoneNow | null =>
  useSyncExternalStore(
    subscribe,
    () => state.done.get(key) ?? null,
    () => null,
  );

/** The board the person is looking at and how to read it again: a write that lands late must not reload another one. */
let onScreen: { board: string | null; reload: () => void } = { board: null, reload: () => {} };
export const showBoardOnScreen = (board: string | null, reload: () => void): void => {
  onScreen = { board, reload };
};
export const reloadIfOnScreen = (board: string): void => {
  if (onScreen.board === board) onScreen.reload();
};
