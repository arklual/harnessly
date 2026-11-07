// Sinopia's tools of the page and what it publishes: while a document is open beside its chat, the editor
// offers the chat's agent tools in WebMCP's form (registerTool through its SDK: show a board, reload it) and publishes what is on screen — the document, the board, the selection — for page_tools to read. The
// tools of its manifest (sinopia_*) stay for the work that needs no open editor.
import { useEffect, useRef } from 'react';
import type { IBuiltinHl, IBuiltinTool } from '@/next/features/app-views/builtin-app';
import { connectSinopia } from '@/next/features/app-views/sinopia/host';
import type { ISelectedNode } from '@/next/features/app-views/sinopia/types';
import type { ConvId, MachineId } from '@/next/protocol/v2';

export interface ISinopiaBoard {
  path: string;
  title: string;
}

export interface ISinopiaScreen {
  id: string;
  kind: string | null;
  title: string;
  boards: readonly ISinopiaBoard[];
  /** The board on screen; null while there is none. */
  path: string | null;
  selection: readonly ISelectedNode[];
  viewing: boolean;
}

/** What the editor's tools do on its screen. */
export interface ISinopiaActions {
  onGo: (path: string) => void;
  onReload: () => void;
}

export interface ISinopiaPageOptions extends ISinopiaScreen {
  machine: MachineId;
  conversation: ConvId | null;
}

type TNow = ISinopiaScreen & ISinopiaActions;

/** At most this many boards and selected nodes are published, and this much of a node's words. */
export const MAX_BOARDS = 100;
export const MAX_NODES = 20;
const MAX_TEXT = 200;
/** A change of the screen is published once it settles. */
export const PUBLISH_MS = 250;

const cut = (s: string | undefined) =>
  s === undefined || s.length <= MAX_TEXT ? s : `${s.slice(0, MAX_TEXT)}…`;

/** What the agent reads of the editor with page_tools: plain JSON, held small. */
export const screenContext = (s: ISinopiaScreen) => {
  const at = s.path === null ? -1 : s.boards.findIndex((b) => b.path === s.path);
  return {
    document: { id: s.id, kind: s.kind, title: s.title },
    mode: s.viewing ? 'viewing' : 'editing',
    board: at < 0 ? null : { number: at + 1, path: s.boards[at].path, title: s.boards[at].title },
    boards: {
      count: s.boards.length,
      list: s.boards.slice(0, MAX_BOARDS).map((b, i) => ({ number: i + 1, path: b.path, title: b.title })),
    },
    selection: s.selection.slice(0, MAX_NODES).map((n) => ({
      ...(n.id ? { id: n.id } : {}),
      ...(n.page ? { board: n.page } : {}),
      ...(n.selector ? { selector: n.selector } : {}),
      ...(n.field ? { field: n.field } : {}),
      tag: n.tag,
      ...(n.text ? { text: cut(n.text) } : {}),
    })),
  };
};

/** The board an agent names: its path, or its number from 1. */
export const boardOf = (boards: readonly ISinopiaBoard[], named: unknown): ISinopiaBoard => {
  if (typeof named === 'number' && Number.isInteger(named) && named >= 1 && named <= boards.length)
    return boards[named - 1];
  if (typeof named === 'string') {
    const found =
      boards.find((b) => b.path === named) ?? (/^\d+$/.test(named) ? boards[Number(named) - 1] : undefined);
    if (found) return found;
  }
  throw new Error(`no board ${JSON.stringify(named)}: give a path or a number from 1 to ${boards.length}`);
};

/** The editor's tools, in WebMCP's form; `now` reads the screen and the editor's hands at the time of the call. */
export const screenTools = (now: () => TNow): IBuiltinTool[] => [
  {
    name: 'show_board',
    description:
      "Show a board of the document on the person's screen: a slide of a deck, a scene of a video, a page of a site. " +
      'Name it by its path or its number from 1 (as page_tools lists them). Answers the board shown.',
    inputSchema: {
      type: 'object',
      properties: {
        board: { type: ['string', 'integer'], description: 'The board’s path, or its number from 1.' },
      },
      required: ['board'],
    },
    execute: ({ board }) => {
      const s = now();
      const found = boardOf(s.boards, board);
      s.onGo(found.path);
      return { shown: found.path, number: s.boards.indexOf(found) + 1 };
    },
  },
  {
    name: 'reload_board',
    description: 'Reload the board on screen, to show the person what was just written to its file.',
    inputSchema: { type: 'object', properties: {} },
    execute: () => {
      const s = now();
      if (s.path === null) throw new Error('no board is on screen');
      s.onReload();
      return { reloaded: s.path };
    },
  },
];

/**
 * Offers the editor's tools and publishes its screen while the document is open beside a chat. The actions are read
 * from their ref when a tool runs, never while rendering.
 */
export const useSinopiaPage = (opts: ISinopiaPageOptions, actions: { readonly current: ISinopiaActions }) => {
  const latest = useRef(opts);
  useEffect(() => {
    latest.current = opts;
  });
  const hl = useRef<IBuiltinHl | null>(null);
  const { machine, conversation, id } = opts;
  useEffect(() => {
    if (conversation === null) return;
    const sdk = connectSinopia({ machine, conversation }, id);
    hl.current = sdk;
    for (const tool of screenTools(() => ({ ...latest.current, ...actions.current })))
      sdk.modelContext.registerTool(tool);
    return () => {
      hl.current = null;
      sdk.dispose();
    };
  }, [machine, conversation, id, actions]);

  const context = conversation === null ? null : JSON.stringify(screenContext(opts));
  useEffect(() => {
    if (context === null) return;
    const timer = window.setTimeout(() => {
      void hl.current?.publish(JSON.parse(context) as unknown).catch(() => undefined);
    }, PUBLISH_MS);
    return () => window.clearTimeout(timer);
    // A new connection (another chat, another document) is told the screen anew.
  }, [context, machine, conversation, id]);
};
