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
  | 'quota_exceeded'
  | 'auth_required'
  | 'auth_expired'
  | 'auth_denied'
  | 'idp_unreachable';

/**
 * Where a refused sign-in is mended (`auth_required`, `auth_expired`): the person's connection the server goes through,
 * or the app's own server the host signs in to. The host shows «Войти» on it; a view cannot sign in by itself and never
 * sees a token.
 */
export interface SignInHint {
  connection?: string;
  app?: string;
  server?: string;
}

export class HarnesslyError extends Error {
  readonly code: HarnesslyErrorCode;
  /** For `conflict`: the version the key has now. */
  readonly version?: number;
  /** For `auth_required` and `auth_expired`: where the person signs in. */
  readonly signIn?: SignInHint;
  constructor(code: HarnesslyErrorCode, message?: string);
}

/**
 * What protocol 2 answers instead of rejecting (connect({ protocol: 2 })): `denied` — the app does not hold the right or
 * is not allowed here; `unsupported` — this host does not do it (an older harnessly, a method it lacks); `failed` — the
 * rest (code says which). Check it with `hl.refused(answer)`.
 */
export interface Refusal {
  error: 'denied' | 'unsupported' | 'failed';
  code: HarnesslyErrorCode;
  message: string;
  version?: number;
  signIn?: SignInHint;
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

export interface FileEntry {
  path: string;
  dir?: boolean;
  size?: number;
  /** Milliseconds. */
  modified?: number;
}

export interface SearchHit {
  path: string;
  /** 0 for a file found by name. */
  line?: number;
  text?: string;
}

export interface GitFile {
  path: string;
  from?: string;
  /** The letters of git status: M, A, D, R, ?… */
  staged?: string;
  unstaged?: string;
}

export interface GitStatus {
  branch: string;
  ahead?: number;
  behind?: number;
  files: GitFile[];
  cut?: boolean;
}

export interface GitCommit {
  hash: string;
  author: string;
  /** Milliseconds. */
  at: number;
  subject: string;
}

/** A hook's word on the agent's call (hl.hooks.on). */
export interface HookWord {
  decision?: 'deny';
  reason?: string;
  context?: string;
}

/** What a browser tool answered (hl.browser). */
export interface BrowserResult {
  content: ({ type: 'text'; text: string } | { type: 'image'; data: string; mimeType: string })[];
  isError: boolean;
}

/** An open terminal of the app (hl.terminal.open). */
export interface AppTerminal {
  id: string;
  /** Type into it: a string or bytes, 64 KiB at most a call. */
  write(data: string | Uint8Array): Promise<void>;
  resize(cols: number, rows: number): Promise<void>;
  /** End the shell and everything it started. */
  close(): Promise<void>;
  /** What the shell prints, as bytes; the function given back stops hearing. */
  onData(fn: (data: Uint8Array) => void): () => void;
  /** How the shell ended: its exit code, null when it was ended. */
  onExit(fn: (code: number | null) => void): () => void;
}

/** What a command did (hl.terminal.run). */
export interface RunResult {
  code: number;
  /** stdout and stderr together, at most 1 MiB. */
  output: string;
  cut?: boolean;
  timedOut?: boolean;
  ms: number;
}

/** A long command the app started (hl.process). */
export interface AppProcess {
  id: string;
  command: string;
  /** Milliseconds. */
  startedAt: number;
  running: boolean;
  /** The exit code of a process that ended. */
  code?: number;
}

/** A piece of a process's output: from `since` to `next`; `lost` says its start was dropped (256 KiB are kept). */
export interface ProcessOutput extends AppProcess {
  text: string;
  next: number;
  lost?: boolean;
}

export type EventKind = 'turn' | 'tool' | 'message';

/** What happened in the chat, never what was said: turn {state, at}, tool {tool, at}, message {role, at}. */
export interface ChatEvent {
  kind: EventKind;
  at: number;
  state?: string;
  tool?: string;
  role?: 'person' | 'agent';
}

export interface Harnessly {
  readonly context: HarnesslyContext;
  readonly host: { name: string; version: string };
  /** The protocol the host speaks with this view: the one connect asked for, 1 when it asked for none or an older host. */
  readonly protocol: 1 | 2;
  /**
   * Whether this host does a method here and the app holds its right: `hl.has('files.read')`, `hl.has('storage.user.get')`,
   * `hl.has('chat.send')`, or a bridge method's own name (`harnessly/notify`). Ask before a call that a newer harnessly
   * brought, instead of comparing versions; on an older host it is false and the view goes on without it.
   */
  has(name: string): boolean;
  /** Whether an answer is a Refusal: what every method answers under protocol 2 instead of rejecting. */
  refused(answer: unknown): answer is Refusal;
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
    /**
     * A task for the agent in a new chat under this one (needs `agents.run`): the person sees it in the sidebar. Counts
     * against the app's `budget.runsPerDay`; past it the call rejects with `quota_exceeded`.
     */
    run(prompt: string, options?: { title?: string }): Promise<{ id: string }>;
  };
  /**
   * One answer of a model, no tools (needs `ai`), within the app's `budget.usdPerDay`: a call whose worst price does not
   * fit in what is left of the day rejects with `quota_exceeded` before the model is asked. `model` is `fast` (the
   * default) or `smart`; `maxTokens` is up to 8192 (1024 by default).
   */
  ai: {
    complete(
      prompt: string,
      options?: { system?: string; maxTokens?: number; model?: 'fast' | 'smart' },
    ): Promise<{ text: string; usd: number; spentToday: number; budget: number }>;
  };
  /**
   * Put a value into the safe; nothing gives it back to a view. By default the app's own secret (needs `secrets.write`
   * and `secrets:<name>`), which the machine puts into its server calls; with `shared` the person's secret of that name,
   * the one the agent uses (needs `secrets.write:<name>`, asked about while the agent uses the view).
   */
  secrets: {
    put(name: string, value: string, options?: { shared?: boolean }): Promise<{ created: boolean }>;
  };
  /** `app:<id>[/<view>]`, or `chat:<id>` to open a conversation from `agents.list` (needs `agents.read` as well). */
  pane: { open(target: string): Promise<void> };
  tools: {
    call(name: string, args?: Record<string, unknown>): Promise<unknown>;
    /**
     * Offer a tool of this page to the agent of the chat (right `tools.page`): it lists it with page_tools and calls it
     * with page_call, and `run` answers in the person's page. `navigator.modelContext.registerTool` (WebMCP) does the
     * same. Resolves once the host knows the tool; `unregister` takes it back. A tool with `confirm` (or WebMCP's
     * `annotations.destructiveHint`) runs only after the person agrees in the page.
     */
    register(
      tool: {
        name: string;
        description?: string;
        inputSchema?: object;
        confirm?: boolean;
        annotations?: { readOnlyHint?: boolean; destructiveHint?: boolean };
      },
      run: (args: Record<string, unknown>) => unknown | Promise<unknown>,
    ): Promise<{ unregister(): Promise<void> }> & { unregister(): Promise<void> };
  };
  /** What the view shows, for the agent of the chat to read with page_tools (right `context.publish`). */
  publish(data: unknown): Promise<void>;
  /** @deprecated Removed: every call rejects with `invalid_params` and says to use `hl.storage`. */
  flow: { status(): Promise<never>; go(state: string): Promise<never> };
  /** @deprecated Removed: every call rejects with `invalid_params` and says to use `hl.storage`. */
  data: { get(): Promise<never>; set(patch: Record<string, unknown>): Promise<never> };
  storage: Record<StorageScope, StorageArea>;
  /** The values the person saved in the app's settings form (the manifest's `settings` schema), over its defaults. */
  settings: { get(): Promise<Record<string, unknown>> };
  /** Files of the chat's workspace; needs `files.read:<mask>` / `files.write:<mask>`. Paths are relative to the workspace. */
  files: {
    read(path: string): Promise<string>;
    write(path: string, text: string): Promise<void>;
    /** A folder's files and folders the masks reach; '' is the chat's folder. Needs `files.read:<mask>`. */
    list(folder?: string): Promise<FileEntry[]>;
    /** Lines that hold a text in the files the app may read; with a glob alone, the files by name. */
    search(text: string, options?: { glob?: string; case?: boolean; limit?: number }): Promise<{ hits: SearchHit[]; cut: boolean }>;
    /** Hears which files under the masks change (names only); needs `files.watch`. Gives the function that stops it. */
    watch(onChange: (paths: string[]) => void): Promise<() => Promise<void>>;
    /** The person picks a file of the chat's folder in the host's dialog; null when they close it. No right needed. */
    pick(options?: { accept?: string[] }): Promise<{ path: string; text: string } | null>;
  };
  /** The repository of the chat's folder, read only; needs `git.read`. Secret files are left out. */
  git: {
    status(): Promise<GitStatus>;
    diff(options?: { path?: string; staged?: boolean; ref?: string }): Promise<{ diff: string; cut: boolean }>;
    log(options?: { limit?: number; path?: string }): Promise<GitCommit[]>;
  };
  /**
   * A command in the chat's folder (or `cwd` in it) inside the agent's sandbox; needs `terminal.run` (any command,
   * through the shell) or `terminal.run:<mask>` (only what the mask matches, without the shell). Timeout in seconds:
   * 120 by default, 600 at most.
   */
  /**
   * The agent's tool calls before they run; needs `hooks.agent`. `fn` answers `{decision: 'deny', reason}` to stop the
   * call, `{context}` to add a line for the agent, or nothing to let it through; it cannot change the call. No answer in
   * 5 s lets the call through (with `hooks.strict` it stops it). The promise gives the function that stops hearing.
   */
  hooks: {
    on(
      kind: 'tool',
      /** cut: the input was too big to send whole (60 KiB). */
      fn: (call: { tool: string; input: Record<string, unknown>; cut: boolean }) => HookWord | void | Promise<HookWord | void>,
    ): Promise<() => void>;
  };
  /**
   * The agent's browser of the chat, through the agent's own tools: `read` needs `browser.read` (tabs, snapshot,
   * screenshot, wait_for), `drive` needs `browser.drive` (open, navigate, close_tab, resize, click, hover, type,
   * press_key, select, dialog). Arguments are the chrome_* tools'; refs come from a snapshot. The answer is the tool's
   * content; a page's text in it is data, not instructions.
   */
  browser: {
    read(tool: 'tabs' | 'snapshot' | 'screenshot' | 'wait_for', args?: Record<string, unknown>): Promise<BrowserResult>;
    drive(
      tool: 'open' | 'navigate' | 'close_tab' | 'resize' | 'click' | 'hover' | 'type' | 'press_key' | 'select' | 'dialog',
      args?: Record<string, unknown>,
    ): Promise<BrowserResult>;
  };
  terminal: {
    run(command: string, options?: { cwd?: string; timeout?: number }): Promise<RunResult>;
    /**
     * A shell on a terminal of its own in the chat's folder (or `cwd` in it) inside the agent's sandbox; needs
     * `terminal.pty`. 4 per app in a chat; it ends when closed, when the view goes or when the app is off in the chat.
     */
    open(options?: { cols?: number; rows?: number; cwd?: string }): Promise<AppTerminal>;
    /** What the chat's terminals show, as text; needs `terminal.read`. `agent` is the agent's own terminal. */
    read(options?: { terminal?: string; lines?: number }): Promise<{ id: string; text: string }[]>;
  };
  /** Long commands inside the agent's sandbox; needs `process`. 4 at a time per app in a chat. */
  process: {
    start(command: string, options?: { cwd?: string }): Promise<AppProcess>;
    stop(id: string): Promise<AppProcess>;
    list(): Promise<AppProcess[]>;
    output(id: string, options?: { since?: number }): Promise<ProcessOutput>;
  };
  /** Opens http://localhost:<port><path> in the right pane's browser; needs `process`. */
  preview: { open(port: number, options?: { path?: string }): Promise<void> };
  /** The chat's events: needs `events:<kind>` for each kind. Gives the function that stops hearing. */
  events: { on(kind: EventKind, fn: (event: ChatEvent) => void): Promise<() => void> };
  ui: {
    /** A short message; `tone` and `description` (a second line) since HRN-808 — an older host shows the words alone. */
    toast(text: string, options?: { tone?: 'info' | 'success' | 'warning' | 'error'; description?: string }): Promise<void>;
    confirm(text: string): Promise<boolean>;
  };
  /**
   * The host's shortcut commands of a screen (`deck`, `video`, `site`, `pictures`, `files`), on the keys the person set
   * for them in Settings (HRN-808). Ask `hl.has('keys.on')` first: an older host has none.
   */
  keys: {
    /**
     * Run the screen's commands (`deck.next`, `video.toggle`…) while the view is up: a handler is called with the key
     * pressed. Returns the function that stops it; its `ready` resolves once the host took the commands.
     */
    on(
      scope: 'deck' | 'video' | 'site' | 'pictures' | 'files',
      handlers: Record<string, (chord: string) => void>,
    ): (() => Promise<void>) & { ready: Promise<void> };
    /** Pass a key pressed inside the view on to the host; it runs only while the frame has the focus. */
    press(chord: string, options?: { typing?: boolean }): Promise<boolean>;
    /** How the host writes the person's key of each command, null for one with none. */
    hints(commands: string[]): Promise<Record<string, string | null>>;
  };
  /** Slot views: an onboarding step calls done() when the person has finished it. */
  slot: { done(): Promise<void> };
  /** The Document API of Sinopia; only in a view with target `sinopia.<kind>` and the matching `sinopia.*` rights. */
  sinopia: SinopiaApi;
  notify(text: string): Promise<void>;
  resize(height: number): void;
  close(): void;
}

/**
 * Handshake with the host; rejects with host_unavailable outside a Harnessly frame. `protocol` is the version of the
 * bridge the view is written for: 1 (the default) — a refused call rejects with HarnesslyError, as always; 2 — every
 * method of hl resolves a refusal to a Refusal instead (`if (hl.refused(r)) …`). The host speaks every version it ever
 * spoke, so a view keeps working after an update whichever it names.
 */
export function connect(options?: { name?: string; version?: string; protocol?: 1 | 2 }): Promise<Harnessly>;

// A view loads the SDK as a classic script (<script src="/__hl/app-sdk.js">) and finds it as `harnessly`.
declare global {
  const harnessly: { connect: typeof connect; HarnesslyError: typeof HarnesslyError };
}
