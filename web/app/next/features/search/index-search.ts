// ⌘K from the database index (GET <platform>/search?q=). The hub (switch hub-search) and the runtime without a
// hub answer from an index of designs, apps, document types and documents, so a design of a machine that is off is
// found too. The page asks it next to the old reads; a 404 (the switch is off, an older runtime) leaves the old path and
// is not asked again for OFF_MS.
import { deviceId } from '@/next/core/device';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { DEVICE_HEADER, type DesignArtifact, type MachineId } from '@/next/protocol/v2';

export interface IIndexHit {
  kind: string;
  id: string;
  parent?: string;
  title: string;
  updatedAt: number;
}

/** A route that answered 404 is asked again after this long. */
export const INDEX_OFF_MS = 10 * 60_000;
const TIMEOUT_MS = 8000;
const offUntil = new Map<MachineId, number>();

export const resetIndexSearch = (): void => offUntil.clear();

/** The hits of the index for the words; null when the index is not there (the old path stands). */
export const searchIndex = async (
  machine: MachineId,
  q: string,
  signal?: AbortSignal,
): Promise<IIndexHit[] | null> => {
  if ((offUntil.get(machine) ?? 0) > Date.now()) return null;
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), TIMEOUT_MS);
  signal?.addEventListener('abort', () => ctl.abort());
  try {
    const res = await fetch(
      runtimePath(platformPath(`/search?q=${encodeURIComponent(q)}&limit=30`), machine),
      {
        credentials: 'same-origin',
        headers: { [DEVICE_HEADER]: deviceId() },
        signal: ctl.signal,
      },
    );
    if (res.status === 404) {
      offUntil.set(machine, Date.now() + INDEX_OFF_MS);
      return null;
    }
    if (!res.ok) return null;
    const body: unknown = await res.json();
    const hits = typeof body === 'object' && body !== null ? (body as { hits?: unknown }).hits : null;
    return Array.isArray(hits) ? (hits as IIndexHit[]) : [];
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
};

/** The designs among the hits, in the shape the search lists take; what only the machine knows is left empty. */
export const designsOfHits = (hits: readonly IIndexHit[]): DesignArtifact[] =>
  hits
    .filter((h) => h.kind === 'design')
    .map((h) => ({
      id: h.id,
      title: h.title,
      updatedAt: h.updatedAt,
      kind: 'site',
      pinned: false,
      cover: null,
      projectPath: '',
      boards: [],
      convs: [],
    }));

/** The library's designs with the ones only the index knows added after them. */
export const mergeDesigns = (
  own: readonly DesignArtifact[] | null,
  found: readonly DesignArtifact[],
): readonly DesignArtifact[] | null => {
  if (found.length === 0) return own;
  const have = new Set((own ?? []).map((d) => d.id));
  return [...(own ?? []), ...found.filter((d) => !have.has(d.id))];
};
