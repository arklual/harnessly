// What two runs of the scenario must agree on: paths under the scenario's home are written {home}, entry ids (a
// counter in both runtimes) are numbered by first appearance, and the heartbeat (a clock) is left out. The calls to the
// layout store and the keeper keep their order within a tab; two tabs' watchers noticing one change run in either order.
const ID_RE = /^id[0-9a-z]{4,}$/;

export const normalize = (out, vars) => {
  const ids = new Map();
  const walk = (v) => {
    if (typeof v === 'string') {
      if (ID_RE.test(v)) {
        if (!ids.has(v)) ids.set(v, `entry-${ids.size + 1}`);
        return ids.get(v);
      }
      return v.split(vars.home).join('{home}');
    }
    if (Array.isArray(v)) return v.map(walk);
    if (v && typeof v === 'object') return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, walk(x)]));
    return v;
  };
  const sockets = {};
  for (const name of Object.keys(out.sockets).sort()) {
    sockets[name] = walk(out.sockets[name].filter((m) => m.type !== 'timeline:heartbeat'));
  }
  const tabOf = (call) => String(call[1]).split('-').pop();
  const effects = out.effects.map((call, i) => [tabOf(call), i, call]).sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : a[1] - b[1]));
  return { sockets, effects: walk(effects.map(([, , call]) => call)) };
};
