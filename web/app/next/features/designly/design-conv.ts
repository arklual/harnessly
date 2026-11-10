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
  lastActivityAt: number;
}

export interface IDesignBinding {
  /** The last chat the person worked on the design in. */
  id: string;
  /** Made here a moment ago: the machine's list may not have it yet, and that does not mean it is gone. */
  fresh: boolean;
}

export type TDesignConv = { k: typeof DESIGN_CONV.none } | { k: typeof DESIGN_CONV.conv; id: string };

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

/** What one machine said of the design's chats: the ones started from it, then the ones that edited its files. */
export interface IDesignChatsFound {
  machine: string;
  convs: readonly string[];
}

/**
 * The design's chats for the column's switch, from every machine of the person (HRN-675): the ones each machine says
 * were started from the design or edited its files, the latest activity first. Nothing else ties a chat to the design:
 * not its folder, not opening the design from it, not naming it — a chat the design was opened from is shown in the
 * column, but is not listed as the design's. A chat just started from the column (`fresh`) is in before the machine's
 * next answer has it.
 */
export const designChats = <T extends IDesignConvRow & { machineId: string }>({
  list,
  found,
  fresh,
}: {
  /** The conversations of every machine. */
  list: readonly T[] | null;
  found: readonly IDesignChatsFound[];
  fresh: string | null;
}): T[] => {
  const tied = new Set(found.flatMap((f) => f.convs.map((id) => `${f.machine}|${id}`)));
  return (list ?? [])
    .filter((c) => c.id === fresh || tied.has(`${c.machineId}|${c.id}`))
    .sort((a, b) => b.lastActivityAt - a.lastActivityAt);
};

/** The key a design's chats are kept under: a design is per machine. */
export const designKey = (machine: string, design: string): string => `${machine}|${design}`;
