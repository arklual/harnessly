import { platformApi } from './base';
import { request } from './http';
import type {
  Conversation,
  Layout,
  MachineLoad,
  MachineSnapshot,
  PanelType,
  Provider,
  Tab,
  Workspace,
  WorkspaceList,
} from './types';

const q = (v: string) => encodeURIComponent(v);

// --- session ------------------------------------------------------------------------------------------------------

export interface SetupState {
  needsSetup: boolean;
  requiresAuth: boolean;
  hostEnvLocked: boolean;
}

export const auth = {
  state: () => request<SetupState>('/api/auth/setup', { quietAuth: true }),
  setup: (password: string) =>
    request<{ ok: true }>('/api/auth/setup', {
      body: { authPassword: password, networkAccess: 'localhost' },
      quietAuth: true,
    }),
  login: (password: string) =>
    request<{ ok: true }>('/api/auth/login', { body: { password }, quietAuth: true }),
  logout: () => request<{ ok: true }>('/api/auth/logout', { method: 'POST', quietAuth: true }),
  /** Resolves when the page's session is accepted, rejects with 401 when it is not. */
  check: () => request<WorkspaceList>('/api/workspace', { quietAuth: true }),
};

export interface Bootstrap {
  owner: string;
  preflightOk: boolean;
  initialWorkspace: WorkspaceList;
}

export const bootstrap = () => request<Bootstrap>(`${platformApi()}/bootstrap`);

// --- workspaces ---------------------------------------------------------------------------------------------------

export const workspaces = {
  list: () => request<WorkspaceList>('/api/workspace'),
  create: (name: string, directory?: string) =>
    request<Workspace>('/api/workspace', { body: { name, ...(directory ? { directory } : {}) } }),
  rename: (id: string, name: string) =>
    request<Workspace>(`/api/workspace/${q(id)}`, { method: 'PATCH', body: { name } }),
  remove: (id: string) => request<null>(`/api/workspace/${q(id)}`, { method: 'DELETE' }),
  setActive: (id: string) =>
    request<{ ok: true }>('/api/workspace/active', { method: 'PATCH', body: { activeWorkspaceId: id } }),
  reorder: (ids: string[]) =>
    request<{ ok: true }>('/api/workspace/reorder', { method: 'PATCH', body: { workspaceIds: ids } }),
  validate: (directory: string) =>
    request<{ valid: boolean; suggestedName?: string; error?: string }>(
      `/api/workspace/validate?directory=${q(directory)}`,
    ),
};

// --- layout -------------------------------------------------------------------------------------------------------

const ws = (workspaceId: string) => `?workspace=${q(workspaceId)}`;

export const layout = {
  get: (w: string) => request<Layout>(`/api/layout${ws(w)}`),
  setActivePane: (w: string, paneId: string) =>
    request<Layout>(`/api/layout${ws(w)}`, { method: 'PATCH', body: { activePaneId: paneId } }),
  resize: (w: string, path: number[], ratio: number) =>
    request<Layout>(`/api/layout${ws(w)}`, { method: 'PATCH', body: { ratioUpdate: { path, ratio } } }),
  equalize: (w: string) =>
    request<Layout>(`/api/layout${ws(w)}`, { method: 'PATCH', body: { equalize: true } }),
  split: (w: string, sourcePaneId: string, orientation: 'horizontal' | 'vertical', cwd?: string) =>
    request<Layout>(`/api/layout/pane${ws(w)}`, {
      body: { sourcePaneId, orientation, ...(cwd ? { cwd } : {}) },
    }),
  closePane: (w: string, paneId: string) =>
    request<Layout>(`/api/layout/pane/${q(paneId)}${ws(w)}`, { method: 'DELETE' }),
  setActiveTab: (w: string, paneId: string, tabId: string) =>
    request<Layout>(`/api/layout/pane/${q(paneId)}${ws(w)}`, {
      method: 'PATCH',
      body: { activeTabId: tabId },
    }),
  createTab: (w: string, paneId: string, body: { panelType?: PanelType; name?: string; command?: string }) =>
    request<Tab>(`/api/layout/pane/${q(paneId)}/tabs${ws(w)}`, { body }),
  closeTab: (w: string, paneId: string, tabId: string) =>
    request<null>(`/api/layout/pane/${q(paneId)}/tabs/${q(tabId)}${ws(w)}`, { method: 'DELETE' }),
  renameTab: (w: string, paneId: string, tabId: string, name: string) =>
    request<Layout>(`/api/layout/pane/${q(paneId)}/tabs/${q(tabId)}${ws(w)}`, {
      method: 'PATCH',
      body: { name },
    }),
  restartTab: (w: string, paneId: string, tabId: string) =>
    request<{ ok: true }>(`/api/layout/pane/${q(paneId)}/tabs/${q(tabId)}${ws(w)}`, { body: {} }),
  reorderTabs: (w: string, paneId: string, tabIds: string[]) =>
    request<Layout>(`/api/layout/pane/${q(paneId)}/tabs/order${ws(w)}`, {
      method: 'PATCH',
      body: { tabIds },
    }),
  moveTab: (w: string, fromPaneId: string, tabId: string, toPaneId: string, toIndex: number) =>
    request<Layout>(`/api/layout/pane/${q(fromPaneId)}/tabs/${q(tabId)}/move${ws(w)}`, {
      body: { toPaneId, toIndex },
    }),
};

// --- agents -------------------------------------------------------------------------------------------------------

export const agents = {
  /** Starts an agent with the runtime's hooks; the runtime puts its tab in the workspace's first pane. */
  start: (provider: Provider, workspaceId: string, prompt?: string) =>
    request<Conversation>(`${platformApi()}/conversations`, {
      body: { provider, workspaceId, ...(prompt ? { prompt } : {}) },
    }),
  send: (tabId: string, text: string, messageId: string) =>
    request<{ via: string }>(`${platformApi()}/prompt-text`, { body: { tabId, text, messageId } }),
  interrupt: (tabId: string) => request<null>(`${platformApi()}/interrupt`, { body: { tabId } }),
};

// --- machine ------------------------------------------------------------------------------------------------------

export const machine = {
  snapshot: () => request<{ machines: MachineSnapshot[] }>(`${platformApi()}/machines`),
  load: () => request<MachineLoad>(`${platformApi()}/machine/load`),
  stop: (pid: number, starttime: number) =>
    request<{ stopped: boolean }>(`${platformApi()}/machine/stop`, { body: { pid, starttime } }),
};
