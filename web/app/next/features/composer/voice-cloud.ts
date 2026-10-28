// The person's recordings in the cluster (…/voice on the gateway): what the
// browser keeps goes there once it is over, so a recording outlives the browser and the machine. The audio is kept 30
// days, the text for good. Standalone there is no such route: the answer reads as «not here» and the takes stay in
// the browser only.
import { gatewayPath } from '@/next/core/net/machine';
import { getPage, sendBlob, sendJson, sendPage } from '@/next/core/net/http';
import type { MachineId, Ms } from '@/next/protocol/v2';

const TIMEOUT_MS = 10_000;
/** A few minutes of speech are a few MB; a slow tunnel takes its time. */
const UPLOAD_MS = 120_000;

export interface ICloudTake {
  id: string;
  machine: MachineId;
  at: Ms;
  ms: number;
  mime: string;
  state: 'failed' | 'done';
  text: string | null;
  /** The audio is still kept (30 days). */
  audio: boolean;
}

const takePath = (id: string) => `/voice/takes/${encodeURIComponent(id)}` as const;

const parseList = (v: unknown): ICloudTake[] => {
  if (typeof v !== 'object' || v === null || !Array.isArray((v as { takes?: unknown }).takes))
    throw new Error('takes');
  return (v as { takes: Record<string, unknown>[] }).takes.flatMap((row) =>
    typeof row.id === 'string' && typeof row.at === 'number'
      ? [
          {
            id: row.id,
            machine: String(row.machineId ?? '') as MachineId,
            at: row.at as Ms,
            ms: typeof row.ms === 'number' ? row.ms : 0,
            mime: typeof row.mime === 'string' ? row.mime : 'audio/webm',
            state: row.state === 'failed' ? ('failed' as const) : ('done' as const),
            text: typeof row.text === 'string' ? row.text : null,
            audio: row.audio === true,
          },
        ]
      : [],
  );
};

const ignore = () => null;

/** The person's takes, newest first; null where the cluster keeps none (standalone) or did not answer. */
export const listCloudTakes = async (): Promise<ICloudTake[] | null> => {
  const res = await getPage(gatewayPath('/voice/takes'), parseList, TIMEOUT_MS);
  return 'ok' in res ? res.value : null;
};

export const putCloudTake = async (take: {
  id: string;
  machine: MachineId;
  at: Ms;
  ms: number;
  state: 'failed' | 'done';
  audio: Blob;
}): Promise<boolean> => {
  const q = new URLSearchParams({
    machine: take.machine,
    at: String(take.at),
    ms: String(take.ms),
    state: take.state,
  });
  const res = await sendBlob(gatewayPath(`${takePath(take.id)}?${q}`), take.audio, ignore, UPLOAD_MS);
  return 'ok' in res;
};

export const patchCloudTake = async (
  id: string,
  patch: { text?: string | null; state?: 'failed' | 'done' },
) => 'ok' in (await sendJson(gatewayPath(takePath(id)), patch, ignore, TIMEOUT_MS));

export const deleteCloudTake = async (id: string) =>
  'ok' in (await sendPage('DELETE', gatewayPath(takePath(id)), ignore, TIMEOUT_MS));

/** Where the browser plays and downloads a take's audio from (the gateway answers with the person's own only). */
export const cloudAudioUrl = (id: string) => gatewayPath(`${takePath(id)}/audio`);
