// What the page asks the runtime for the apps' views (cmd/runtime/routes_apps.go, routes_apps_bridge.go): the installed
// apps with their views, and the two calls of a view that reach the machine, storage and tools.
import { useEffect, useState, useSyncExternalStore } from 'react';
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
  readSignIn,
  type TFilesOp,
  type TGitOp,
  type TRunOp,
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
  /** Slot views: the name and glyph the host draws, the order among the app's own, and the kinds of object an action is for. */
  label?: string;
  icon?: string;
  order?: number;
  objects?: string[];
}

export interface IBranding {
  name: string;
  accent?: string;
  supportUrl?: string;
  tagline?: string;
}

/** A chat.commands entry of an app, as the «/» palette lists it. */
export interface IAppCommand {
  /** The command without its slash. */
  name: string;
  /** What the agent is sent: /<plugin>:<name>. */
  slash: string;
  description: string;
  /** `prompt`: sent to the agent. (`view`, opening an app view in the pane, is not in the manifest yet.) */
  kind: 'prompt';
}

/** A chat.buttons entry: `command` (one of chat.commands, with its slash) or `view` (a view with the pane target). */
export interface IAppChatButton {
  id: string;
  title: string;
  icon?: string;
  command?: string;
  view?: string;
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
  /** The app's chat.commands, for the «/» palette of a chat it is on in. */
  commands?: IAppCommand[];
  /** Lines an empty chat it is on in offers under the field (chat.starters). */
  starters?: { text: string }[];
  /** Buttons at the field of a chat it is on in (chat.buttons): a command of the app or a view in the pane. */
  buttons?: IAppChatButton[];
  /** Addresses it shows a card for under a message (chat.links): https://<host>/<path>, `*.` and a trailing `*`. */
  links?: { match: string; card: string }[];
  /** What «@» in the field finds in the app (chat.mentions): its tool answers what is typed. */
  mentions?: { title: string; tool: string };
  /** On in every new Claude chat of the machine (`activation.on: ["always"]`, an app the person installed). */
  always?: boolean;
  /** What the app puts into the document editor; absent for an app that puts nothing. */
  sinopia?: ISinopia;
  dev?: boolean;
  /** The app comes with the organization's environment: the person cannot remove it. */
  env?: boolean;
  /** The environment's item is pinned: the app cannot be turned off. */
  locked?: boolean;
  /** The person turned the environment's app off for themselves. */
  muted?: boolean;
  /** The app has settings or secrets: «О приложении» draws their form. */
  setup?: boolean;
  /** The required secrets without a value: the app is «ждёт секрет». */
  waiting?: string[];
  /** Ready, or why not: waiting for a secret, a connection or another harnessly, a copy that fails, turned off. */
  state?: TAppState;
  reason?: TAppReason;
  /** The secrets, connections or version range the reason is about, or what failed. */
  detail?: string;
  /** The branding slot: the name, accent and support address the app brings. */
  branding?: IBranding;
  /** Everything the app reaches, from the most dangerous, in words; absent from an older machine. */
  access?: IAccess[];
  /** The install waits for the person to agree to all of `access`. */
  consent?: boolean;
  /** The chat whose agent installed what waits for consent: its card shows there. */
  askedIn?: string;
  /** A newer version that reaches more waits for the person's word; its `access` is only what it adds. */
  pending?: { version: string; access: IAccess[] };
  /** The person turned the app off with its switch. */
  off?: boolean;
  /** Where it was installed from: a folder, an address, a catalog. */
  source?: string;
}

/** The classes of risk, from the most dangerous (internal/apps surface.go). */
export const ACCESS_CLASS = {
  critical: 'critical',
  high: 'high',
  medium: 'medium',
  low: 'low',
} as const;
export type TAccessClass = (typeof ACCESS_CLASS)[keyof typeof ACCESS_CLASS];

/** A line in both languages of the page. */
export interface IText {
  ru: string;
  en: string;
}

/** One thing an app reaches (internal/apps access.go): a right, a secret, a host, a server, a part of a plugin. */
export interface IAccess {
  key: string;
  /** How the manifest writes it; shown small, on hover. */
  code: string;
  class: TAccessClass;
  label: IText;
  /** What a high or critical one can do to the person. */
  threat?: IText;
}

/** One line of the app's journal (internal/apps journal.go): what it did of a high or critical class, never a value. */
export interface IJournalEntry {
  at: number;
  what: string;
  class: TAccessClass;
  detail?: string;
  chat?: string;
  ok: boolean;
}

/** «Установить» / «Обновить» on the install screen, for the version it showed. */
/** «Установить» on the install screen: access are the keys the screen showed, and only those are granted. */
export const consentApp = (
  m: MachineId,
  id: string,
  version: string,
  access: readonly string[],
): Promise<unknown> => runtimeJson(m, platformPath('/apps/consent'), postJson({ id, version, access }));

/** «Отмена»: a waiting update is dropped (the running version stays); a first install that waited is removed. */
export const declineApp = (m: MachineId, id: string): Promise<{ removed: boolean }> =>
  runtimeJson<{ removed: boolean }>(m, platformPath('/apps/decline'), postJson({ id }));

/** One line a view reported: its console, an exception, a failed bridge call. */
export interface IViewLogLine {
  at: number;
  level: 'error' | 'warn' | 'bridge';
  text: string;
}

/** Hands what a view reported to the machine, where `harnessly app logs` and the runtime's log read it. */
export const reportViewLog = (
  m: MachineId,
  id: string,
  view: string,
  lines: readonly IViewLogLine[],
): Promise<unknown> => runtimeJson(m, platformPath('/apps/view-log'), postJson({ id, view, lines }));

/** The switch of an app: off, it starts nowhere and its views' calls are refused at once. */
export const switchApp = (m: MachineId, id: string, on: boolean): Promise<unknown> =>
  runtimeJson(m, platformPath('/apps/switch'), postJson({ id, on }));

export const loadAppJournal = async (m: MachineId, id: string): Promise<IJournalEntry[]> => {
  const r = await runtimeJson<{ journal: IJournalEntry[] }>(
    m,
    platformPath(`/apps/journal?id=${encodeURIComponent(id)}`),
  );
  return r.journal;
};

export const APP_STATE = {
  ready: 'ready',
  waiting: 'waiting',
  failed: 'failed',
  disabled: 'disabled',
} as const;
export type TAppState = (typeof APP_STATE)[keyof typeof APP_STATE];
export const APP_REASON = {
  off: 'off',
  secret: 'secret',
  connection: 'connection',
  host: 'host',
  package: 'package',
  safe: 'safe',
  consent: 'consent',
} as const;
export type TAppReason = (typeof APP_REASON)[keyof typeof APP_REASON];

export interface IInstalledApps {
  apps: IInstalledApp[];
  /** The loopback port of the view server, which a preview slot shows. */
  viewPort: number;
  /** The agents of chats with apps run inside the operating system's wall around the apps (docs/apps.md); false on a
   * machine that cannot make it, where only the guard hook keeps them out. */
  agentWalled: boolean;
  /** Whether the machine runs with UI slots on (HARNESSLY_UI_SLOTS=on); without it the list holds no slot view. */
  slots?: boolean;
  /** Safe mode: new chats start with only the apps the organization pinned. */
  safeMode?: boolean;
}

/** Turns safe mode on or off (POST /apps/safe-mode); chats already running keep their apps. */
export const setSafeMode = (m: MachineId, on: boolean): Promise<unknown> =>
  runtimeJson(m, platformPath('/apps/safe-mode'), postJson({ on }));

/**
 * «Войти» of an app whose server signs in by OAuth (auth: oauth, error.signIn {app, server}): the connection the sign-in
 * goes through (POST /apps/signin). The page then runs the connections' own sign-in for that id in a top-level popup.
 */
export const appSignInConnection = (
  m: MachineId,
  app: string,
  server: string,
): Promise<{ connection: string }> =>
  runtimeJson<{ connection: string }>(m, platformPath('/apps/signin'), postJson({ app, server }));

export const loadInstalledApps = (m: MachineId): Promise<IInstalledApps> =>
  runtimeJson<IInstalledApps>(m, platformPath('/installed-apps'));

/** The runtime's refusal says its code in `error`; a view gets it as the code of its HarnesslyError. */
const asBridgeError = (e: unknown): BridgeError => {
  if (e instanceof RuntimeError) {
    // A conflict says the version the key has now, which the view needs to read it again and decide.
    const body = typeof e.body === 'object' && e.body !== null ? (e.body as Record<string, unknown>) : {};
    const text = typeof body.message === 'string' ? body.message : e.message;
    const signIn = readSignIn(body.signIn);
    const extra = {
      ...(typeof body.version === 'number' ? { version: body.version } : {}),
      ...(signIn ? { signIn } : {}),
    };
    return new BridgeError(
      isBridgeCode(e.message) ? e.message : 'host_unavailable',
      text,
      Object.keys(extra).length > 0 ? extra : undefined,
    );
  }
  return new BridgeError('host_unavailable', e instanceof Error ? e.message : undefined);
};

export interface IViewSession {
  /** The token the runtime knows the view by: its app, target and chat are kept there, not sent with each call. */
  session: string;
  /** What the view's own pages carry (a meta tag) and the bridge asks for in ui/initialize. */
  nonce: string;
  /** Where the view's entry is on the view server, with the session in it; absent for a remote view. */
  path?: string;
  /** The address of a view the app's own server hosts (delivery mode remote). */
  url?: string;
}

/** Starts the session of a view the page mounts; the runtime refuses a view that is not the app's for the target. */
interface IViewAsk {
  app: string;
  view: string;
  target: TTarget;
  conversation: string | null;
  /** The Sinopia document a view of target `sinopia.<kind>` is shown in. */
  document?: string;
}

const askSession = async (m: MachineId, view: IViewAsk): Promise<IViewSession> => {
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

// A session asked for before the click (the pointer is on the app in the sidebar): the window that mounts next takes
// it instead of asking again, and saves a round trip to the machine. One mount takes it once; an old one is dropped.
const READY_MS = 20_000;
const ready = new Map<string, { at: number; made: Promise<IViewSession> }>();
const readyKey = (m: MachineId, v: IViewAsk) =>
  [m, v.app, v.view, v.target, v.conversation ?? '', v.document ?? ''].join('|');

export const prepareViewSession = (m: MachineId, view: IViewAsk): void => {
  const key = readyKey(m, view);
  const held = ready.get(key);
  if (held && Date.now() - held.at < READY_MS) return;
  const made = askSession(m, view);
  ready.set(key, { at: Date.now(), made });
  made.catch(() => {
    if (ready.get(key)?.made === made) ready.delete(key);
  });
};

export const createViewSession = (m: MachineId, view: IViewAsk): Promise<IViewSession> => {
  const key = readyKey(m, view);
  const held = ready.get(key);
  ready.delete(key);
  if (held && Date.now() - held.at < READY_MS) return held.made.catch(() => askSession(m, view));
  return askSession(m, view);
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
  /** For a set: write only if the key is at this version. */
  ifVersion?: number;
}

/** A get answers `{value, version}`, a set `{version}`, keys the list, a delete nothing. */
export const callStorage = async (m: MachineId, call: IStorageCall): Promise<unknown> => {
  try {
    const r = await runtimeJson<{ value?: unknown; keys?: string[]; version?: number }>(
      m,
      platformPath('/apps/storage'),
      postJson(call),
    );
    if (call.op === 'keys') return r.keys ?? [];
    if (call.op === 'get') return { value: r.value ?? null, version: r.version ?? 0 };
    if (call.op === 'set') return { version: r.version ?? 0 };
    return undefined;
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

/** The workspace beyond one file: the runtime judges each against the app's masks. */
export const callFilesMore = async (
  m: MachineId,
  session: string,
  op: TFilesOp,
  params: Record<string, unknown>,
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(m, platformPath('/apps/files'), postJson({ ...params, session, op }));
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** The repository of the chat's folder, read only (git.read). */
export const callGit = async (
  m: MachineId,
  session: string,
  op: TGitOp,
  params: Record<string, unknown>,
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(m, platformPath('/apps/git'), postJson({ ...params, session, op }));
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** A command or a process of the app in the chat's folder; the runtime runs it inside the agent's wall. */
/** The person turns an app's hook off (or on) on the machine: from the feed's line of its stop. */
export const setHookOff = (m: MachineId, id: string, off: boolean): Promise<unknown> =>
  runtimeJson(m, platformPath('/apps/hooks-off'), postJson({ id, off }));

/** The agent's browser of the chat through an app (browser.read, browser.drive). */
export const callBrowser = async (
  m: MachineId,
  session: string,
  params: { right: string; tool: string; args: Record<string, unknown>; confirmed?: boolean },
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(m, platformPath('/apps/browser'), postJson({ ...params, session }));
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** One answer of a model within the app's budget of the day (ai). */
export const callAI = async (
  m: MachineId,
  session: string,
  params: { prompt: string; system: string; maxTokens: number; model: string; confirmed?: boolean },
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(m, platformPath('/apps/ai'), postJson({ ...params, session }));
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** A value into the safe, never read back (secrets.write): {created}. */
export const callSecretsPut = async (
  m: MachineId,
  session: string,
  params: { name: string; value: string; shared: boolean; confirmed?: boolean },
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(m, platformPath('/apps/secrets'), postJson({ ...params, session }));
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** A task in a new chat under the app's chat, within the app's budget of the day (agents.run). */
export const callAgentsRun = async (
  m: MachineId,
  session: string,
  params: { prompt: string; title: string; confirmed?: boolean },
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(m, platformPath('/apps/agents/run'), postJson({ ...params, session }));
  } catch (e) {
    throw asBridgeError(e);
  }
};

/** What the chat's terminals show (terminal.read). */
export const callTerminalRead = async (
  m: MachineId,
  session: string,
  params: { terminal: string; lines: number },
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(
      m,
      platformPath('/apps/terminal/read'),
      postJson({ ...params, session }),
    );
  } catch (e) {
    throw asBridgeError(e);
  }
};

export const callRun = async (
  m: MachineId,
  session: string,
  op: TRunOp,
  params: Record<string, unknown>,
): Promise<unknown> => {
  try {
    return await runtimeJson<unknown>(m, platformPath('/apps/run'), postJson({ ...params, session, op }));
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

/** One decision of the guard of a chat's apps: a call it refused (`deny`) or put to the person (`ask`). */
export interface IAppDecision {
  /** Milliseconds. */
  at: number;
  app: string;
  tool: string;
  rule: string;
  decision: 'deny' | 'ask';
  reason?: string;
}

/** The last decisions of the guard for an app in a chat, newest first (the runtime keeps fifty). */
export const loadAppDecisions = async (
  m: MachineId,
  app: string,
  conversation: string,
): Promise<IAppDecision[]> => {
  const r = await runtimeJson<{ decisions: IAppDecision[] }>(
    m,
    platformPath(
      `/apps/decisions?app=${encodeURIComponent(app)}&conversation=${encodeURIComponent(conversation)}`,
    ),
  );
  return r.decisions;
};

/** The pick, or null to cancel; the runtime refuses an ask that is gone or an option it did not offer. */
export const answerAppAsk = async (m: MachineId, id: string, answer: string | null): Promise<void> => {
  await runtimeJson<object>(m, platformPath('/apps/asks/answer'), postJson({ id, answer }));
};

const lists = new Map<MachineId, Promise<IInstalledApps>>();

/** How many times each machine's list was read again: a consent given in one place shows in every other. */
const rereads = new Map<MachineId, number>();
const rereadListeners = new Set<() => void>();
const onReread = (f: () => void) => {
  rereadListeners.add(f);
  return () => {
    rereadListeners.delete(f);
  };
};

/** Read the machine's installed apps again, for every screen that shows them (after a consent, by a watch). */
export const refreshInstalledApps = (m: MachineId): void => {
  lists.delete(m);
  rereads.set(m, (rereads.get(m) ?? 0) + 1);
  for (const f of rereadListeners) f();
};

/** The apps that wait for the person's word, light: no view server is started for it. */
export const loadConsentApps = async (m: MachineId): Promise<IInstalledApp[]> =>
  (await runtimeJson<{ apps: IInstalledApp[] }>(m, platformPath('/apps/consent/asks'))).apps;

/** How often a screen that shows consent asks reads them again, while the page is seen. */
const CONSENT_WATCH_MS = 15_000;

/**
 * The apps of a machine that wait for consent: null until the first answer (or on a machine without the route). Read
 * again with every refreshInstalledApps and, with `watch`, every few seconds while the page is seen, so an agent's
 * install shows without a reload.
 */
export const useConsentApps = (m: MachineId, enabled = true, watch = false): IInstalledApp[] | null => {
  const reread = useSyncExternalStore(onReread, () => rereads.get(m) ?? 0);
  const [state, setState] = useState<{ m: MachineId; apps: IInstalledApp[] } | null>(null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    // A machine without the route, or one that does not answer now, keeps what was shown: the next read tries again.
    loadConsentApps(m).then(
      (apps) => live && setState({ m, apps }),
      () => undefined,
    );
    return () => {
      live = false;
    };
  }, [m, enabled, reread, tick]);
  useEffect(() => {
    if (!enabled || !watch) return;
    const timer = setInterval(() => {
      if (!document.hidden) setTick((n) => n + 1);
    }, CONSENT_WATCH_MS);
    return () => clearInterval(timer);
  }, [m, enabled, watch]);
  return enabled && state?.m === m ? state.apps : null;
};

/**
 * The installed apps of a machine: null while they load. One read per machine until `reload` or a
 * refreshInstalledApps; a read again keeps the list it had on screen until the new one comes. `enabled: false` asks
 * nothing (a read starts the view server on the machine, so a screen that may have no use for the list waits for one).
 */
export const useInstalledApps = (
  m: MachineId,
  reload = 0,
  enabled = true,
): { list: IInstalledApps | null; error: string | null } => {
  const [state, setState] = useState<{
    m: MachineId;
    key: string;
    list: IInstalledApps | null;
    error: string | null;
  } | null>(null);
  const reread = useSyncExternalStore(onReread, () => rereads.get(m) ?? 0);
  const key = `${m}|${reload}|${reread}`;
  useEffect(() => {
    if (!enabled) return;
    let live = true;
    if (reload > 0) lists.delete(m);
    const asked = lists.get(m) ?? loadInstalledApps(m);
    lists.set(m, asked);
    asked.then(
      (list) => live && setState({ m, key, list, error: null }),
      (e: Error) => {
        lists.delete(m);
        if (live) setState({ m, key, list: null, error: e.message });
      },
    );
    return () => {
      live = false;
    };
  }, [m, reload, key, enabled]);
  if (!enabled || state === null) return { list: null, error: null };
  if (state.key === key) return state;
  // Read again for the same machine and `reload`: the old list stays until the new one comes, so nothing flickers.
  return state.m === m && state.key.startsWith(`${m}|${reload}|`) && state.list !== null
    ? { list: state.list, error: null }
    : { list: null, error: null };
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

/** A connection of the person's (Настройки → Подключения) a server of the app goes through. */
export interface IAppConnection {
  id: string;
  name: string;
  /** The person allowed it to this app. */
  granted: boolean;
  /** The person is signed in to it. */
  connected: boolean;
}

export interface IAppSetup {
  fields: ISettingField[];
  values: Record<string, unknown>;
  secrets: ISecretState[];
  waiting: string[];
  connections?: IAppConnection[];
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

/** Allows the app the person's connection (on) or takes it back. */
export const setAppConnection = (m: MachineId, id: string, name: string, on: boolean): Promise<IAppSetup> =>
  runtimeJson<IAppSetup>(m, platformPath('/apps/setup/connection'), postJson({ id, name, remove: !on }));

export const removeAppSecret = (m: MachineId, id: string, name: string): Promise<IAppSetup> =>
  runtimeJson<IAppSetup>(m, platformPath('/apps/setup/secret'), postJson({ id, name, remove: true }));

/** One find of an app for «@»: what the palette shows and what goes into the field. */
export interface IAppMention {
  label: string;
  detail?: string;
  insert: string;
}

export interface IAppMentionGroup {
  app: string;
  title: string;
  items: IAppMention[];
}

/** What the apps on in the chat find for the words after «@»; an app that is slow or fails is simply not there. */
export const findAppMentions = async (
  m: MachineId,
  conversation: string,
  query: string,
  signal: AbortSignal,
): Promise<IAppMentionGroup[]> =>
  (
    await runtimeJson<{ groups: IAppMentionGroup[] }>(m, platformPath('/apps/mentions'), {
      ...postJson({ conversation, query }),
      signal,
    })
  ).groups;
