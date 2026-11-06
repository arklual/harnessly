// The arithmetic of the environment screen that both the mock hub and the screen's optimistic writes need: what a layer
// counts and what the summary tiles add up to, from the rows alone.
import type { CatalogKind, EnvLayer, EnvResponse, KindCounts } from '../../hub-api/env';

export const KINDS: readonly CatalogKind[] = [
  'skill',
  'mcp',
  'repo',
  'doc',
  'script',
  'access',
  'plugin',
  'claude-settings',
  'app',
];

export const noCounts = (): KindCounts => ({ skill: 0, mcp: 0, repo: 0, doc: 0, script: 0, access: 0 });

/** The entries a layer counts: those that are switched on. */
export const countsOf = (rows: readonly { kind: CatalogKind; enabled: boolean }[]): KindCounts => {
  const out = noCounts();
  for (const r of rows) if (r.enabled) out[r.kind] = (out[r.kind] ?? 0) + 1;
  return out;
};

const sumCounts = (layers: readonly EnvLayer[]): KindCounts => {
  const out = noCounts();
  for (const l of layers)
    for (const k of KINDS) {
      const n = l.counts[k];
      // plugin and claude-settings come only when there are some, as the hub writes them
      if (n !== undefined || out[k] !== undefined) out[k] = (out[k] ?? 0) + (n ?? 0);
    }
  return out;
};

/**
 * The layers with their counts and the summary's count tiles taken from the rows again. What needs the tool facts (calls,
 * the most called, unused) stays as the hub said it.
 */
export const recount = (view: EnvResponse): EnvResponse => {
  const layers = view.layers.map((l) => ({ ...l, counts: countsOf(l.items) }));
  const byKind = sumCounts(layers);
  const pinnedByOrg = layers
    .filter((l) => l.layer === 'org')
    .reduce((n, l) => n + l.items.filter((r) => r.enabled && r.pinned).length, 0);
  return {
    ...view,
    layers,
    summary: {
      ...view.summary,
      total: KINDS.reduce((n, k) => n + (byKind[k] ?? 0), 0),
      byKind,
      pinnedByOrg,
    },
  };
};
