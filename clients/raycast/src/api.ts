export type Machine = { id: string; name: string; online: boolean; status: string; org: string };

export type Attention = "" | "question" | "permission" | "plan" | "unread";

export type Chat = {
  id: string;
  machineId: string;
  machineName: string;
  online: boolean;
  title: string;
  project: string;
  projectId: string | null;
  cwd: string;
  provider: string;
  model: string;
  state: string;
  attention: Attention;
  unread: boolean;
  pinned: boolean;
  recap: string | null;
  snippet: string | null;
  lastTurnStatus: string | null;
  queue: number;
  background: number;
  from?: string;
  delegated: boolean;
  createdAt: number;
  lastPromptAt: number;
  activityAt: number;
  url: string;
};

export type Turn = {
  id: string;
  status: string;
  startedAt: number;
  endedAt: number | null;
  toolCount: number;
  prompt: string;
  answer: string;
};

export type Delivery = "delivered" | "queued" | "failed";
export type ChatFilter = "all" | "attention" | "working";

export type ClientUser = { id: string; email: string; name: string };
export type MeResponse = { user: ClientUser; hub: { url: string }; scopes: string[]; machines: Machine[] };
export type ChatsResponse = { chats: Chat[]; counts: { all: number; attention: number; working: number } };
export type ChatResponse = { chat: Chat; turns: Turn[]; complete: boolean };
export type SendResponse = {
  delivery: Delivery;
  chatId: string;
  machineId: string;
  clientId: string;
  url: string;
  error?: string;
  queueId?: string;
};
export type CreateChatRequest = {
  machineId: string;
  projectId?: string;
  cwd?: string;
  provider?: string;
  model?: string;
  text: string;
  worktree?: boolean;
  clientId?: string;
};
export type CreateChatResponse = {
  delivery: Delivery;
  machineId: string;
  clientId: string;
  id?: string;
  url?: string;
  error?: string;
  queueId?: string;
};
export type Project = {
  machineId: string;
  machineName: string;
  online: boolean;
  projectId: string;
  cwd: string;
  name: string;
  providers: string[];
  lastUsedAt: number;
  chats: number;
};
export type ProjectsResponse = { projects: Project[] };
export type AgentModel = { id: string; label: string; hint: string };
export type MachineAgent = { provider: string; status: string; models: AgentModel[] };
export type AgentsResponse = { machineId: string; agents: MachineAgent[] };

export type DeviceStart = {
  deviceCode: string;
  userCode: string;
  verifyUrl: string;
  interval: number;
  expiresIn: number;
};
export type DeviceToken = {
  token: string;
  tokenId: string;
  scopes: string[];
  expiresAt: string;
  user: ClientUser;
};

/** An error answer of the hub: HTTP status plus the `error` code of the JSON body. */
export class ApiError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message?: string) {
    super(message || code || `HTTP ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }

  /** The token no longer works: the person has to connect again. */
  get tokenInvalid(): boolean {
    return this.status === 401;
  }

  get scopeMissing(): boolean {
    return this.status === 403;
  }
}

export function normalizeUrl(raw: string): string {
  let u = raw.trim();
  if (!/^https?:\/\//i.test(u)) u = `https://${u}`;
  return u.replace(/\/+$/, "");
}

/** A readable one-line message for a failed call. */
export function describeError(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.tokenInvalid) return "Token is no longer valid, reconnect the hub";
    if (e.scopeMissing) return "The token is not allowed to do this (missing scope or the API is off)";
    if (e.code === "machine_offline") return "The machine is offline";
    if (e.status === 429) return "Too many requests, try again in a minute";
    return e.code ? `${e.code} (HTTP ${e.status})` : `HTTP ${e.status}`;
  }
  if (e instanceof Error) {
    if (e.name === "AbortError" || e.name === "TimeoutError") return "The hub did not answer in time";
    return `Cannot reach the hub: ${e.message}`;
  }
  return String(e);
}

async function call<T>(
  baseUrl: string,
  path: string,
  opts: { method?: string; token?: string; body?: unknown; allowEmpty?: boolean } = {},
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(`${normalizeUrl(baseUrl)}${path}`, {
    method: opts.method ?? "GET",
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    signal: AbortSignal.timeout(20_000),
  });
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  let json: unknown = undefined;
  try {
    json = text ? JSON.parse(text) : undefined;
  } catch {
    json = undefined;
  }
  if (!res.ok) {
    const code = json && typeof json === "object" && "error" in json ? String((json as { error: unknown }).error) : "";
    throw new ApiError(res.status, code);
  }
  return json as T;
}

// Device flow (no token).

export function deviceStart(baseUrl: string, client: string, device?: string): Promise<DeviceStart> {
  return call<DeviceStart>(baseUrl, "/api/auth/device/start", { method: "POST", body: { client, device } });
}

export function deviceToken(baseUrl: string, deviceCode: string): Promise<DeviceToken> {
  return call<DeviceToken>(baseUrl, "/api/auth/device/token", { method: "POST", body: { deviceCode } });
}

// Client API (Bearer token).

export type Conn = { url: string; token: string };

const v1 = "/api/client/v1";

export const getMe = (c: Conn) => call<MeResponse>(c.url, `${v1}/me`, { token: c.token });

export function listChats(c: Conn, filter: ChatFilter = "all", limit = 100): Promise<ChatsResponse> {
  return call<ChatsResponse>(c.url, `${v1}/chats?filter=${filter}&limit=${limit}`, { token: c.token });
}

export function getChat(c: Conn, id: string, limit = 5): Promise<ChatResponse> {
  return call<ChatResponse>(c.url, `${v1}/chats/${encodeURIComponent(id)}?limit=${limit}`, { token: c.token });
}

export function sendMessage(c: Conn, id: string, text: string, mode?: "queue" | "now"): Promise<SendResponse> {
  return call<SendResponse>(c.url, `${v1}/chats/${encodeURIComponent(id)}/messages`, {
    method: "POST",
    token: c.token,
    body: { text, mode, clientId: newClientId() },
  });
}

export function createChat(c: Conn, req: CreateChatRequest): Promise<CreateChatResponse> {
  return call<CreateChatResponse>(c.url, `${v1}/chats`, {
    method: "POST",
    token: c.token,
    body: { ...req, clientId: req.clientId ?? newClientId() },
  });
}

export function stopChat(c: Conn, id: string): Promise<void> {
  return call<void>(c.url, `${v1}/chats/${encodeURIComponent(id)}/stop`, { method: "POST", token: c.token });
}

export function markChatRead(c: Conn, id: string): Promise<void> {
  return call<void>(c.url, `${v1}/chats/${encodeURIComponent(id)}/read`, { method: "POST", token: c.token });
}

export function listProjects(c: Conn): Promise<ProjectsResponse> {
  return call<ProjectsResponse>(c.url, `${v1}/projects`, { token: c.token });
}

export function listAgents(c: Conn, machineId: string): Promise<AgentsResponse> {
  return call<AgentsResponse>(c.url, `${v1}/machines/${encodeURIComponent(machineId)}/agents`, { token: c.token });
}

export function newClientId(): string {
  return `rc-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}
