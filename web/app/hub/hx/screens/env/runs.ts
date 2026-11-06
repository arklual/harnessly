// Runs of the machines: how a step looks, why it fell and what could mend it, and the poll that follows a run until it ends.
import { useEffect, useRef, useState } from 'react';

import type { CatalogItem, EnvLayer, EnvRunRow, EnvRunStep, StepStatus } from '../../../../hub-api/env';
import type { Target } from './scope';

export type StepState = 'wait' | 'active' | 'done' | 'fail';

export const stateOf = (s: StepStatus): StepState => {
  if (s === 'running') return 'active';
  if (s === 'error') return 'fail';
  if (s === 'pending') return 'wait';
  return 'done';
};

export const MARK: Record<StepState, string> = {
  wait: 'circle-dashed',
  active: 'loader-circle',
  done: 'check',
  fail: 'circle-x',
};

/** The run is over: nothing of it is still going. */
export const ended = (r: EnvRunRow): boolean => r.status !== 'running';

/** «exit 127» for a step that fell with a code; «готово» for the rest. */
export const codeOf = (s: EnvRunStep): string => {
  if (s.status === 'error') return s.exit == null ? 'ошибка' : `exit ${s.exit}`;
  if (s.status === 'pending') return 'ждёт';
  if (s.status === 'skipped') return 'не нужно';
  return 'готово';
};

/** The command a log says is missing: «pnpm: command not found». */
export const missingCommand = (log: string): string | null => {
  const hits = [...log.matchAll(/([\w.+-]+): (?:command )?not found/g)];
  return hits.at(-1)?.[1] ?? null;
};

/** What the failure means in words; the board explains the two it knows, the rest says what the log says. */
export const whyOf = (s: EnvRunStep): string => {
  const log = s.log ?? '';
  const cmd = missingCommand(log);
  if (cmd === 'brew') return 'На Linux-машинах brew нет — поставьте через apt или curl.';
  if (cmd) return `На машине нет «${cmd}». Поставьте его раньше или допишите установку в сам скрипт.`;
  return s.message ?? '';
};

export interface Fix {
  /** The script to move up. */
  itemId: string;
  /** It is to stand before this one. */
  beforeItemId: string;
  name: string;
  layer: Target;
}

const textOf = (it: CatalogItem): string => {
  const s = it.source;
  return [it.name, s.body, s.run, s.path, s.check].filter((x): x is string => !!x).join('\n');
};

/** A later script of the same layer that brings the missing command: put it before the one that fell. */
export const fixFor = (
  run: EnvRunRow,
  step: EnvRunStep,
  catalog: readonly CatalogItem[],
  layers: readonly EnvLayer[],
): Fix | null => {
  const cmd = missingCommand(step.log ?? '');
  if (!cmd || cmd === 'brew') return null;
  const at = run.steps.findIndex((x) => x.itemId === step.itemId);
  const word = new RegExp(`(^|[^\\w-])${cmd.replace(/[.+]/g, '\\$&')}($|[^\\w-])`);
  for (const later of run.steps.slice(at + 1)) {
    if (later.kind !== 'script') continue;
    const item = catalog.find((c) => c.id === later.itemId);
    if (!item || !word.test(textOf(item))) continue;
    const layer = layers.find(
      (l) => l.items.some((r) => r.itemId === later.itemId) && l.items.some((r) => r.itemId === step.itemId),
    );
    if (layer) return { itemId: later.itemId, beforeItemId: step.itemId, name: later.name, layer };
  }
  return null;
};

/** The entries of a layer's scripts with `itemId` moved to stand before `beforeItemId`. */
export const moveBefore = (layer: EnvLayer, itemId: string, beforeItemId: string): string[] => {
  const ids = layer.items.filter((r) => r.kind === 'script').map((r) => r.entryId);
  const mover = layer.items.find((r) => r.itemId === itemId)?.entryId;
  const anchor = layer.items.find((r) => r.itemId === beforeItemId)?.entryId;
  if (!mover || !anchor) return ids;
  const rest = ids.filter((x) => x !== mover);
  rest.splice(rest.indexOf(anchor), 0, mover);
  return rest;
};

const POLL_MS = 900;

/**
 * Follows a run: asks for it again until it ends. `runId` null: nothing to follow. A failed ask is kept in `error` and the
 * poll stops; starting another run starts over.
 */
export const useRunPoll = (
  runId: string | null,
  fetchRun: (id: string) => Promise<EnvRunRow>,
  onEnd?: (run: EnvRunRow) => void,
): { run: EnvRunRow | null; error: unknown } => {
  const [state, setState] = useState<{ id: string | null; run: EnvRunRow | null; error: unknown }>({
    id: null,
    run: null,
    error: null,
  });
  const fetchRef = useRef(fetchRun);
  const endRef = useRef(onEnd);
  useEffect(() => {
    fetchRef.current = fetchRun;
    endRef.current = onEnd;
  });
  useEffect(() => {
    if (runId === null) return;
    let live = true;
    let timer: number | undefined;
    const tick = () => {
      fetchRef.current(runId).then(
        (run) => {
          if (!live) return;
          setState({ id: runId, run, error: null });
          if (!ended(run)) timer = window.setTimeout(tick, POLL_MS);
          else endRef.current?.(run);
        },
        (error: unknown) => {
          if (live) setState({ id: runId, run: null, error });
        },
      );
    };
    tick();
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [runId]);
  if (runId === null || state.id !== runId) return { run: null, error: null };
  return { run: state.run, error: state.error };
};
