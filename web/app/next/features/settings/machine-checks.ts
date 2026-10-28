// «Проверка машины» from the runtime's <platform>/machine/checks: what may quietly not work on the machine (a missing
// program, ~/.local/bin off PATH, a voice model that cannot be downloaded…), each with the command that puts it right;
// and the two things only this browser knows: a page without https gets no microphone, and a microphone it was refused.
import { getPage, sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 25_000;
/** A fix may update the machine's programs: the runtime answers when they are in place. */
const FIX_TIMEOUT_MS = 10 * 60_000;

export interface IMachineCheck {
  id: string;
  ok: boolean;
  /** The runtime's own short name of the check, for a check this page has no words for. */
  title: string;
  /** What was seen: a version, a path, the error. */
  detail: string;
  /** The command that puts it right, '' when there is none. */
  fixCommand: string;
  /** The runtime does the fix itself, without root. */
  fixable: boolean;
}

/** The checks this page has words for (next-settings.json: next.settings.checks.<id>, .hint). */
export const KNOWN_CHECKS = [
  'tmux',
  'git',
  'agent_cli',
  'git_identity',
  'local_bin_path',
  'cli',
  'linger',
  'ffmpeg',
  'voice_engine',
  'voice_download',
  'disk',
  'https',
  'mic',
] as const;

export const isKnownCheck = (id: string): id is (typeof KNOWN_CHECKS)[number] =>
  KNOWN_CHECKS.includes(id as (typeof KNOWN_CHECKS)[number]);

const readCheck = (v: unknown): IMachineCheck | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.id !== 'string' || typeof o.ok !== 'boolean') return null;
  return {
    id: o.id,
    ok: o.ok,
    title: typeof o.title === 'string' ? o.title : o.id,
    detail: typeof o.detail === 'string' ? o.detail : '',
    fixCommand: typeof o.fix_command === 'string' ? o.fix_command : '',
    fixable: o.fixable === true,
  };
};

export const readChecks = (v: unknown): IMachineCheck[] | null => {
  if (typeof v !== 'object' || v === null) return null;
  const list = (v as Record<string, unknown>).checks;
  if (!Array.isArray(list)) return null;
  return list.map(readCheck).filter((c): c is IMachineCheck => c !== null);
};

const checksPath = (machine: MachineId, tail = '') =>
  runtimePath(platformPath(`/machine/checks${tail}`), machine);

/** The machine's checks; null when it does not answer (an older runtime has none). */
export const loadMachineChecks = async (machine: MachineId): Promise<IMachineCheck[] | null> => {
  const page = await getPage(checksPath(machine), readChecks, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

/** Runs the runtime's own fix of a check, then the checks again; null when it did not go through. */
export const fixMachineCheck = async (machine: MachineId, id: string): Promise<IMachineCheck[] | null> => {
  const page = await sendBody('POST', checksPath(machine, '/fix'), { id }, readChecks, FIX_TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

/** What this browser knows about its microphone: a page that is not https gets none, and one may have been refused. */
export const browserChecks = async (): Promise<IMachineCheck[]> => {
  const secure = window.isSecureContext;
  let refused = false;
  try {
    const state = await navigator.permissions?.query({ name: 'microphone' as PermissionName });
    refused = state?.state === 'denied';
  } catch {
    // A browser that does not tell (older Safari, Firefox) is not known to refuse.
  }
  const check = (id: 'https' | 'mic', ok: boolean): IMachineCheck => ({
    id,
    ok,
    title: id,
    detail: '',
    fixCommand: '',
    fixable: false,
  });
  return [check('https', secure), check('mic', !secure || !refused)];
};
