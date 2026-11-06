// The host side of an app view's bridge: MCP Apps (JSON-RPC 2.0 over postMessage: ui/initialize, tools/call,
// ui/message, notifications) plus the host's own methods under harnessly/. Pure: a frame component hands it the
// messages the window receives and the handlers that do the work; nothing here touches the DOM.
//
// A message is believed only when it comes from the frame's own window AND from the origin its messages carry (a view
// runs sandboxed on an opaque origin, "null", so the window is what tells frames apart), then checked against what the app's manifest grants. The same table is held by the runtime
// (internal/apps/bridge.go), which checks the calls that reach the machine once more.

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

/** The right each method needs; '' is a method that needs none. */
export const METHOD_PERMISSION = {
  'ui/initialize': '',
  'ui/notifications/initialized': '',
  'ui/notifications/size-changed': '',
  'ui/open-link': '',
  'tools/call': '',
  'ui/message': 'chat.prompt',
  'harnessly/chat.read': 'chat.read',
  'harnessly/chat.card': 'chat.card',
  'harnessly/pane.open': 'pane.open',
  'harnessly/agents.list': 'agents.read',
  'harnessly/storage.get': 'storage',
  'harnessly/storage.set': 'storage',
  'harnessly/storage.delete': 'storage',
  'harnessly/storage.keys': 'storage',
  'harnessly/settings.get': '',
  'harnessly/notify': 'notify',
  'harnessly/files.read': 'files.read',
  'harnessly/files.write': 'files.write',
  'harnessly/ui.toast': '',
  'harnessly/ui.confirm': '',
  'harnessly/slot.done': '',
  'harnessly/sinopia.document': 'sinopia.read',
  'harnessly/sinopia.selection': 'sinopia.read',
  'harnessly/sinopia.node': 'sinopia.read',
  'harnessly/sinopia.query': 'sinopia.read',
  'harnessly/sinopia.fields': 'sinopia.read',
  'harnessly/sinopia.mark': 'sinopia.read',
  'harnessly/sinopia.patch': 'sinopia.write',
  'harnessly/sinopia.setFields': 'sinopia.write',
  'harnessly/sinopia.boards.add': 'sinopia.write',
  'harnessly/sinopia.render': 'sinopia.export',
  'harnessly/sinopia.comment': 'sinopia.comment',
} as const satisfies Record<string, string>;

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

/** The Sinopia methods (the part after `harnessly/sinopia.`). */
export const SINOPIA_METHODS = [
  'document',
  'selection',
  'node',
  'query',
  'fields',
  'mark',
  'patch',
  'setFields',
  'boards.add',
  'render',
  'comment',
] as const;
export type TSinopiaMethod = (typeof SINOPIA_METHODS)[number];

/** Whether an app that lists these targets works in that one: `sinopia.*` stands for every document kind. */
export const targetAllowed = (targets: readonly string[], target: string): boolean =>
  targets.includes(target) || (target.startsWith('sinopia.') && targets.includes('sinopia.*'));

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
  toast?: (text: string) => void;
  /** A slot view (an onboarding step) says it is finished. */
  slotDone?: () => void;
  confirm?: (text: string) => Promise<boolean>;
  /** The Document API of Sinopia: the runtime does the work and checks the right once more. */
  sinopia?: (method: TSinopiaMethod, params: Record<string, unknown>) => Promise<unknown>;
  openLink?: (url: string) => void;
  resize?: (height: number) => void;
}

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
}

export interface IBridgeLogEntry {
  method: string;
  permission: string;
  ok: boolean;
  code?: TBridgeCode;
  at: number;
}

export interface IMessageLike {
  source: unknown;
  origin: string;
  data: unknown;
}

/** Calls a view may make in a second. */
export const RATE_LIMIT = 60;
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
    if (this.port) this.port.postMessage(message);
    else this.opts.frame()?.postMessage(message, this.replyTo);
  }

  /** A notification to the view: a new context, a tool result, a flow step. */
  notify(method: string, params: unknown): void {
    this.send({ jsonrpc: '2.0', method, params });
  }

  setContext(next: IViewContext): void {
    this.context = next;
    if (this.initialized) this.notify('ui/notifications/host-context-changed', hostContextOf(next));
  }

  dispose(): void {
    this.disposed = true;
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
    this.opts.onCall?.({ method, permission, ok: error === null, code: error?.code, at: this.now() });
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
      if (this.limited()) throw new BridgeError('rate_limited', 'at most 60 calls a second');
      this.authorize(method, need, params);
      const result = await this.run(method, params);
      this.log(method, need, null);
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
    } else if (!this.initialized) throw new BridgeError('host_unavailable', 'ui/initialize first');
    if (!targetAllowed(app.targets, this.context.target))
      throw new BridgeError('not_in_target', `the app does not work in ${this.context.target}`);
    // files.read and files.write are held with a mask (files.read:docs/**); which paths they reach is the runtime's to judge.
    const holds = app.permissions.some(
      (p) => p === need || ((need === 'files.read' || need === 'files.write') && p.startsWith(`${need}:`)),
    );
    if (need !== '' && !holds)
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

  private need<K extends keyof IBridgeHandlers>(name: K): NonNullable<IBridgeHandlers[K]> {
    const h = this.opts.handlers[name];
    if (!h) throw new BridgeError('host_unavailable', 'the host does not do this here');
    return h as NonNullable<IBridgeHandlers[K]>;
  }

  private async run(method: TBridgeMethod, p: Record<string, unknown>): Promise<unknown> {
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
      case 'ui/initialize':
        this.handshaken = true;
        return {
          protocolVersion: PROTOCOL_VERSION,
          hostInfo: { name: 'harnessly', version: this.opts.hostVersion },
          hostCapabilities: { serverTools: {}, message: {}, openLinks: {} },
          hostContext: hostContextOf(this.context),
        };
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
      case 'harnessly/ui.toast':
        this.need('toast')(str('text', 500));
        return {};
      case 'harnessly/slot.done':
        this.need('slotDone')();
        return {};
      case 'harnessly/ui.confirm':
        return { ok: await this.need('confirm')(str('text', 500)) };
      default: {
        // harnessly/sinopia.<method>: the bridge has checked the right; the params are the runtime's to read.
        const name = method.slice('harnessly/sinopia.'.length);
        if (!isSinopiaMethod(name)) throw new BridgeError('invalid_params', `no method ${method}`);
        return { result: (await this.need('sinopia')(name, p)) ?? null };
      }
    }
  }
}
