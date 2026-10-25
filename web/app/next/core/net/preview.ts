// The machine's pages in preview slots (/api/preview/open): each slot is an origin of its own, entered once with a
// ticket; and the ports that listen on the machine (/api/platform/preview/ports), a chat's own first, under the runtime's platform prefix.
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import { platformPath } from '@/next/core/net/machine';
import { stampSlot } from '@/next/core/net/preview-stamp';
import type { MachineId } from '@/next/protocol/v2';

export { slotNonce } from '@/next/core/net/preview-stamp';

export interface IPreviewSlot {
  slot: number;
  origin: string;
  /** Works once and for a minute; after the first load the slot's cookie lets plain slot addresses in. */
  enterUrl: string;
}

/** A port of the machine, or a site (a stand) by its origin. */
export type TPreviewTarget = { kind: 'port'; port: number } | { kind: 'url'; origin: string };

/** Slots 1–6 take ports, 7–8 sites: the runtime refuses the other way round. */
export const openPreview = (m: MachineId, target: TPreviewTarget, slot: number, path = '/') =>
  runtimeJson<IPreviewSlot>(m, '/api/preview/open', postJson({ target, slot, path })).then((opened) => ({
    ...opened,
    nonce: stampSlot(slot),
  }));

export interface IListeningPort {
  port: number;
  command: string | null;
  cwd: string | null;
  /** chat: the process runs in one of the folders asked about. */
  group: 'chat' | 'agent' | 'other';
  kind: 'site' | 'api' | 'unknown';
  responding: boolean;
  /** What the page calls itself (its <title>): the name to show, rather than the process's. */
  title: string | null;
  description: string | null;
  /** The page's icon as a small data: image (the machine's address is not one the browser can load). */
  icon: string | null;
}

/** The ports listening on the machine; `paths` mark the chat's own. */
export const fetchPorts = async (m: MachineId, paths: string[], signal?: AbortSignal) => {
  const query = encodeURIComponent(JSON.stringify(paths));
  const raw = await runtimeJson<{ ports: IListeningPort[] }>(
    m,
    platformPath(`/preview/ports?paths=${query}`),
    {
      signal,
    },
  );
  return raw.ports.map((p) => ({
    port: p.port,
    command: p.command,
    cwd: p.cwd,
    group: p.group,
    kind: p.kind,
    responding: p.responding,
    title: p.title ?? null,
    description: p.description ?? null,
    icon: p.icon ?? null,
  }));
};
