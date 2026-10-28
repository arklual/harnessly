// «Обновления» from the runtime's GET <platform>/update: the programs of a machine a hub carries (`machine`: whether
// they are the hub's, and the way to update them — by themselves, `auto`, or by the hub installer's command, `manual`),
// or, on a standalone runtime, its version and the newest release.
import { getPage, sendPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
/** An update downloads two programs; the runtime answers when they are in place. */
const APPLY_TIMEOUT_MS = 10 * 60_000;

export interface IMachineUpdate {
  mode: 'auto' | 'manual';
  checked: boolean;
  upToDate: boolean;
  applying: boolean;
  error: string | null;
  command: string;
}

export const readMachineUpdate = (v: unknown): IMachineUpdate | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if ((o.mode !== 'auto' && o.mode !== 'manual') || typeof o.command !== 'string') return null;
  return {
    mode: o.mode,
    checked: o.checked === true,
    upToDate: o.upToDate === true,
    applying: o.applying === true,
    error: typeof o.error === 'string' && o.error !== '' ? o.error : null,
    command: o.command,
  };
};

/** What «Обновления» shows: a hub's machine updates itself; a standalone runtime only knows the newest release. */
export type TUpdateInfo =
  | { kind: 'hub'; machine: IMachineUpdate }
  | { kind: 'standalone'; current: string; latest: string | null; url: string };

const readAnswer = (v: unknown): TUpdateInfo | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  const machine = readMachineUpdate(o.machine);
  if (machine) return { kind: 'hub', machine };
  // A runtime that does not look for releases (a «dev» build, a platform's own machine) has nothing to tell here.
  if (o.checking !== true || typeof o.current !== 'string' || o.current === '') return null;
  const newer = o.updateAvailable === true && typeof o.latest === 'string' && o.latest !== '';
  return {
    kind: 'standalone',
    current: o.current,
    latest: newer ? (o.latest as string) : null,
    url: typeof o.url === 'string' && o.url.startsWith('https://') ? o.url : '',
  };
};

export const loadUpdateInfo = async (machine: MachineId): Promise<TUpdateInfo | null> => {
  const page = await getPage(runtimePath(platformPath('/update'), machine), readAnswer, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

/** Checks now and, when the hub has newer programs, puts them in place; the runtime restarts right after. */
export const applyMachineUpdate = async (machine: MachineId): Promise<IMachineUpdate | null> => {
  const page = await sendPage(
    'POST',
    runtimePath(platformPath('/update/apply'), machine),
    readMachineUpdate,
    APPLY_TIMEOUT_MS,
  );
  return 'ok' in page ? page.value : null;
};
