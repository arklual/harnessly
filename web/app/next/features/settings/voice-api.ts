// The machine's dictation (the runtime's <platform>/voice/engine): whether ffmpeg is there, how the recognition stands,
// and the model it recognizes with.
import { getPage, sendBody } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const VOICE_MODELS = ['auto', 'tiny', 'base', 'small'] as const;
export type TVoiceModel = (typeof VOICE_MODELS)[number];

export interface IVoiceEngine {
  /** ffmpeg is there; without it the microphone is not offered. */
  decoder: boolean;
  /** The runtime's own engine recognizes here, so the model is the person's to choose. */
  builtin: boolean;
  state: 'idle' | 'preparing' | 'ready' | 'failed' | 'external';
  percent: number;
  error: string;
  model: TVoiceModel;
  models: readonly TVoiceModel[];
  /** The command that installs ffmpeg on this machine. */
  install: string;
}

const STATES = ['idle', 'preparing', 'ready', 'failed', 'external'] as const;

const isModel = (v: unknown): v is TVoiceModel => VOICE_MODELS.includes(v as TVoiceModel);

export const readVoiceEngine = (v: unknown): IVoiceEngine | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.decoder !== 'boolean' || typeof o.install !== 'string') return null;
  const state = STATES.find((s) => s === o.state) ?? 'idle';
  return {
    decoder: o.decoder,
    builtin: o.builtin === true,
    state,
    percent: typeof o.percent === 'number' ? o.percent : 0,
    error: typeof o.error === 'string' ? o.error : '',
    model: isModel(o.model) ? o.model : 'auto',
    models: Array.isArray(o.models) ? o.models.filter(isModel) : [...VOICE_MODELS],
    install: o.install,
  };
};

const enginePath = (machine: MachineId) => runtimePath(platformPath('/voice/engine'), machine);

export const loadVoiceEngine = async (machine: MachineId): Promise<IVoiceEngine | null> => {
  const page = await getPage(enginePath(machine), readVoiceEngine, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};

export const saveVoiceModel = async (
  machine: MachineId,
  model: TVoiceModel,
): Promise<IVoiceEngine | null> => {
  const page = await sendBody('PUT', enginePath(machine), { model }, readVoiceEngine, TIMEOUT_MS);
  return 'ok' in page ? page.value : null;
};
