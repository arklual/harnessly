// Types of @harnessly/app-sdk, served at /__hl/app-sdk.d.ts.

export type HarnesslyErrorCode =
  | 'permission_denied'
  | 'not_in_target'
  | 'policy_blocked'
  | 'network_denied'
  | 'host_not_allowed'
  | 'tls_untrusted'
  | 'upstream_error'
  | 'rate_limited'
  | 'host_unavailable'
  | 'invalid_params'
  | 'conflict'
  | 'quota_exceeded';

export class HarnesslyError extends Error {
  readonly code: HarnesslyErrorCode;
  /** For `conflict`: the version the key has now. */
  readonly version?: number;
  constructor(code: HarnesslyErrorCode, message?: string);
}

export type StorageScope = 'install' | 'user' | 'chat' | 'space' | 'doc';

export interface HarnesslyContext {
  app: { id: string; version: string };
  /** Where the view is shown. */
  target: 'chat' | 'pane' | 'app' | `sinopia.${string}`;
  view: string;
  theme: { mode: 'light' | 'dark'; tokens: Record<string, string> };
  locale: string;
  chat: { id: string } | null;
  /** The name and role the host shows; not a proof of identity. */
  user: { name: string; role: string } | null;
  /** True when the document the view is shown in may only be read: draw it, offer no edit (the host refuses writes to it). */
  readOnly?: boolean;
  /** For a card: the data the tool returned. */
  data?: unknown;
}

export interface ChatMessage {
  role: 'user' | 'assistant';
  text: string;
}

export interface StorageArea {
  get<T = unknown>(key: string): Promise<T | null>;
  /** The value and the key's version: a counter that goes up with every write or delete of the key (0: never written). */
  getVersioned<T = unknown>(key: string): Promise<{ value: T | null; version: number }>;
  /**
   * Writes the value and answers the key's new version. With `ifVersion` it writes only if the key is still at that
   * version; otherwise it rejects with code `conflict` (`error.version` is the current one) and writes nothing.
   * A write past the area's limit rejects with `quota_exceeded`.
   */
  set(key: string, value: unknown, options?: { ifVersion?: number }): Promise<number>;
  delete(key: string): Promise<void>;
  keys(): Promise<string[]>;
}

/** An element of a board: `id` is `<page>:<number>`, the number is its `data-dc-tpl`. */
export interface SinopiaNode {
  id: string;
  page: string;
  tag: string;
  text?: string;
  attrs?: Record<string, string>;
  field?: string;
  children?: string[];
  [more: string]: unknown;
}

/** One change of a board; all the ops of a patch apply or none does. `fp` (a node's fingerprint) refuses the change if the node moved on. */
export type SinopiaOp =
  | { op: 'setText'; node: string; value: string; fp?: string }
  | { op: 'setStyle'; node: string; style: Record<string, string | null>; fp?: string }
  | { op: 'setAttr'; node: string; name: string; value: string | null; fp?: string }
  | { op: 'insert'; node: string; html: string; position: 'before' | 'after' | 'start' | 'end'; fp?: string }
  | { op: 'remove'; node: string; fp?: string }
  | { op: 'setField'; field: string; value: unknown };

export interface SinopiaApi {
  document(): Promise<{ id: string; kind: string; title: string; boards: { id: string; title: string; file: string }[]; rev: number }>;
  /** What the person picked on the board (also arrives as the `selection` event). */
  selection(): Promise<{ document: string; nodes: SinopiaNode[] }>;
  node(id: string): Promise<SinopiaNode>;
  /** A CSS selector: type, #id, .class, [attr], and the combinators ' ' and '>'. At most 500 nodes come back. */
  query(selector: string, options?: { page?: string }): Promise<{ nodes: SinopiaNode[]; count: number; truncated?: boolean }>;
  patch(ops: SinopiaOp[]): Promise<unknown>;
  /** The values of the fields the app's `data-hl-field` bindings mark on the boards. */
  fields(): Promise<{ fields: Record<string, unknown>; bindings: unknown[] }>;
  setFields(patch: Record<string, unknown>): Promise<unknown>;
  /** Marks the nodes on the board (the person sees them outlined); `replace` drops the app's earlier marks first. */
  mark(nodes: string[], options: { note: string; level?: 'info' | 'warning' | 'error'; replace?: boolean }): Promise<{ marks: number }>;
  comment(node: string, text: string): Promise<unknown>;
  /** Not available yet: refused with host_unavailable. */
  render(options?: Record<string, unknown>): Promise<unknown>;
  boards: { add(options: { template: string; title?: string; data?: Record<string, unknown> }): Promise<unknown> };
}

export interface AgentInfo {
  /** The conversation's id: `hl.pane.open('chat:' + id)` opens it. */
  id: string;
  title: string;
  /** At work, waiting for the person (a question, a permission, a plan), or free. */
  status: 'working' | 'waiting' | 'free';
  provider: string;
  /** The name of the machine. */
  machine: string;
}

export interface Harnessly {
  readonly context: HarnesslyContext;
  readonly host: { name: string; version: string };
  /**
   * Events: `context`, `theme`, `selection`, `data` (a chat card's new data), and
   * `storage` ({ scope, key }: a key of this app's storage was written or deleted, by the agent, another view or this
   * one; never the value, read the key again; an area of a chat or document only reaches the views of that chat or
   * document), `visibility` ({ visible }: the view was hidden or shown), `agents` ({ agents }: the list of the person's
   * conversations on this machine, or the status of one, changed; only with `agents.read`). `tool-input` and `tool-result` are declared by
   * MCP Apps but the host does not send them yet. Returns the unsubscribe.
   */
  on(event: 'storage', fn: (payload: { scope: StorageScope; key: string }) => void): () => void;
  on(event: 'agents', fn: (payload: { agents: AgentInfo[] }) => void): () => void;
  on(event: 'visibility', fn: (payload: { visible: boolean }) => void): () => void;
  on(event: string, fn: (payload: any) => void): () => void;
  chat: {
    /** The chat shows the app's name before the text. */
    send(text: string): Promise<void>;
    read(opts?: { last?: number }): Promise<ChatMessage[]>;
    card(view: string, data: unknown): Promise<void>;
  };
  agents: {
    /** The person's conversations on this machine (needs `agents.read`): who and in what state, never what was said. */
    list(): Promise<AgentInfo[]>;
  };
  /** `app:<id>[/<view>]`, or `chat:<id>` to open a conversation from `agents.list` (needs `agents.read` as well). */
  pane: { open(target: string): Promise<void> };
  tools: { call(name: string, args?: Record<string, unknown>): Promise<unknown> };
  /** @deprecated Removed: every call rejects with `invalid_params` and says to use `hl.storage`. */
  flow: { status(): Promise<never>; go(state: string): Promise<never> };
  /** @deprecated Removed: every call rejects with `invalid_params` and says to use `hl.storage`. */
  data: { get(): Promise<never>; set(patch: Record<string, unknown>): Promise<never> };
  storage: Record<StorageScope, StorageArea>;
  /** The values the person saved in the app's settings form (the manifest's `settings` schema), over its defaults. */
  settings: { get(): Promise<Record<string, unknown>> };
  /** Files of the chat's workspace; needs `files.read:<mask>` / `files.write:<mask>`. Paths are relative to the workspace. */
  files: { read(path: string): Promise<string>; write(path: string, text: string): Promise<void> };
  ui: { toast(text: string): Promise<void>; confirm(text: string): Promise<boolean> };
  /** Slot views: an onboarding step calls done() when the person has finished it. */
  slot: { done(): Promise<void> };
  /** The Document API of Sinopia; only in a view with target `sinopia.<kind>` and the matching `sinopia.*` rights. */
  sinopia: SinopiaApi;
  notify(text: string): Promise<void>;
  resize(height: number): void;
  close(): void;
}

/** Handshake with the host; rejects with host_unavailable outside a Harnessly frame. */
export function connect(options?: { name?: string; version?: string }): Promise<Harnessly>;

// A view loads the SDK as a classic script (<script src="/__hl/app-sdk.js">) and finds it as `harnessly`.
declare global {
  const harnessly: { connect: typeof connect; HarnesslyError: typeof HarnesslyError };
}
