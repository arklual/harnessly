// Which chat the design's column shows. A design is not tied to one chat: any chat may edit it, and one chat may work on
// several designs. The column shows the design's current chat: the one the person came from (a chat's link, its right
// pane, the agent opening the design), else the last one they worked on this design in, else a new chat for it. It
// never moves by itself to «the latest one in the folder»: only the person's own pick changes it.

export const DESIGN_CONV = {
  /** No chat yet: the first message makes one. */
  none: 'none',
  conv: 'conv',
} as const;

export interface IDesignConvRow {
  id: string;
  cwd: string;
  lastActivityAt: number;
}

export interface IDesignBinding {
  /** The last chat the person worked on the design in. */
  id: string;
  /** Made here a moment ago: the machine's list may not have it yet, and that does not mean it is gone. */
  fresh: boolean;
}

export type TDesignConv = { k: typeof DESIGN_CONV.none } | { k: typeof DESIGN_CONV.conv; id: string };

/** In the project's folder or under it: the runtime's own rule for the design's conversations. */
export const inFolder = (cwd: string, dir: string): boolean => {
  const root = dir.replace(/\/+$/, '');
  const at = cwd.replace(/\/+$/, '');
  return at === root || at.startsWith(`${root}/`);
};

export interface IPickInput {
  /** The chat the person came from and has not changed since; null without one. */
  arrived: string | null;
  /** The last chat of the design, kept in the browser. */
  bound: IDesignBinding | null;
  /** The machine's conversations; null until its list (cached or its own) is there. */
  list: readonly IDesignConvRow[] | null;
  /** The list is the machine's own answer, not the one the browser kept: only it can say a conversation is gone. */
  answered: boolean;
}

/** The design's current chat (see the head of the file). A chat the machine itself no longer has is not shown. */
export const pickDesignConv = ({ arrived, bound, list, answered }: IPickInput): TDesignConv => {
  const id = arrived ?? bound?.id ?? null;
  if (id === null) return { k: DESIGN_CONV.none };
  const fresh = arrived === null && bound?.fresh === true;
  const gone = answered && list !== null && !fresh && !list.some((c) => c.id === id);
  return gone ? { k: DESIGN_CONV.none } : { k: DESIGN_CONV.conv, id };
};

/**
 * The chats that worked on the design, for the column's switch. First the ones tied to it — in its folder, or the ones
 * it was opened from or worked in (`seen`) — then the others the machine found worked on it (`touched`: their agent
 * wrote its files or named it to the design CLI, the chats from before included); each group the latest activity
 * first. A chat the list does not have yet (made a moment ago, or the list still loading) stays in when it is the
 * current one.
 */
export const designChats = <T extends IDesignConvRow>({
  list,
  dir,
  seen,
  touched,
  current,
}: {
  list: readonly T[] | null;
  dir: string | null;
  seen: readonly string[];
  touched: readonly string[];
  current: string | null;
}): T[] => {
  const known = new Set(seen);
  if (current) known.add(current);
  const worked = new Set(touched);
  const latest = (a: T, b: T) => b.lastActivityAt - a.lastActivityAt;
  const all = list ?? [];
  const tied = all.filter((c) => known.has(c.id) || (dir !== null && inFolder(c.cwd, dir)));
  const tiedIds = new Set(tied.map((c) => c.id));
  const others = all.filter((c) => worked.has(c.id) && !tiedIds.has(c.id));
  return [...tied.sort(latest), ...others.sort(latest)];
};

/** The key a design's chats are kept under: a design is per machine. */
export const designKey = (machine: string, design: string): string => `${machine}|${design}`;

/** How many chats a design remembers it was opened from or worked in. */
export const SEEN_MAX = 20;

/** The remembered chats with this one first. */
export const withSeen = (seen: readonly string[], id: string): string[] =>
  [id, ...seen.filter((x) => x !== id)].slice(0, SEEN_MAX);
