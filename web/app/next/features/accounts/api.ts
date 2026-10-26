// The requests of an agent's sign-in (Claude Code, Codex) under /api/v2/agents/<agent>/: the login the machine runs
// (POST/GET/DELETE login, POST login/code), the Codex API key (PUT key) and the sign-out (POST logout). A code or a key
// goes out in the body and is never read back.
import { getPage, sendBody, sendPage } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { parsePage, type MachineId, type Provider } from '@/next/protocol/v2';
import type { LoginVia } from '@/next/features/accounts/model';

const AUTH_TIMEOUT_MS = 15_000;
/** The address of a route of the agent; the machine goes into the query, so the path is given whole. */
const route = (machine: MachineId, provider: Provider, tail: string) =>
  machineApi(machine).http(`/agents/${provider}/${tail}`);

/** The mode the machine takes for a way of signing in: Codex has only the device login. */
const modeOf = (provider: Provider, via: LoginVia) => (provider === 'codex' ? 'device' : via);

export const loadLogin = (machine: MachineId, provider: Provider) =>
  getPage(route(machine, provider, 'login'), parsePage.login, AUTH_TIMEOUT_MS);

export const startLogin = (machine: MachineId, provider: Provider, via: LoginVia) =>
  sendBody(
    'POST',
    route(machine, provider, 'login'),
    { mode: modeOf(provider, via) },
    parsePage.login,
    AUTH_TIMEOUT_MS,
  );

export const cancelLogin = (machine: MachineId, provider: Provider) =>
  sendPage('DELETE', route(machine, provider, 'login'), parsePage.login, AUTH_TIMEOUT_MS);

export const sendLoginCode = (machine: MachineId, provider: Provider, id: string, code: string) =>
  sendBody('POST', route(machine, provider, 'login/code'), { id, code }, parsePage.login, AUTH_TIMEOUT_MS);

export const logoutAgent = (machine: MachineId, provider: Provider) =>
  sendPage('POST', route(machine, provider, 'logout'), parsePage.loginOK, AUTH_TIMEOUT_MS);

export const putAgentKey = (machine: MachineId, provider: Provider, key: string) =>
  sendBody('PUT', route(machine, provider, 'key'), { key }, parsePage.loginOK, AUTH_TIMEOUT_MS);
