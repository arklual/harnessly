// The machine's dictation (the runtime's <platform>/voice/engine): whether ffmpeg is there, how the recognition stands,
// which engine recognizes (the built-in whisper or a server of the person's own that speaks OpenAI's API) and the
// model it recognizes with. The server's key is never sent back: only its last characters, as `keyMask`.
import { getPage, sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const VOICE_MODELS = ['auto', 'tiny', 'base', 'small', 'medium', 'large-v3-turbo'] as const;
export type TVoiceModel = (typeof VOICE_MODELS)[number];

export const VOICE_ENGINES = ['builtin', 'remote'] as const;
export type TVoiceEngineKind = (typeof VOICE_ENGINES)[number];

export interface IVoiceRemote {
  url: string;
  model: string;
  /** The key's last characters behind dots, '' when there is no key. */
  keyMask: string;
}

export interface IVoiceEngine {
  /** ffmpeg is there; without it the microphone is not offered. */
  decoder: boolean;
  /** The runtime's own whisper recognizes here, so the model is the person's to choose. */
  builtin: boolean;
  /** The runtime decides how this machine recognizes (no service of the operator's is set up instead). */
  choosable: boolean;
  engine: TVoiceEngineKind;
  state: 'idle' | 'preparing' | 'ready' | 'failed' | 'external';
  percent: number;
  error: string;
  model: TVoiceModel;
  models: readonly TVoiceModel[];
  /** A model's download size in bytes. */
  sizes: Readonly<Partial<Record<TVoiceModel, number>>>;
  remote: IVoiceRemote | null;
  /** The command that installs ffmpeg on this machine. */
  install: string;
}

/** A change the machine did not take, in its words. */
export interface IVoiceRefusal {
  error: string;
}

const STATES = ['idle', 'preparing', 'ready', 'failed', 'external'] as const;

const isModel = (v: unknown): v is TVoiceModel => VOICE_MODELS.includes(v as TVoiceModel);

const readRemote = (v: unknown): IVoiceRemote | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.url !== 'string' || o.url === '') return null;
  return {
    url: o.url,
    model: typeof o.model === 'string' ? o.model : '',
    keyMask: typeof o.keyMask === 'string' ? o.keyMask : '',
  };
};

const readSizes = (v: unknown): Partial<Record<TVoiceModel, number>> => {
  if (typeof v !== 'object' || v === null) return {};
  const out: Partial<Record<TVoiceModel, number>> = {};
  for (const [k, n] of Object.entries(v)) if (isModel(k) && typeof n === 'number') out[k] = n;
  return out;
};

export const readVoiceEngine = (v: unknown): IVoiceEngine | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.decoder !== 'boolean' || typeof o.install !== 'string') return null;
  const state = STATES.find((s) => s === o.state) ?? 'idle';
  return {
    decoder: o.decoder,
    builtin: o.builtin === true,
    choosable: o.choosable === true,
    engine: o.engine === 'remote' ? 'remote' : 'builtin',
    state,
    percent: typeof o.percent === 'number' ? o.percent : 0,
    error: typeof o.error === 'string' ? o.error : '',
    model: isModel(o.model) ? o.model : 'auto',
    models: Array.isArray(o.models) ? o.models.filter(isModel) : [...VOICE_MODELS],
    sizes: readSizes(o.sizes),
    remote: readRemote(o.remote),
    install: o.install,
  };
};

const readChange = (v: unknown): IVoiceEngine | IVoiceRefusal | null => {
  const engine = readVoiceEngine(v);
  if (engine) return engine;
  if (typeof v === 'object' && v !== null && typeof (v as Record<string, unknown>).error === 'string')
    return { error: (v as { error: string }).error };
  return null;
};

/** An address's origin (scheme, host, port), null when it is not an address. */
export const originOf = (raw: string): string | null => {
  try {
    return new URL(raw.trim()).origin.toLowerCase();
  } catch {
    return null;
  }
};

/** The saved key is kept only for the server it was saved for: another origin needs its key typed. */
export const needsNewKey = (remote: IVoiceRemote | null, url: string): boolean =>
  remote !== null && remote.keyMask !== '' && originOf(remote.url) !== originOf(url);

const enginePath = (machine: MachineId) => runtimePath(platformPath('/voice/engine'), machine);

export const loadVoiceEngine = async (machine: MachineId): Promise<IVoiceEngine | null> => {
  const page = await getPage(enginePath(machine), readVoiceEngine, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

/** A change of the machine's dictation: a model, or the engine (with the server's address, model and key). */
export type TVoiceChange =
  | { model: TVoiceModel }
  | { engine: 'builtin' }
  | { engine: 'remote'; url: string; model: string; key?: string };

const change = async (
  machine: MachineId,
  body: TVoiceChange,
): Promise<IVoiceEngine | IVoiceRefusal | null> => {
  const page = await sendBody('PUT', enginePath(machine), body, readChange, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

export const saveVoiceModel = async (
  machine: MachineId,
  model: TVoiceModel,
): Promise<IVoiceEngine | null> => {
  const res = await change(machine, { model });
  return res && 'decoder' in res ? res : null;
};

export const saveVoiceEngine = (machine: MachineId, body: Exclude<TVoiceChange, { model: TVoiceModel }>) =>
  change(machine, body);
