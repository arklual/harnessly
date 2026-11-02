// The keys of the cloud recognition models (the runtime's <platform>/voice/keys), kept beside the vendors' keys: OpenAI's
// key, Yandex Cloud's API key and, if the account needs it, the folder's id. As everywhere, a key goes out in the body
// of the PUT and is never read back: the answers carry only «configured» and the last characters.
import { getPage, sendBody, sendPage } from '@/next/core/net/http';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId, ProfileKey } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;

export const SPEECH_KEYS = ['openai', 'yandex', 'yandex-folder'] as const;
export type SpeechKeyId = (typeof SPEECH_KEYS)[number];
export type SpeechKeys = Readonly<Record<SpeechKeyId, ProfileKey>>;

const TAIL = /^….{4}$/u;

const readKey = (v: unknown): ProfileKey | null => {
  if (typeof v !== 'object' || v === null) return null;
  const o = v as Record<string, unknown>;
  if (typeof o.configured !== 'boolean') return null;
  if (o.tail === null) return o.configured ? null : { configured: false, tail: null };
  if (typeof o.tail !== 'string' || !TAIL.test(o.tail)) return null;
  return { configured: o.configured, tail: o.tail };
};

export const readSpeechKeys = (v: unknown): SpeechKeys => {
  const keys = (v as { keys?: Record<string, unknown> } | null)?.keys;
  if (typeof keys !== 'object' || keys === null) throw new Error('not the speech keys');
  const out: Partial<Record<SpeechKeyId, ProfileKey>> = {};
  for (const id of SPEECH_KEYS) {
    const key = readKey(keys[id]);
    if (!key) throw new Error(`not a key: ${id}`);
    out[id] = key;
  }
  return out as SpeechKeys;
};

const keysPath = (machine: MachineId, id?: SpeechKeyId) =>
  runtimePath(platformPath(id ? `/voice/keys/${id}` : '/voice/keys'), machine);

export const loadSpeechKeys = (machine: MachineId) => getPage(keysPath(machine), readSpeechKeys, TIMEOUT_MS);

export const putSpeechKey = (machine: MachineId, id: SpeechKeyId, key: string) =>
  sendBody('PUT', keysPath(machine, id), { key }, readSpeechKeys, TIMEOUT_MS);

export const deleteSpeechKey = (machine: MachineId, id: SpeechKeyId) =>
  sendPage('DELETE', keysPath(machine, id), readSpeechKeys, TIMEOUT_MS);
