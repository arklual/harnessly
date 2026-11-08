// The host side of an app view's bridge: MCP Apps (JSON-RPC 2.0 over postMessage: ui/initialize, tools/call,
// ui/message, notifications) plus the host's own methods under harnessly/. Pure: a frame component hands it the
// messages the window receives and the handlers that do the work; nothing here touches the DOM.
//
// A message is believed only when it comes from the frame's own window AND from the origin its messages carry (a view
// runs sandboxed on an opaque origin, "null", so the window is what tells frames apart), then checked against what the app's manifest grants. The same table is held by the runtime
// (internal/apps/bridge.go), which checks the calls that reach the machine once more. Both are written from one
// description, internal/apps/surface.go: the table here is surface.gen.ts.

import type { ReactNode } from 'react';
import type { IModalProps } from '@/next/features/shell/modal';
import {
  BUILTIN_APPS,
  DEPRECATED_METHODS,
  METHOD_PERMISSION,
  PARAM_PERMISSIONS,
  PERMISSION_CLASS,
  SDK_PROTOCOLS,
  SINOPIA_METHODS,
} from './surface.gen';
import type { IAppTerminal, IAppTerminalEvents, IAppTerminalOpen } from './app-terminal';

export { METHOD_PERMISSION, SINOPIA_METHODS };

export type TBridgeCode =
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

const CODES: readonly string[] = [
  'permission_denied',
  'not_in_target',
  'policy_blocked',
  'network_denied',
  'host_not_allowed',
  'tls_untrusted',
  'upstream_error',
  'rate_limited',
  'host_unavailable',
  'invalid_params',
  'conflict',
  'quota_exceeded',
];

/** A refusal or failure with the code a view sees as HarnesslyError.code. */
export class BridgeError extends Error {
  constructor(
    readonly code: TBridgeCode,
    message?: string,
    /** More the view reads in the error's `data` next to the code (a `conflict` says the key's current `version`). */
    readonly extra?: Record<string, unknown>,
  ) {
    super(message ?? code);
  }
}

export const isBridgeCode = (v: unknown): v is TBridgeCode => typeof v === 'string' && CODES.includes(v);

/** A file moves as text, at most as much as the runtime takes (4 MB, internal/apps/files.go). */
const MAX_FILE_CHARS = 4 << 20;

export type TBridgeMethod = keyof typeof METHOD_PERMISSION;

type TSend = (message: unknown, transfer?: Transferable[]) => void;

const isSinopiaMethod = (m: string): m is TSinopiaMethod =>
  (SINOPIA_METHODS as readonly string[]).includes(m);

/** Whether a call writes to the document the view is shown in: its storage area, or its boards. */
const changesDocument = (method: TBridgeMethod, params: Record<string, unknown>): boolean =>
  ((method === 'harnessly/storage.set' || method === 'harnessly/storage.delete') && params.scope === 'doc') ||
  method === 'harnessly/sinopia.patch' ||
  method === 'harnessly/sinopia.setFields' ||
  method === 'harnessly/sinopia.boards.add';

const isMethod = (m: string): m is TBridgeMethod => Object.hasOwn(METHOD_PERMISSION, m);

/** Where a view is shown: a chat, a pane tab, an app tab, or a Sinopia document of a kind (`sinopia.slides`). */
export type TTarget =
  | 'chat'
  | 'pane'
  | 'app'
  | 'settings'
  | 'onboarding'
  | 'object-action'
  | 'hub-settings'
  | `sinopia.${string}`;

export type TSinopiaMethod = (typeof SINOPIA_METHODS)[number];

/** The wildcard aliases of the builtin apps' screens (`sinopia.*`), from their manifests. */
const WILDCARDS: readonly string[] = BUILTIN_APPS.flatMap((a) =>
  a.aliases.filter((alias: string) => alias.endsWith('.*')),
);

/**
 * Whether an app that lists these targets works in that one: a wildcard alias of a builtin app's screen (`sinopia.*`)
 * stands for every place under it (every Sinopia document kind).
 */
export const targetAllowed = (targets: readonly string[], target: string): boolean =>
  targets.includes(target) || WILDCARDS.some((w) => target.startsWith(w.slice(0, -1)) && targets.includes(w));

export interface IBridgeApp {
  id: string;
  version: string;
  permissions: readonly string[];
  targets: readonly string[];
}

/** What the view is told at the handshake and whenever it changes. */
export interface IViewContext {
  app: { id: string; version: string };
  target: TTarget;
  view: string;
  theme: { mode: 'light' | 'dark'; tokens: Record<string, string> };
  locale: string;
  chat: { id: string } | null;
  user: { name: string; role: string } | null;
  /** Set when the document the view is shown in may only be read: the view offers no edit, the host refuses one. */
  readOnly?: boolean;
  /** For a card: the data its tool returned. */
  data?: unknown;
}

/**
 * The context as MCP Apps spells it (`theme` is the mode, `styles.variables` the tokens, then locale, displayMode,
 * platform), so a standard view reads it unchanged; the whole of ours rides along under `harnessly` for the SDK.
 */
export const hostContextOf = (ctx: IViewContext) => ({
  theme: ctx.theme.mode,
  styles: { variables: ctx.theme.tokens },
  locale: ctx.locale,
  displayMode: 'inline' as const,
  platform: 'web' as const,
  harnessly: ctx,
});

export type TStorageScope = 'install' | 'user' | 'chat' | 'space' | 'doc';
const SCOPES: readonly string[] = ['install', 'user', 'chat', 'space', 'doc'];

/** The work behind the methods. A handler the host does not provide answers host_unavailable. */
/** A tool a view offers from its page, as it described it. */
export interface IPageTool {
  name: string;
  description: string;
  inputSchema?: unknown;
  /** The page asks the person before it runs the tool. */
  confirm?: boolean;
}

const PAGE_TOOL_NAME = /^[A-Za-z][A-Za-z0-9_.-]{0,63}$/;
const MAX_PAGE_TOOLS = 32;

export interface IBridgeHandlers {
  tool?: (name: string, args: Record<string, unknown>) => Promise<unknown>;
  storage?: (
    op: 'get' | 'set' | 'delete' | 'keys',
    scope: TStorageScope,
    key: string,
    value?: unknown,
    /** For a set: write only if the key is at this version, else `conflict`. */
    ifVersion?: number,
  ) => Promise<unknown>;
  /** The settings the person saved in the app's form, over the schema's defaults. */
  settings?: () => Promise<unknown>;
  chatSend?: (text: string) => Promise<void>;
  chatRead?: (last: number) => Promise<{ role: string; text: string }[]>;
  chatCard?: (view: string, data: unknown) => Promise<void>;
  paneOpen?: (target: string) => Promise<void>;
  /** The person's conversations on this machine: who and in what state, never what was said. */
  agents?: () => Promise<readonly object[]>;
  notify?: (text: string) => Promise<void>;
  /** A file of the chat's workspace; the runtime judges the path against the app's masks and answers for it. */
  files?: (op: 'read' | 'write', path: string, text?: string) => Promise<string | undefined>;
  /** The workspace beyond one file: a folder, a search, the start and end of a watch. */
  filesMore?: (op: TFilesOp, params: Record<string, unknown>) => Promise<unknown>;
  /** The person picks a file in the host's dialog; null when they close it. */
  pick?: (accept: readonly string[]) => Promise<{ path: string; text: string } | null>;
  /** The repository of the chat's folder, read only. */
  git?: (op: TGitOp, params: Record<string, unknown>) => Promise<unknown>;
  /** Commands and processes of the app in the chat's folder, inside the agent's sandbox. */
  run?: (op: TRunOp, params: Record<string, unknown>) => Promise<unknown>;
  /** An app's own terminal (terminal.pty): the page holds its socket to the runtime. */
  terminal?: (params: IAppTerminalOpen, on: IAppTerminalEvents) => Promise<IAppTerminal>;
  /** The view hears the agent's tool calls before they run (hooks.agent): reported with the page's tools. */
  hooks?: (on: boolean) => void;
  /** The agent's browser of the chat (browser.read, browser.drive): a tool of the right's and its arguments. */
  browser?: (params: {
    right: 'browser.read' | 'browser.drive';
    tool: string;
    args: Record<string, unknown>;
    confirmed?: boolean;
  }) => Promise<unknown>;
  /** One answer of a model within the app's budget (ai). */
  ai?: (params: {
    prompt: string;
    system: string;
    maxTokens: number;
    model: string;
    confirmed?: boolean;
  }) => Promise<unknown>;
  /** A value into the safe, never read back: the app's own secret or, shared, the person's (secrets.write). */
  secretsPut?: (params: {
    name: string;
    value: string;
    shared: boolean;
    confirmed?: boolean;
  }) => Promise<unknown>;
  /** A task in a new chat under this one, within the app's budget (agents.run). */
  agentsRun?: (params: { prompt: string; title: string; confirmed?: boolean }) => Promise<unknown>;
  /** What the chat's terminals show (terminal.read). */
  terminalRead?: (params: { terminal: string; lines: number }) => Promise<unknown>;
  /** A local port in the pane's browser, after the runtime has checked the right and journaled it. */
  previewOpen?: (port: number, path: string) => Promise<void>;
  /**
   * A high or critical call while the agent is using the view's tools: the host asks the person in a dialog of its own,
   * which the frame cannot draw or answer, so the agent never gets more through an app than the person allowed.
   */
  elevate?: (method: string, detail: string) => Promise<boolean>;
  toast?: (text: string, options: IToastOptions) => void;
  /**
   * A window of the host's own over the page (ui.modal): drawn by the host with the app's words and the app's content.
   * Only an app in the host's page has it (its content is its own components); a frame is not offered it.
   */
  modal?: (props: IModalProps) => ReactNode;
  /**
   * While the view is up, it runs the host's shortcut commands of a screen (keys.on): the host asks `handlers` on every
   * press of the person's keys for them; a handler that answers false leaves the press to the screen below. Returns
   * the function that stops it.
   */
  keys?: (scope: TAppKeyScope, handlers: () => TAppKeyHandlers) => () => void;
  /** A key pressed inside the view, run as the host's shortcut; true when one ran. */
  keyPress?: (chord: string, typing: boolean) => boolean;
  /** The keys the person has for the host's commands, as the host writes them. */
  keyHints?: (commands: readonly string[]) => Record<string, string | null>;
  /** A slot view (an onboarding step) says it is finished. */
  slotDone?: () => void;
  confirm?: (text: string) => Promise<boolean>;
  /** An error or console.error of the view, for the debug section (harnessly/debug.log). */
  debugLog?: (level: 'error' | 'warn', text: string) => void;
  /** The Document API of Sinopia: the runtime does the work and checks the right once more. */
  sinopia?: (method: TSinopiaMethod, params: Record<string, unknown>) => Promise<unknown>;
  /** The view's page tools, the whole list each time (tools.page). */
  pageTools?: (tools: readonly IPageTool[]) => void;
  /** The view's answer to a call of its tool. */
  pageToolResult?: (id: string, answer: { result?: unknown; error?: string }) => void;
  /** What the view published for the agent (context.publish). */
  publish?: (data: unknown) => void;
  openLink?: (url: string) => void;
  resize?: (height: number) => void;
}

/** The tone of a toast (harnessly/ui.toast). */
export const TOAST_TONES = ['info', 'success', 'warning', 'error'] as const;
export type TToastTone = (typeof TOAST_TONES)[number];
export interface IToastOptions {
  tone?: TToastTone;
  description?: string;
}

/** The screens whose shortcut commands a view may run (keys.on); the host's global ones and the composer's are not. */
export const APP_KEY_SCOPES = ['deck', 'video', 'site', 'pictures', 'files'] as const;
export type TAppKeyScope = (typeof APP_KEY_SCOPES)[number];
/** A command's handler: false when the press is not the view's after all. */
export type TAppKeyHandlers = Partial<Record<string, (chord: string) => void | false>>;
const KEY_COMMAND = /^[a-z]+\.[A-Za-z0-9]{1,40}$/;
const MAX_KEY_COMMANDS = 64;

export type TFilesOp = 'list' | 'search' | 'watch' | 'unwatch';
export type TGitOp = 'status' | 'diff' | 'log';
export type TRunOp = 'run' | 'start' | 'stop' | 'list' | 'output' | 'preview';

/** How long after the view answers an agent's call its high calls are still taken for the agent's. */
export const AGENT_AFTER_MS = 30_000;
/** The longest an agent's call is held open for that. */
const AGENT_CALL_MS = 60_000;

/** What the person is asked about for a call the agent may have caused. */
/** The longest command the person is asked about: a longer one is refused rather than shown in part. */
export const MAX_ELEVATED_COMMAND = 1500;

/**
 * What the person is asked about for a call the agent may have caused: the whole command, as a JSON string, so a line
 * break or a control character shows as what it is. null when it cannot be shown whole: the call is then refused.
 */
/** What a view types into a terminal: a text, or bytes (a view of a buffer of any realm). */
const bytesOf = (v: unknown): Uint8Array | null => {
  if (typeof v === 'string') return new TextEncoder().encode(v);
  if (ArrayBuffer.isView(v)) return new Uint8Array(v.buffer, v.byteOffset, v.byteLength);
  return null;
};

const elevationDetail = (method: string, p: Record<string, unknown>): string | null => {
  // The name of the secret, never its value.
  if (method === 'harnessly/secrets.put') return typeof p.name === 'string' ? JSON.stringify(p.name) : null;
  if (method === 'harnessly/terminal.write') {
    const bytes = bytesOf(p.data);
    if (bytes === null) return null;
    const text = new TextDecoder().decode(bytes);
    return text.length > MAX_ELEVATED_COMMAND ? null : JSON.stringify(text);
  }
  if (method === 'harnessly/browser.read' || method === 'harnessly/browser.drive') {
    const shown = `${String(p.tool)} ${JSON.stringify(isRecord(p.args) ? p.args : {})}`;
    return shown.length > MAX_ELEVATED_COMMAND ? null : shown;
  }
  if (typeof p.command === 'string')
    return p.command.length > MAX_ELEVATED_COMMAND ? null : JSON.stringify(p.command);
  if (typeof p.prompt === 'string')
    return p.prompt.length > MAX_ELEVATED_COMMAND ? null : JSON.stringify(p.prompt);
  if (typeof p.port === 'number')
    return `localhost:${p.port}${typeof p.path === 'string' ? JSON.stringify(p.path).slice(1, -1) : ''}`;
  if (typeof p.id === 'string') return JSON.stringify(p.id);
  return method;
};
/** What events:<kind> names (internal/apps surface.go EventKinds). */
export const EVENT_KINDS = ['turn', 'tool', 'message'] as const;
export type TEventKind = (typeof EVENT_KINDS)[number];

/** The window the frame holds, as far as the bridge needs it. */
export interface IFrameWindow {
  postMessage: (message: unknown, targetOrigin: string, transfer?: Transferable[]) => void;
}

/** One end of a MessageChannel, as far as the bridge needs it. */
export interface IBridgePort {
  onmessage: ((event: { data: unknown }) => void) | null;
  postMessage: (message: unknown) => void;
  start?: () => void;
  close: () => void;
}

export interface IBridgeChannel {
  port1: IBridgePort;
  port2: unknown;
}

export interface IBridgeOptions {
  /** The frame's window now (null while it is not there). */
  frame: () => IFrameWindow | null;
  /** The origin the frame's messages carry: "null" for a view on an opaque origin (a sandbox without allow-same-origin). */
  origin: string;
  /** Where replies are addressed. An opaque origin cannot be named, so "*": the reply goes to the frame's own window. */
  replyTo?: string;
  app: IBridgeApp;
  context: IViewContext;
  handlers: IBridgeHandlers;
  hostVersion: string;
  /**
   * What the view's document presents in ui/initialize: the view server writes the session's nonce into its own pages,
   * so a document the frame navigated to (another page, another origin) cannot. Absent for a view at the app's own
   * address, whose pages the host does not serve.
   */
  nonce?: string;
  /** Makes the channel the calls move to after the handshake; the browser's MessageChannel unless a test gives one. */
  channel?: () => IBridgeChannel;
  now?: () => number;
  /** Every call, for the app inspector. */
  onCall?: (entry: IBridgeLogEntry) => void;
  /**
   * A builtin app: the host mounted it in its own page, so there is no frame and no handshake; it calls the
   * bridge with `call`, and every call goes through the same rights, checks and limits as a frame's.
   */
  inProcess?: boolean;
  /** A builtin app hears the bridge's notifications here (a call of its page tool, a key), in the same process. */
  onNotify?: (method: string, params: unknown) => void;
}

export interface IBridgeLogEntry {
  method: string;
  permission: string;
  ok: boolean;
  code?: TBridgeCode;
  /** Why the call failed, or what the view logged: for the debug section only. */
  message?: string;
  at: number;
}

export interface IMessageLike {
  source: unknown;
  origin: string;
  data: unknown;
}

/** Calls a view may make in a second. */
export const RATE_LIMIT = 60;
/** Typing into a terminal and its size are not counted: a paste or a resizing window makes many at once. */
const UNMETERED: ReadonlySet<string> = new Set(['harnessly/terminal.write', 'harnessly/terminal.resize']);
/** The most one write into a terminal carries. */
export const MAX_TERMINAL_WRITE = 64 * 1024;
/** How many terminals a view keeps open (the runtime counts 4 per app in a chat as well). */
const MAX_VIEW_TERMINALS = 4;
const RATE_WINDOW_MS = 1000;
const PROTOCOL_VERSION = '2025-11-21';
const MAX_CHAT_READ = 200;

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);

/** JSON-RPC error codes: ours go in data.code, which the SDK turns into HarnesslyError.code. */
const RPC_ERROR = {
  methodNotFound: -32601,
  invalidParams: -32602,
  internal: -32603,
  refused: -32000,
} as const;

export class AppBridge {
  private initialized = false;
  /** ui/initialize passed (with the nonce, where there is one); nothing else is answered before. */
  private handshaken = false;
  /** The protocol the view named with harnessly/protocol (1 when it named none): new behavior is only for a view that asked. */
  private protocol = 1;
  private disposed = false;
  /**
   * Our end of the channel made at the handshake. The window races: a document the frame navigated to can post before
   * the host hears its `load`. The port goes only to the document that presented the nonce, so once it is open the
   * frame's window is not heard at all, and what comes on the port is that document's.
   */
  private port: IBridgePort | null = null;
  private calls: number[] = [];
  private context: IViewContext;
  private readonly now: () => number;
  private readonly replyTo: string;

  constructor(private readonly opts: IBridgeOptions) {
    this.context = opts.context;
    this.now = opts.now ?? Date.now;
    this.replyTo = opts.replyTo ?? opts.origin;
    if (opts.inProcess === true) {
      this.handshaken = true;
      this.initialized = true;
    }
  }

  /**
   * A call of a builtin app, in the page's own process: the answer, or the bridge's refusal as a BridgeError. A bridge
   * of a frame takes no call this way.
   */
  call(method: string, params: Record<string, unknown> = {}): Promise<unknown> {
    if (this.opts.inProcess !== true || this.disposed)
      return Promise.reject(new BridgeError('host_unavailable', 'not a bridge of a builtin app'));
    return new Promise((resolve, reject) => {
      void this.dispatch(
        (body) => {
          if (!isRecord(body)) return;
          if ('result' in body) resolve(body.result);
          else {
            const error = isRecord(body.error) ? body.error : {};
            const data = isRecord(error.data) ? error.data : {};
            const message = typeof error.message === 'string' ? error.message : undefined;
            reject(new BridgeError(isBridgeCode(data.code) ? data.code : 'invalid_params', message));
          }
        },
        0,
        method,
        params,
      );
    });
  }

  /** Feed it every `message` event of the page window. Returns whether the message was this view's. */
  onMessage(event: IMessageLike): boolean {
    if (this.disposed || this.port !== null) return false;
    const frame = this.opts.frame();
    // The window and the origin must both be the frame's: any other window (a sibling frame, the page's opener) or the
    // same window on another origin (the frame navigated away) is not heard.
    if (frame === null || event.source !== frame || event.origin !== this.opts.origin) return false;
    const m = event.data;
    if (!isRecord(m) || m.jsonrpc !== '2.0' || typeof m.method !== 'string') return false;
    const id = typeof m.id === 'number' || typeof m.id === 'string' ? m.id : undefined;
    const params = isRecord(m.params) ? m.params : {};
    void this.dispatch(
      (body, transfer) => frame.postMessage(body, this.replyTo, transfer),
      id,
      m.method,
      params,
    );
    return true;
  }

  private send(message: unknown): void {
    if (this.opts.inProcess === true) {
      if (!this.disposed && isRecord(message) && typeof message.method === 'string')
        this.opts.onNotify?.(message.method, message.params);
      return;
    }
    if (this.port) this.port.postMessage(message);
    else this.opts.frame()?.postMessage(message, this.replyTo);
  }

  /** A notification to the view: a new context, a tool result, a flow step. */
  notify(method: string, params: unknown): void {
    this.send({ jsonrpc: '2.0', method, params });
  }

  /** The view's open terminals, by the id the bridge gave each. */
  private terminals = new Map<string, IAppTerminal>();
  private terminalSeq = 0;
  private terminalsOpening = 0;

  /** The open terminal of an id the view gives. */
  private terminalOf(p: Record<string, unknown>): IAppTerminal {
    const term = typeof p.id === 'string' ? this.terminals.get(p.id) : undefined;
    if (!term) throw new BridgeError('invalid_params', 'no open terminal of this id');
    return term;
  }

  /** The kinds of the chat's events the view asked to hear (and the app holds events:<kind> for). */
  private heard = new Set<TEventKind>();

  /** The agent's calls of the view's tools now running, with when each began; and when the last one was answered. */
  private agentCalls = new Map<string, number>();
  private agentAnswered = Number.NEGATIVE_INFINITY;

  /** The agent called a tool of the view (page_call): until it is answered, high calls are asked about. */
  agentCall(id: string): void {
    this.agentCalls.set(id, this.now());
  }

  /** Whether a call now may be the agent's doing: a call of the view's tool runs, or was answered moments ago. */
  private agentActive(): boolean {
    const now = this.now();
    for (const [id, at] of this.agentCalls) if (now - at > AGENT_CALL_MS) this.agentCalls.delete(id);
    return this.agentCalls.size > 0 || now - this.agentAnswered < AGENT_AFTER_MS;
  }

  /** An event of the chat: told to the view when it subscribed to the kind; its content never goes with it. */
  event(kind: TEventKind, params: Record<string, unknown>): void {
    if (this.initialized && this.heard.has(kind))
      this.notify('harnessly/notifications/event', { kind, ...params });
  }

  /** Files of a watch changed: their names only. */
  filesChanged(paths: readonly string[]): void {
    if (this.initialized) this.notify('harnessly/notifications/files-changed', { paths });
  }

  setContext(next: IViewContext): void {
    this.context = next;
    if (this.initialized) this.notify('ui/notifications/host-context-changed', hostContextOf(next));
  }

  /** The screens whose shortcut commands the view runs now, with the function that stops each. */
  private keyScopes = new Map<TAppKeyScope, () => void>();

  /**
   * A builtin app runs a screen's shortcut commands with its own handlers, in the same process: the press is answered
   * at once, so a handler may still leave it to the screen below. Checked as harnessly/keys.on is.
   */
  listenKeys(scope: TAppKeyScope, handlers: () => TAppKeyHandlers): () => void {
    if (this.opts.inProcess !== true || this.disposed) return () => undefined;
    return this.startKeys(scope, handlers);
  }

  private startKeys(scope: TAppKeyScope, handlers: () => TAppKeyHandlers): () => void {
    if (!APP_KEY_SCOPES.includes(scope))
      throw new BridgeError('invalid_params', `scope is ${APP_KEY_SCOPES.join(', ')}`);
    this.keyScopes.get(scope)?.();
    const stop = this.need('keys')(scope, handlers);
    const off = () => {
      if (this.keyScopes.get(scope) !== off) return;
      this.keyScopes.delete(scope);
      stop();
    };
    this.keyScopes.set(scope, off);
    return off;
  }

  dispose(): void {
    for (const off of [...this.keyScopes.values()]) off();
    this.disposed = true;
    for (const term of this.terminals.values()) term.close();
    this.terminals.clear();
    this.port?.close();
    this.port = null;
  }

  private reply(
    send: TSend,
    id: number | string,
    body: { result: unknown } | { error: unknown },
    transfer?: Transferable[],
  ): void {
    send({ jsonrpc: '2.0', id, ...body }, transfer);
  }

  private fail(
    frame: TSend,
    id: number | string | undefined,
    code: TBridgeCode,
    message: string,
    extra?: Record<string, unknown>,
  ): void {
    if (id === undefined) return;
    const rpc = code === 'invalid_params' ? RPC_ERROR.invalidParams : RPC_ERROR.refused;
    this.reply(frame, id, { error: { code: rpc, message, data: { ...extra, code } } });
  }

  private limited(): boolean {
    const t = this.now();
    this.calls = this.calls.filter((at) => t - at < RATE_WINDOW_MS);
    if (this.calls.length >= RATE_LIMIT) return true;
    this.calls.push(t);
    return false;
  }

  private log(method: string, permission: string, error: BridgeError | null): void {
    // The view's own log lines are written by the debugLog handler, not as calls.
    if (method === 'harnessly/debug.log' && error === null) return;
    this.opts.onCall?.({
      method,
      permission,
      ok: error === null,
      code: error?.code,
      ...(error?.message ? { message: error.message.slice(0, 500) } : {}),
      at: this.now(),
    });
  }

  /** The view made the handshake and said it is ready. */
  get ready(): boolean {
    return this.initialized;
  }

  private async dispatch(
    frame: TSend,
    id: number | string | undefined,
    method: string,
    params: Record<string, unknown>,
  ) {
    if (!isMethod(method)) {
      this.log(method, '', new BridgeError('invalid_params'));
      if (id !== undefined)
        this.reply(frame, id, { error: { code: RPC_ERROR.methodNotFound, message: `no method ${method}` } });
      return;
    }
    const need: string = METHOD_PERMISSION[method];
    try {
      if (!UNMETERED.has(method) && this.limited())
        throw new BridgeError('rate_limited', 'at most 60 calls a second');
      this.authorize(method, need, params);
      const asked = this.loudForAgent(method, need, params);
      if (asked) await this.elevation(method, params);
      const result = await this.run(method, params, asked);
      this.log(method, need, null);
      // A deprecated method works as it did; the inspector says what replaces it.
      if (Object.hasOwn(DEPRECATED_METHODS, method))
        this.opts.onCall?.({
          method,
          permission: need,
          ok: true,
          message: `deprecated: use ${DEPRECATED_METHODS[method]}`,
          at: this.now(),
        });
      if (method === 'ui/initialize' && id !== undefined && !this.disposed && this.port === null) {
        // The handshake passed (the nonce is the mounted document's): the other end of a fresh channel goes with the
        // answer, and from here the calls come on ours.
        const channel = (this.opts.channel ?? (() => new MessageChannel() as unknown as IBridgeChannel))();
        this.port = channel.port1;
        channel.port1.onmessage = (event) => this.onPort(event.data);
        channel.port1.start?.();
        this.reply(frame, id, { result }, [channel.port2 as Transferable]);
      } else if (id !== undefined) this.reply(frame, id, { result: result ?? {} });
    } catch (e) {
      const err = this.asBridgeError(e);
      this.log(method, need, err);
      this.fail(frame, id, err.code, err.message, err.extra);
    }
  }

  private onPort(m: unknown): void {
    if (this.disposed || !isRecord(m) || m.jsonrpc !== '2.0' || typeof m.method !== 'string') return;
    const id = typeof m.id === 'number' || typeof m.id === 'string' ? m.id : undefined;
    const params = isRecord(m.params) ? m.params : {};
    void this.dispatch((body) => this.port?.postMessage(body), id, m.method, params);
  }

  private asBridgeError(e: unknown): BridgeError {
    if (e instanceof BridgeError) return e;
    const code = isRecord(e) ? e.code : undefined;
    return new BridgeError(
      isBridgeCode(code) ? code : 'host_unavailable',
      e instanceof Error ? e.message : undefined,
    );
  }

  private authorize(method: TBridgeMethod, need: string, params: Record<string, unknown>): void {
    const { app } = this.opts;
    if (method === 'ui/initialize') {
      const { nonce } = this.opts;
      if (nonce !== undefined && params.nonce !== nonce)
        throw new BridgeError('host_unavailable', 'not the view the host mounted');
    } else if (method === 'ui/notifications/initialized') {
      if (!this.handshaken) throw new BridgeError('host_unavailable', 'ui/initialize first');
    } else if (method === 'harnessly/debug.log') {
      // A view's error before its handshake is the one most worth seeing.
    } else if (!this.initialized) throw new BridgeError('host_unavailable', 'ui/initialize first');
    if (!targetAllowed(app.targets, this.context.target))
      throw new BridgeError('not_in_target', `the app does not work in ${this.context.target}`);
    // files.read and files.write are held with a mask (files.read:docs/**); which paths they reach is the runtime's to judge.
    if (need !== '' && !this.holds(need))
      throw new BridgeError('permission_denied', `${method} needs the permission ${need}`);
    // A document open for reading is not changed through the bridge, whatever the view does: its own storage, the boards.
    if (this.context.readOnly === true && changesDocument(method, params))
      throw new BridgeError(
        'permission_denied',
        `${method} changes the document, which is open for reading only`,
      );
    // Opening a conversation (chat:<id>) shows the person's chats, which is what agents.read is for.
    if (
      method === 'harnessly/pane.open' &&
      typeof params.target === 'string' &&
      params.target.startsWith('chat:') &&
      !app.permissions.includes('agents.read')
    )
      throw new BridgeError('permission_denied', `${method} of chat:<id> needs the permission agents.read`);
    const writes = method === 'harnessly/storage.set' || method === 'harnessly/storage.delete';
    if (writes && params.scope === 'space' && !app.permissions.includes('storage.space.write'))
      throw new BridgeError(
        'permission_denied',
        `${method} on the space needs the permission storage.space.write`,
      );
  }

  /** The agent does not get more through an app than the person allowed: a loud call in its turn is asked about. */
  private loudForAgent(method: TBridgeMethod, need: string, params: Record<string, unknown>): boolean {
    if (need === '' || !Object.hasOwn(PERMISSION_CLASS, need)) return false;
    // secrets.write is the app's own keys; with a name, the person's secret the agent uses (surface.go ParamClass).
    const shared = method === 'harnessly/secrets.put' && params.shared === true;
    const cls: string = shared ? 'high' : PERMISSION_CLASS[need as keyof typeof PERMISSION_CLASS];
    if (cls !== 'high' && cls !== 'critical') return false;
    // Reading what the app's own processes print asks nothing: it changes nothing; nor do a terminal's size and end.
    if (
      method === 'harnessly/process.list' ||
      method === 'harnessly/process.output' ||
      method === 'harnessly/terminal.resize' ||
      method === 'harnessly/terminal.close'
    )
      return false;
    return this.agentActive();
  }

  private async elevation(method: TBridgeMethod, params: Record<string, unknown>): Promise<void> {
    const ask = this.opts.handlers.elevate;
    if (!ask)
      throw new BridgeError(
        'permission_denied',
        `${method} while the agent uses the view needs the person's yes`,
      );
    const detail = elevationDetail(method, params);
    if (detail === null)
      throw new BridgeError('permission_denied', 'the command is too long to show the person whole');
    if (!(await ask(method, detail)))
      throw new BridgeError('permission_denied', 'the person did not allow it');
  }

  /** The app holds the right; a right with a value (files.read:docs/**, events:turn) is held with any value. */
  private holds(need: string): boolean {
    return this.opts.app.permissions.some(
      (p) => p === need || (PARAM_PERMISSIONS.has(need) && p.startsWith(`${need}:`)),
    );
  }

  /** The methods this view may call here, for hl.has: those of no right and those whose right the app holds. */
  private callable(): string[] {
    return Object.entries(METHOD_PERMISSION)
      .filter(([, need]) => need === '' || this.holds(need))
      .map(([name]) => name);
  }

  private need<K extends keyof IBridgeHandlers>(name: K): NonNullable<IBridgeHandlers[K]> {
    const h = this.opts.handlers[name];
    if (!h) throw new BridgeError('host_unavailable', 'the host does not do this here');
    return h as NonNullable<IBridgeHandlers[K]>;
  }

  /** `confirmed`: the person said yes to this call in the host's dialog (the runtime asks for it while the agent drives). */
  private async run(method: TBridgeMethod, p: Record<string, unknown>, confirmed = false): Promise<unknown> {
    const yes = confirmed ? { confirmed: true } : {};
    const str = (key: string, max = 100_000): string => {
      const v = p[key];
      if (typeof v !== 'string' || v === '' || v.length > max)
        throw new BridgeError('invalid_params', `${key} is a text`);
      return v;
    };
    const scope = (): TStorageScope => {
      const v = p.scope;
      if (typeof v !== 'string' || !SCOPES.includes(v))
        throw new BridgeError('invalid_params', 'scope is install, user, chat, space or doc');
      return v as TStorageScope;
    };
    switch (method) {
      case 'ui/initialize': {
        this.handshaken = true;
        return {
          protocolVersion: PROTOCOL_VERSION,
          hostInfo: { name: 'harnessly', version: this.opts.hostVersion },
          hostCapabilities: {
            serverTools: {},
            message: {},
            openLinks: {},
            harnessly: { protocols: SDK_PROTOCOLS, methods: this.callable() },
          },
          hostContext: hostContextOf(this.context),
        };
      }
      case 'harnessly/protocol': {
        const asked = p.protocol;
        this.protocol =
          typeof asked === 'number' && (SDK_PROTOCOLS as readonly number[]).includes(asked) ? asked : 1;
        return { protocol: this.protocol };
      }
      case 'ui/notifications/initialized':
        this.initialized = true;
        return undefined;
      case 'ui/notifications/size-changed': {
        const h = p.height;
        if (typeof h === 'number' && Number.isFinite(h) && h >= 0) this.opts.handlers.resize?.(h);
        return undefined;
      }
      case 'ui/open-link': {
        const url = str('url', 2048);
        // Only a web address leaves the view; javascript:, data: and the like never do.
        if (!/^https:\/\//i.test(url)) throw new BridgeError('invalid_params', 'url is an https address');
        this.need('openLink')(url);
        return undefined;
      }
      case 'tools/call': {
        const args = isRecord(p.arguments) ? p.arguments : {};
        return this.need('tool')(str('name', 200), args);
      }
      case 'ui/message': {
        const content = Array.isArray(p.content) ? p.content : [];
        const first = content[0];
        const text =
          isRecord(first) && first.type === 'text' && typeof first.text === 'string' ? first.text : '';
        if (text === '' || text.length > 20_000)
          throw new BridgeError('invalid_params', 'content is one text');
        // The view names no writer: the host prefixes the app's real name.
        await this.need('chatSend')(text);
        return {};
      }
      case 'harnessly/chat.read': {
        const last =
          typeof p.last === 'number' && Number.isInteger(p.last)
            ? Math.min(MAX_CHAT_READ, Math.max(1, p.last))
            : 20;
        return { messages: await this.need('chatRead')(last) };
      }
      case 'harnessly/chat.card':
        await this.need('chatCard')(str('view', 80), p.data);
        return {};
      case 'harnessly/pane.open':
        await this.need('paneOpen')(str('target', 2048));
        return {};
      case 'harnessly/agents.list':
        return { agents: await this.need('agents')() };
      case 'harnessly/storage.get': {
        const read = (await this.need('storage')('get', scope(), str('key', 256))) as
          { value?: unknown; version?: number } | undefined;
        return { value: read?.value ?? null, version: read?.version ?? 0 };
      }
      case 'harnessly/storage.set': {
        const ifVersion = p.ifVersion;
        if (ifVersion !== undefined && (!Number.isSafeInteger(ifVersion) || (ifVersion as number) < 0))
          throw new BridgeError('invalid_params', 'ifVersion is a whole number, 0 or more');
        const wrote = (await this.need('storage')(
          'set',
          scope(),
          str('key', 256),
          p.value,
          ifVersion as number | undefined,
        )) as { version?: number } | undefined;
        return { version: wrote?.version ?? 0 };
      }
      case 'harnessly/storage.delete':
        await this.need('storage')('delete', scope(), str('key', 256));
        return {};
      case 'harnessly/storage.keys':
        return { keys: await this.need('storage')('keys', scope(), '-') };
      case 'harnessly/settings.get':
        return { value: (await this.need('settings')()) ?? {} };
      case 'harnessly/notify':
        await this.need('notify')(str('text', 2000));
        return {};
      case 'harnessly/files.read':
        return { text: await this.need('files')('read', str('path', 4096)) };
      case 'harnessly/files.write': {
        if (typeof p.text !== 'string' || p.text.length > MAX_FILE_CHARS)
          throw new BridgeError('invalid_params', 'text is what to write, up to 4 MB');
        await this.need('files')('write', str('path', 4096), p.text);
        return {};
      }
      case 'harnessly/files.list':
        return this.need('filesMore')('list', {
          path: typeof p.path === 'string' ? p.path.slice(0, 4096) : '',
        });
      case 'harnessly/files.search': {
        const query = typeof p.text === 'string' ? p.text.slice(0, 1000) : '';
        const glob = typeof p.glob === 'string' ? p.glob.slice(0, 4096) : '';
        if (query === '' && glob === '')
          throw new BridgeError('invalid_params', 'say what to look for: a text, a glob or both');
        return this.need('filesMore')('search', {
          query,
          glob,
          case: p.case === true,
          limit: typeof p.limit === 'number' ? p.limit : 0,
        });
      }
      case 'harnessly/files.watch':
        await this.need('filesMore')('watch', {});
        return {};
      case 'harnessly/files.unwatch':
        await this.need('filesMore')('unwatch', {});
        return {};
      case 'harnessly/files.pick': {
        const accept = Array.isArray(p.accept)
          ? p.accept.filter((a): a is string => typeof a === 'string').slice(0, 20)
          : [];
        return { file: await this.need('pick')(accept) };
      }
      case 'harnessly/git.status':
        return this.need('git')('status', {});
      case 'harnessly/git.diff':
        return this.need('git')('diff', {
          path: typeof p.path === 'string' ? p.path.slice(0, 4096) : '',
          staged: p.staged === true,
          ref: typeof p.ref === 'string' ? p.ref.slice(0, 100) : '',
        });
      case 'harnessly/git.log':
        return this.need('git')('log', {
          path: typeof p.path === 'string' ? p.path.slice(0, 4096) : '',
          limit: typeof p.limit === 'number' ? p.limit : 0,
        });
      case 'harnessly/terminal.run':
        return this.need('run')('run', {
          ...yes,
          command: str('command', 4096),
          cwd: typeof p.cwd === 'string' ? p.cwd.slice(0, 4096) : '',
          timeout:
            typeof p.timeout === 'number' && Number.isFinite(p.timeout)
              ? Math.max(0, Math.round(p.timeout))
              : 0,
        });
      case 'harnessly/process.start':
        return this.need('run')('start', {
          ...yes,
          command: str('command', 4096),
          cwd: typeof p.cwd === 'string' ? p.cwd.slice(0, 4096) : '',
        });
      case 'harnessly/process.stop':
        return this.need('run')('stop', { ...yes, id: str('id', 100) });
      case 'harnessly/process.list':
        return this.need('run')('list', {});
      case 'harnessly/process.output':
        return this.need('run')('output', {
          id: str('id', 100),
          since:
            typeof p.since === 'number' && Number.isFinite(p.since) ? Math.max(0, Math.floor(p.since)) : 0,
        });
      case 'harnessly/terminal.open': {
        // The place is held before the socket opens, so opens at once do not pass the limit together.
        if (this.terminals.size + this.terminalsOpening >= MAX_VIEW_TERMINALS)
          throw new BridgeError('rate_limited', 'too many terminals open: close one first');
        const size = (key: string, max: number): number => {
          const v = p[key];
          return typeof v === 'number' && Number.isInteger(v) && v >= 1 && v <= max
            ? v
            : key === 'cols'
              ? 80
              : 24;
        };
        const id = `t${++this.terminalSeq}`;
        let ended = false;
        this.terminalsOpening++;
        const term = await this.need('terminal')(
          {
            cols: size('cols', 1000),
            rows: size('rows', 500),
            cwd: typeof p.cwd === 'string' ? p.cwd.slice(0, 4096) : '',
            ...yes,
          },
          {
            data: (bytes) => this.notify('harnessly/notifications/terminal-output', { id, data: bytes }),
            exit: (code) => {
              ended = true;
              this.terminals.delete(id);
              this.notify('harnessly/notifications/terminal-exit', { id, code });
            },
          },
        ).finally(() => this.terminalsOpening--);
        if (this.disposed) {
          term.close();
          throw new BridgeError('host_unavailable', 'the view is gone');
        }
        if (!ended) this.terminals.set(id, term);
        return { id };
      }
      case 'harnessly/terminal.write': {
        const term = this.terminalOf(p);
        const bytes = bytesOf(p.data);
        if (bytes === null || bytes.length > MAX_TERMINAL_WRITE)
          throw new BridgeError('invalid_params', 'data is a text or bytes, 64 KiB at most');
        term.write(bytes);
        return {};
      }
      case 'harnessly/terminal.resize': {
        const term = this.terminalOf(p);
        const { cols, rows } = p;
        if (
          typeof cols !== 'number' ||
          typeof rows !== 'number' ||
          !Number.isInteger(cols) ||
          !Number.isInteger(rows) ||
          cols < 1 ||
          rows < 1 ||
          cols > 1000 ||
          rows > 500
        )
          throw new BridgeError('invalid_params', 'cols is from 1 to 1000, rows from 1 to 500');
        term.resize(cols, rows);
        return {};
      }
      case 'harnessly/terminal.close':
        this.terminalOf(p).close();
        return {};
      case 'harnessly/browser.read':
      case 'harnessly/browser.drive':
        if (p.args !== undefined && !isRecord(p.args))
          throw new BridgeError('invalid_params', 'args is an object');
        return this.need('browser')({
          right: method === 'harnessly/browser.read' ? 'browser.read' : 'browser.drive',
          tool: str('tool', 40),
          args: isRecord(p.args) ? p.args : {},
          ...yes,
        });
      case 'harnessly/ai.complete':
        return this.need('ai')({
          prompt: str('prompt', 20_000),
          system: p.system === undefined || p.system === '' ? '' : str('system', 4000),
          maxTokens:
            typeof p.maxTokens === 'number' && Number.isFinite(p.maxTokens) ? Math.floor(p.maxTokens) : 0,
          model: p.model === undefined || p.model === '' ? '' : str('model', 20),
          ...yes,
        });
      case 'harnessly/secrets.put':
        if (p.shared !== undefined && typeof p.shared !== 'boolean')
          throw new BridgeError('invalid_params', 'shared is true or false');
        return this.need('secretsPut')({
          name: str('name', 64),
          value: str('value', 65_536),
          shared: p.shared === true,
          ...yes,
        });
      case 'harnessly/agents.run':
        return this.need('agentsRun')({
          prompt: str('prompt', 20_000),
          title: p.title === undefined || p.title === '' ? '' : str('title', 120),
          ...yes,
        });
      case 'harnessly/terminal.read':
        return this.need('terminalRead')({
          terminal: typeof p.terminal === 'string' ? p.terminal.slice(0, 100) : '',
          lines:
            typeof p.lines === 'number' && Number.isFinite(p.lines) ? Math.max(0, Math.floor(p.lines)) : 0,
        });
      case 'harnessly/preview.open': {
        const port = p.port;
        if (typeof port !== 'number' || !Number.isInteger(port) || port < 1024 || port > 65535)
          throw new BridgeError('invalid_params', 'port is a local port from 1024 to 65535');
        const path = typeof p.path === 'string' && p.path.startsWith('/') ? p.path.slice(0, 2000) : '/';
        if (/\s/.test(path)) throw new BridgeError('invalid_params', 'path is a path of the address');
        await this.need('run')('preview', { ...yes, port });
        await this.need('previewOpen')(port, path);
        return {};
      }
      case 'harnessly/events.subscribe': {
        const kinds = Array.isArray(p.kinds) ? p.kinds : [];
        const held = kinds.filter(
          (k): k is TEventKind =>
            typeof k === 'string' &&
            (EVENT_KINDS as readonly string[]).includes(k) &&
            this.opts.app.permissions.includes(`events:${k}`),
        );
        if (held.length < kinds.length)
          throw new BridgeError(
            'permission_denied',
            'each kind needs its own events:<kind> (turn, tool, message)',
          );
        for (const k of held) this.heard.add(k);
        return { kinds: [...this.heard] };
      }
      case 'harnessly/ui.toast': {
        const tone =
          typeof p.tone === 'string' && (TOAST_TONES as readonly string[]).includes(p.tone)
            ? (p.tone as TToastTone)
            : undefined;
        const description =
          typeof p.description === 'string' && p.description !== '' ? p.description.slice(0, 500) : undefined;
        this.need('toast')(str('text', 500), {
          ...(tone ? { tone } : {}),
          ...(description ? { description } : {}),
        });
        return {};
      }
      case 'harnessly/keys.on': {
        const scope = str('scope', 20) as TAppKeyScope;
        const list = Array.isArray(p.commands) ? p.commands : [];
        if (list.length === 0 || list.length > MAX_KEY_COMMANDS)
          throw new BridgeError('invalid_params', `commands: 1 to ${MAX_KEY_COMMANDS} command ids`);
        const handlers: TAppKeyHandlers = {};
        for (const c of list) {
          // A view runs the commands of the screen it names, not another's.
          if (typeof c !== 'string' || !KEY_COMMAND.test(c) || !c.startsWith(`${scope}.`))
            throw new BridgeError('invalid_params', `a command is ${scope}.<name>`);
          handlers[c] = (chord) => this.notify('harnessly/notifications/key', { command: c, chord });
        }
        this.startKeys(scope, () => handlers);
        return {};
      }
      case 'harnessly/keys.off':
        this.keyScopes.get(str('scope', 20) as TAppKeyScope)?.();
        return {};
      case 'harnessly/keys.press':
        return { ran: this.need('keyPress')(str('chord', 60), p.typing === true) };
      case 'harnessly/keys.hints': {
        const list = Array.isArray(p.commands) ? p.commands : [];
        if (list.length > MAX_KEY_COMMANDS || list.some((c) => typeof c !== 'string' || !KEY_COMMAND.test(c)))
          throw new BridgeError('invalid_params', 'commands are command ids (deck.next)');
        return { hints: this.need('keyHints')(list as string[]) };
      }
      case 'harnessly/slot.done':
        this.need('slotDone')();
        return {};
      case 'harnessly/ui.confirm':
        return { ok: await this.need('confirm')(str('text', 500)) };
      case 'harnessly/debug.log': {
        const text = typeof p.text === 'string' ? p.text.slice(0, 2000) : '';
        if (text !== '') this.opts.handlers.debugLog?.(p.level === 'warn' ? 'warn' : 'error', text);
        return undefined;
      }
      case 'harnessly/tools.register': {
        const list = Array.isArray(p.tools) ? p.tools : [];
        if (list.length > MAX_PAGE_TOOLS)
          throw new BridgeError('invalid_params', `at most ${MAX_PAGE_TOOLS} tools`);
        const tools: IPageTool[] = [];
        for (const t of list) {
          if (!isRecord(t) || typeof t.name !== 'string' || !PAGE_TOOL_NAME.test(t.name))
            throw new BridgeError('invalid_params', 'a tool has a name of latin letters, digits, _ . -');
          const description = typeof t.description === 'string' ? t.description.slice(0, 1000) : '';
          tools.push({
            name: t.name,
            description,
            ...(isRecord(t.inputSchema) ? { inputSchema: t.inputSchema } : {}),
            ...(t.confirm === true ? { confirm: true } : {}),
          });
        }
        this.need('pageTools')(tools);
        return {};
      }
      case 'harnessly/hooks.subscribe':
        this.need('hooks')(true);
        return {};
      case 'harnessly/hooks.result': {
        const id = str('id', 100);
        if (this.agentCalls.delete(id)) this.agentAnswered = this.now();
        const word = {
          ...(p.decision === 'deny' ? { decision: 'deny' } : {}),
          ...(typeof p.reason === 'string' && p.reason !== '' ? { reason: p.reason.slice(0, 1000) } : {}),
          ...(typeof p.context === 'string' && p.context !== '' ? { context: p.context.slice(0, 4000) } : {}),
        };
        this.need('pageToolResult')(id, { result: word });
        return {};
      }
      case 'harnessly/tools.result': {
        const id = str('id', 100);
        if (this.agentCalls.delete(id)) this.agentAnswered = this.now();
        const error = typeof p.error === 'string' ? p.error.slice(0, 2000) : undefined;
        this.need('pageToolResult')(id, error === undefined ? { result: p.result ?? null } : { error });
        return {};
      }
      case 'harnessly/context.publish':
        this.need('publish')(p.data ?? null);
        return {};
      default: {
        // harnessly/sinopia.<method>: the bridge has checked the right; the params are the runtime's to read.
        const name = method.slice('harnessly/sinopia.'.length);
        if (!isSinopiaMethod(name)) throw new BridgeError('invalid_params', `no method ${method}`);
        return { result: (await this.need('sinopia')(name, p)) ?? null };
      }
    }
  }
}
