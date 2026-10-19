// The world the stubs read: panes, processes, the keeper's busy tabs, and what the timeline told the layout store and
// the keeper.
export const world = () => globalThis.__world;
export const record = (...call) => world().effects.push(call);
