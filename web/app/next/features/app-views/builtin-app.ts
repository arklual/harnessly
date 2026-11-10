// The SDK of a builtin app (HRN-808): an app of the host's own (Sinopia) runs in the host's page, not in a frame, and
// reaches the host only through this — the bridge's own methods, rights and checks, called in the same process. Its
// manifest (internal/apps/builtin/<app>/harnessly/app.json, read here from surface.gen.ts) names its rights, and a
// method it has no right to is refused the same way a frame's is.
import type { ReactNode } from 'react';
import type { IModalProps } from '@/next/features/shell/modal';
import {
  AppBridge,
  BridgeError,
  type IBridgeHandlers,
  type IBridgeLogEntry,
  type IToastOptions,
  type IViewContext,
  type TAppKeyHandlers,
  type TAppKeyScope,
} from '@/next/features/app-views/bridge';
import { BUILTIN_APPS } from '@/next/features/app-views/surface.gen';

export type TBuiltinApp = (typeof BUILTIN_APPS)[number]['id'];

/** A tool of the page, in the form of WebMCP's registerTool: the agent of the chat calls it with page_call. */
export interface IBuiltinTool {
  name: string;
  description: string;
  inputSchema?: Record<string, unknown>;
  /** The person is asked before it runs (as WebMCP's destructiveHint). */
  confirm?: boolean;
  annotations?: { destructiveHint?: boolean; readOnlyHint?: boolean };
  execute: (args: Record<string, unknown>) => unknown;
}

/** What a builtin app gets: the SDK's calls it uses, by the SDK's names (hl.chat.send, hl.ui.toast…). */
export interface IBuiltinHl {
  readonly app: TBuiltinApp;
  chat: { send: (text: string) => Promise<void> };
  ui: {
    toast: (text: string, options?: IToastOptions) => Promise<void>;
    confirm: (text: string) => Promise<boolean>;
    /** A window of the host's own over the page, with the app's content: drawn in the app's tree (ui.modal). */
    modal: (props: IModalProps) => ReactNode;
  };
  /** The host's shortcut commands of a screen, on the person's keys (keys.on, keys.press, keys.hints). */
  keys: {
    /** Runs the screen's commands with these handlers (read on every press) until the returned function is called. */
    on: (scope: TAppKeyScope, handlers: () => TAppKeyHandlers) => () => void;
    press: (chord: string, typing?: boolean) => Promise<boolean>;
    hints: (commands: readonly string[]) => Promise<Record<string, string | null>>;
  };
  /** Tools of the page (tools.page): the whole list is told to the host on every change. */
  tools: { register: (tool: IBuiltinTool) => { unregister: () => void } };
  /** The same tools in WebMCP's own words (navigator.modelContext). */
  modelContext: {
    registerTool: (tool: IBuiltinTool) => { unregister: () => void };
    unregisterTool: (name: string) => void;
  };
  /** What the app shows, for the agent of the chat to read with page_tools (context.publish). */
  publish: (data: unknown) => Promise<void>;
  /** The app's page is gone: its tools, its keys and its context go with it. */
  dispose: () => void;
  /** The host tells the app something (a call of its page tool), as it tells a frame. */
  notifyView: (method: string, params: unknown) => void;
  notify: (text: string) => Promise<void>;
  /** Any method of the bridge by its wire name, for what the shorthands above do not cover. */
  call: (method: string, params?: Record<string, unknown>) => Promise<unknown>;
  /** The host's side changed (the chat the app is shown with, the theme). */
  setContext: (next: Partial<IViewContext>) => void;
}

export interface IBuiltinOptions {
  /** The work behind the methods: the host's own, as for a frame. */
  handlers: IBridgeHandlers;
  hostVersion?: string;
  context?: Partial<IViewContext>;
  onCall?: (entry: IBridgeLogEntry) => void;
  /** The app's page is gone (after the bridge stops): what the host keeps for it is let go. */
  onDispose?: () => void;
}

const MAX_RESULT_TEXT = 200_000;

/** What a tool's execute gave, as the agent reads it: JSON, within the limit. */
const resultOf = (value: unknown): unknown => {
  if (value === undefined) return null;
  const text = JSON.stringify(value);
  if (text !== undefined && text.length > MAX_RESULT_TEXT)
    throw new BridgeError('invalid_params', 'the tool answered more than the agent may read');
  return value;
};

/** Connects a builtin app to the host in the page's own process. */
export const connectBuiltin = (id: TBuiltinApp, opts: IBuiltinOptions): IBuiltinHl => {
  const manifest = BUILTIN_APPS.find((a) => a.id === id);
  if (manifest === undefined) throw new Error(`${id} is not an app of the host's own`);
  let context: IViewContext = {
    app: { id: manifest.id, version: manifest.version },
    target: 'app',
    view: 'main',
    theme: { mode: 'light', tokens: {} },
    locale: 'en',
    chat: null,
    user: null,
    ...opts.context,
  };
  const bridge = new AppBridge({
    frame: () => null,
    origin: 'builtin',
    app: {
      id: manifest.id,
      version: manifest.version,
      permissions: manifest.permissions,
      targets: manifest.targets,
    },
    context,
    handlers: opts.handlers,
    hostVersion: opts.hostVersion ?? '',
    onCall: opts.onCall,
    inProcess: true,
    onNotify: (method, params) => {
      if (method === 'harnessly/notifications/tool-call') void runTool(params);
    },
  });
  const call = (method: string, params: Record<string, unknown> = {}) => bridge.call(method, params);
  const tools = new Map<string, IBuiltinTool>();
  let announced: Promise<unknown> = Promise.resolve();
  const announce = () => {
    const list = [...tools.values()].map((t) => ({
      name: t.name,
      description: t.description,
      ...(t.inputSchema ? { inputSchema: t.inputSchema } : {}),
      ...(t.confirm === true || t.annotations?.destructiveHint === true ? { confirm: true } : {}),
    }));
    // In order: a later list never lands before an earlier one.
    announced = announced
      .then(() => call('harnessly/tools.register', { tools: list }))
      .catch(() => undefined);
  };
  const runTool = async (params: unknown) => {
    const p = (params ?? {}) as { id?: unknown; name?: unknown; arguments?: unknown };
    if (typeof p.id !== 'string' || typeof p.name !== 'string') return;
    const tool = tools.get(p.name);
    const args =
      p.arguments !== null && typeof p.arguments === 'object' ? (p.arguments as Record<string, unknown>) : {};
    let answer: { result: unknown } | { error: string };
    try {
      if (tool === undefined) throw new Error(`no tool ${p.name} on this page`);
      answer = { result: resultOf(await tool.execute(args)) };
    } catch (e) {
      answer = { error: e instanceof Error ? e.message : String(e) };
    }
    await call('harnessly/tools.result', { id: p.id, ...answer }).catch(() => undefined);
  };
  const register = (tool: IBuiltinTool) => {
    tools.set(tool.name, tool);
    announce();
    return {
      unregister: () => {
        if (tools.get(tool.name) !== tool) return;
        tools.delete(tool.name);
        announce();
      },
    };
  };
  return {
    app: manifest.id,
    chat: {
      send: async (text) => {
        await call('ui/message', { role: 'user', content: [{ type: 'text', text }] });
      },
    },
    ui: {
      toast: async (text, options = {}) => {
        await call('harnessly/ui.toast', { text, ...options });
      },
      confirm: async (text) => {
        const got = (await call('harnessly/ui.confirm', { text })) as { ok?: boolean } | undefined;
        return got?.ok === true;
      },
      modal: (props) => {
        const draw = opts.handlers.modal;
        if (draw === undefined)
          throw new BridgeError('host_unavailable', 'the host draws no windows for this app');
        return draw(props);
      },
    },
    notify: async (text) => {
      await call('harnessly/notify', { text });
    },
    keys: {
      on: (scope, handlers) => bridge.listenKeys(scope, handlers),
      press: async (chord, typing = false) => {
        const got = (await call('harnessly/keys.press', { chord, typing })) as { ran?: boolean } | undefined;
        return got?.ran === true;
      },
      hints: async (commands) => {
        const got = (await call('harnessly/keys.hints', { commands: [...commands] })) as
          { hints?: Record<string, string | null> } | undefined;
        return got?.hints ?? {};
      },
    },
    tools: { register },
    modelContext: {
      registerTool: register,
      unregisterTool: (name) => {
        if (tools.delete(name)) announce();
      },
    },
    publish: async (data) => {
      await call('harnessly/context.publish', { data });
    },
    dispose: () => {
      tools.clear();
      bridge.dispose();
      opts.onDispose?.();
    },
    notifyView: (method, params) => bridge.notify(method, params),
    call,
    setContext: (next) => {
      context = { ...context, ...next };
      bridge.setContext(context);
    },
  };
};
