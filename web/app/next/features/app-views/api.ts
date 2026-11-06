// What the page asks the runtime for the apps' views (cmd/runtime/routes_apps.go, routes_apps_bridge.go): the installed
// apps with their views, and the two calls of a view that reach the machine, storage and tools.
import { useEffect, useState } from 'react';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { postJson, runtimeJson, RuntimeError } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';
import type {
  ISelectedNode,
  ISinopia,
  ISinopiaCheckResult,
  ISinopiaMark,
  ISinopiaState,
} from '@/next/features/app-views/sinopia/types';
import {
  BridgeError,
  isBridgeCode,
  type TStorageScope,
  type TTarget,
} from '@/next/features/app-views/bridge';

export interface IAppView {
  id: string;
  title?: string;
  targets: string[];
  height?: string;
  /** The view's entry on the runtime's view server, for a view the package carries. */
  path?: string;
  /** The address of a view the app's own server hosts. */
  url?: string;
}

export interface IInstalledApp {
  id: string;
  name: string;
  description: string;
  icon?: string;
  version: string;
  trust: string;
  targets: string[];
  permissions: string[];
  views: IAppView[];
  /** The servers of the package's .mcp.json. */
  mcpServers: string[];
  /** What the app's instructions weigh in the prompt of every turn; the page shows tokens (bytes / 4). */
  instructionsBytes?: number;
  /** The slash command that turns the app on in a chat (`activation.on: ["command:/x"]`); absent when none. */
  command?: string;
  /** On in every new Claude chat of the machine (`activation.on: ["always"]`, an app the person installed). */
  always?: boolean;
  /** What the app puts into the document editor; absent for an app that puts nothing. */
  sinopia?: ISinopia;
  dev?: boolean;
  /** The app has settings or secrets: «О приложении» draws their form. */
  setup?: boolean;
  /** The required secrets without a value: the app is «ждёт секрет». */
  waiting?: string[];
}

export interface IInstalledApps {
  apps: IInstalledApp[];
  /** The loopback port of the view server, which a preview slot shows. */
  viewPort: number;
  /** The agents of chats with apps run inside the operating system's wall around the apps (docs/apps.md); false on a
   * machine that cannot make it, where only the guard hook keeps them out. */
  agentWalled: boolean;
}

export const loadInstalledApps = (m: MachineId): Promise<IInstalledApps> =>
  runtimeJson<IInstalledApps>(m, platformPath('/installed-apps'));

/** The runtime's refusal says its code in `error`; a view gets it as the code of its HarnesslyError. */
const asBridgeError = (e: unknown): BridgeError => {
  if (e instanceof RuntimeError)
    return new BridgeError(isBridgeCode(e.message) ? e.message : 'host_unavailable', e.message);
  return new BridgeError('host_unavailable', e instanceof Error ? e.message : undefined);
};

export interface IViewSession {
  /** The token the runtime knows the view by: its app, target and chat are kept there, not sent with each call. */
  session: string;
  /** What the view's own pages carry (a meta tag) and the bridge asks for in ui/initialize. */
  nonce: string;
  /** Where the view's entry is on the view server, with the session in it. */
  path: string;
}

/** Starts the session of a view the page mounts; the runtime refuses a view that is not the app's for the target. */
export const createViewSession = async (
  m: MachineId,
  view: {
    app: string;
    view: string;
    target: TTarget;
    conversation: string | null;
    /** The Sinopia document a view of target `sinopia.<kind>` is shown in. */
    document?: string;
  },
): Promise<IViewSession> => {
  try {
    return await runtimeJson<IViewSession>(
      m,
      platformPath('/apps/session'),
      postJson({ ...view, conversation: view.conversation ?? '' }),
    );
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** Keeps a mounted view's session alive while nobody uses it; the runtime refuses a session that is gone. */
export const renewViewSession = async (m: MachineId, session: string): Promise<void> => {
  try {
    await runtimeJson<{ expiresIn: number }>(m, platformPath('/apps/session/renew'), postJson({ session }));
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** Posts to a route of the Sinopia API; a refusal keeps the runtime's code and words, as a view reads them. */
const postSinopia = async <T>(m: MachineId, path: `/${string}`, body: unknown): Promise<T> => {
  let res: Response;
  try {
    res = await fetch(runtimePath(platformPath(path), m), { credentials: 'same-origin', ...postJson(body) });
  } catch {
    throw new BridgeError('host_unavailable', 'the machine did not answer');
  }
  const answer: unknown = await res.json().catch(() => null);
  const record = typeof answer === 'object' && answer !== null ? (answer as Record<string, unknown>) : {};
  if (!res.ok) {
    const code =
      typeof record.error === 'string' && isBridgeCode(record.error) ? record.error : 'host_unavailable';
    throw new BridgeError(code, typeof record.message === 'string' ? record.message : undefined);
  }
  return answer as T;
};

/** A Document API call of a view (`hl.sinopia.<method>`); the runtime knows the document from the view's session. */
export const callSinopia = async (
  m: MachineId,
  session: string,
  method: string,
  params: Record<string, unknown>,
): Promise<unknown> =>
  (await postSinopia<{ result: unknown }>(m, '/apps/sinopia', { session, method, params })).result;

/**
 * Tells the runtime which document is open beside which chat and what is picked (when it is given), and gets what it
 * keeps of the document: the apps on in it, the marks and the results of the checks. The save checks run on the
 * runtime when the boards changed since the last time.
 */
export const syncSinopia = (
  m: MachineId,
  document: string,
  conversation: string | null,
  selection?: ISelectedNode[],
): Promise<ISinopiaState> =>
  postSinopia<ISinopiaState>(m, '/apps/sinopia/state', {
    document,
    conversation: conversation ?? '',
    ...(selection === undefined ? {} : { selection }),
  });

export interface ISinopiaChecked {
  results: ISinopiaCheckResult[];
  /** A check keeps the document from being shared as it is. */
  blocked: boolean;
  marks: ISinopiaMark[];
}

/** Runs the checks that run on `save` or `share` and answers what they found. */
export const runSinopiaChecks = (
  m: MachineId,
  document: string,
  on: 'save' | 'share',
): Promise<ISinopiaChecked> => postSinopia<ISinopiaChecked>(m, '/apps/sinopia/checks', { document, on });

export interface IStorageCall {
  session: string;
  op: 'get' | 'set' | 'delete' | 'keys';
  scope: TStorageScope;
  key: string;
  value?: unknown;
}

export const callStorage = async (m: MachineId, call: IStorageCall): Promise<unknown> => {
  try {
    const r = await runtimeJson<{ value?: unknown; keys?: string[] }>(
      m,
      platformPath('/apps/storage'),
      postJson(call),
    );
    return call.op === 'keys' ? (r.keys ?? []) : (r.value ?? null);
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** hl.settings.get(): what the person saved in the app's form. The runtime reads the app from the session. */
export const callSettings = async (m: MachineId, session: string): Promise<unknown> => {
  try {
    const r = await runtimeJson<{ value?: unknown }>(
      m,
      platformPath('/apps/settings'),
      postJson({ session }),
    );
    return r.value ?? {};
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** hl.files: a file of the chat's workspace. The runtime holds the app's masks and says no to anything outside them. */
export const callFiles = async (
  m: MachineId,
  session: string,
  op: 'read' | 'write',
  path: string,
  text?: string,
): Promise<string | undefined> => {
  try {
    const r = await runtimeJson<{ text?: string }>(
      m,
      platformPath('/apps/files'),
      postJson({ session, op, path, text }),
    );
    return r.text;
  } catch (e) {
    throw asBridgeError(e);
  }
};

export const callTool = async (
  m: MachineId,
  session: string,
  name: string,
  args: Record<string, unknown>,
): Promise<unknown> => {
  try {
    const r = await runtimeJson<{ result: unknown }>(
      m,
      platformPath('/apps/tool'),
      postJson({ session, name, args }),
    );
    return r.result;
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** A question the agent put to the person inside an app (app.ask), waiting for a choice. */
export interface IAppAsk {
  id: string;
  app: string;
  appName: string;
  question: string;
  options: string[];
  createdAt: number;
}

export const loadAppAsks = async (m: MachineId, conversation: string): Promise<IAppAsk[]> => {
  const r = await runtimeJson<{ asks: IAppAsk[] }>(
    m,
    platformPath(`/apps/asks?conversation=${encodeURIComponent(conversation)}`),
  );
  return r.asks;
};

/** The pick, or null to cancel; the runtime refuses an ask that is gone or an option it did not offer. */
export const answerAppAsk = async (m: MachineId, id: string, answer: string | null): Promise<void> => {
  await runtimeJson<object>(m, platformPath('/apps/asks/answer'), postJson({ id, answer }));
};

const lists = new Map<MachineId, Promise<IInstalledApps>>();

/**
 * The installed apps of a machine: null while they load. One read per machine until `reload`. `enabled: false` asks
 * nothing (a read starts the view server on the machine, so a screen that may have no use for the list waits for one).
 */
export const useInstalledApps = (
  m: MachineId,
  reload = 0,
  enabled = true,
): { list: IInstalledApps | null; error: string | null } => {
  const [state, setState] = useState<{
    key: string;
    list: IInstalledApps | null;
    error: string | null;
  } | null>(null);
  const key = `${m}|${reload}`;
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    if (reload > 0) lists.delete(m);
    const asked = lists.get(m) ?? loadInstalledApps(m);
    lists.set(m, asked);
    asked.then(
      (list) => live && setState({ key, list, error: null }),
      (e: Error) => {
        lists.delete(m);
        if (live) setState({ key, list: null, error: e.message });
      },
    );
    return () => {
      live = false;
    };
  }, [m, reload, key, enabled]);
  return enabled && state?.key === key ? state : { list: null, error: null };
};

/** One setting of an app, as its form draws it (internal/apps/setup.go). */
export interface ISettingField {
  name: string;
  type: 'string' | 'number' | 'integer' | 'boolean';
  title: string;
  description?: string;
  enum?: (string | number | boolean)[];
  default?: unknown;
  required?: boolean;
  minimum?: number;
  maximum?: number;
}

/** A secret the app asks for: whether it has a value, never the value. */
export interface ISecretState {
  name: string;
  description: string;
  optional: boolean;
  set: boolean;
}

export interface IAppSetup {
  fields: ISettingField[];
  values: Record<string, unknown>;
  secrets: ISecretState[];
  waiting: string[];
  /** After a save the runtime did not accept: the reason by setting. Nothing was saved. */
  problems?: Record<string, string>;
}

export const loadAppSetup = (m: MachineId, id: string): Promise<IAppSetup> =>
  runtimeJson<IAppSetup>(m, platformPath('/apps/setup'), postJson({ id }));

export const saveAppSettings = (
  m: MachineId,
  id: string,
  values: Record<string, unknown>,
): Promise<IAppSetup> =>
  runtimeJson<IAppSetup>(m, platformPath('/apps/setup/settings'), postJson({ id, values }));

/** Puts a secret's value into the machine's safe; the value goes in and never comes back. */
export const putAppSecret = (m: MachineId, id: string, name: string, value: string): Promise<IAppSetup> =>
  runtimeJson<IAppSetup>(m, platformPath('/apps/setup/secret'), postJson({ id, name, value }));

export const removeAppSecret = (m: MachineId, id: string, name: string): Promise<IAppSetup> =>
  runtimeJson<IAppSetup>(m, platformPath('/apps/setup/secret'), postJson({ id, name, remove: true }));
