// The machine's pages in preview slots (/api/preview/open): each slot is an origin of its own, entered once with a
// ticket; and the ports that listen on the machine (/api/platform/preview/ports), a chat's own first, under the runtime's platform prefix.
import { postJson, RuntimeError, runtimeJson } from '@/next/core/net/runtime-json';
import { t } from '@/next/i18n';
import { platformPath } from '@/next/core/net/machine';
import { stampSlot } from '@/next/core/net/preview-stamp';
import type { MachineId } from '@/next/protocol/v2';

export { slotNonce } from '@/next/core/net/preview-stamp';

export interface IPreviewSlot {
  slot: number;
  origin: string;
  /** Works once and for a minute; after the first load the slot's cookie lets plain slot addresses in. */
  enterUrl: string;
  /** A slot by path (`<appBase>/__pv/<n>-<key>`): its key is in every address, there is no cookie to enter for, and
   *  `enterUrl` is only the plain address of `path`. */
  byPath?: boolean;
}

/** A port of the machine, or a site (a stand) by its origin. */
export type TPreviewTarget = { kind: 'port'; port: number } | { kind: 'url'; origin: string };

/** The runtime's refusals of an open, in the person's words: its codes are for logs, not for the pane. */
const OPEN_REFUSALS: Readonly<Record<string, string>> = {
  'invalid slot': 'next.pane.openRefused.slot',
  'invalid neighbours': 'next.pane.openRefused.slot',
  'invalid target': 'next.pane.openRefused.target',
  'invalid path': 'next.pane.openRefused.path',
  'preview slots unavailable': 'next.pane.openRefused.unavailable',
  'preview slots need https origins': 'next.pane.openRefused.https',
};

/** A refusal the runtime said in a code becomes a sentence; anything else stays as it was said. */
export const openFailure = (failure: unknown): unknown => {
  if (!(failure instanceof RuntimeError)) return failure;
  const key = OPEN_REFUSALS[failure.message];
  return key === undefined ? failure : new RuntimeError(t(key), failure.body);
};

/**
 * Slots 1–6 take ports, 7–8 sites: the runtime refuses the other way round. `neighbours` are the ports of the page's
 * own other servers (an MFE's remotes): the proxy turns their localhost addresses into slot paths. Ports only.
 */
export const openPreview = (
  m: MachineId,
  target: TPreviewTarget,
  slot: number,
  path = '/',
  neighbours: readonly number[] = [],
  /**
   * `path`: the slot's session rides in the address instead of a cookie. An app's view runs in an opaque-origin frame,
   * whose loads the browser files under another cookie partition than the enter navigation, so a cookie never comes along.
   */
  carry?: 'path',
) =>
  runtimeJson<IPreviewSlot>(
    m,
    '/api/preview/open',
    postJson({
      target,
      slot,
      path,
      ...(target.kind === 'port' && neighbours.length > 0 ? { neighbours: [...neighbours] } : {}),
      ...(carry ? { carry } : {}),
    }),
  ).then(
    (opened) => ({
      ...opened,
      nonce: stampSlot(slot),
    }),
    (failure: unknown) => {
      throw openFailure(failure);
    },
  );

export interface IListeningPort {
  port: number;
  pid: number | null;
  /** The agent task whose folder the process runs in. */
  owner: { repo: string; task: string } | null;
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
    pid: p.pid ?? null,
    owner: p.owner ?? null,
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
