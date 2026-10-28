// How an agent on a machine reads as one row of the agents step: derived from the status the agent reported, the
// install job and the login the hub streamed. Pure, so every combination can be tested.
import type { AgentStatus, Job, Login, Machine, Provider } from './types';

export const PROVIDERS: readonly Provider[] = ['claude', 'codex', 'gemini'];

export interface ProviderInfo {
  name: string;
  vendor: string;
  /** The variable that carries the API key. */
  env: string;
  keyPlaceholder: string;
  /** What the person types in a console to sign in. */
  loginCommand: string;
  apiHost: string;
  /** The only hosts its sign-in page may be on. The machine reports the link, so the machine does not pick the host. */
  loginHosts: readonly string[];
}

export const PROVIDER_INFO: Record<Provider, ProviderInfo> = {
  claude: {
    name: 'Claude Code',
    vendor: 'Anthropic',
    env: 'ANTHROPIC_API_KEY',
    keyPlaceholder: 'sk-ant-…',
    loginCommand: 'claude auth login',
    apiHost: 'api.anthropic.com',
    loginHosts: ['claude.ai', 'console.anthropic.com', 'platform.claude.com'],
  },
  codex: {
    name: 'Codex',
    vendor: 'OpenAI',
    env: 'OPENAI_API_KEY',
    keyPlaceholder: 'sk-…',
    loginCommand: 'codex login --device-auth',
    apiHost: 'api.openai.com',
    loginHosts: ['auth.openai.com'],
  },
  gemini: {
    name: 'Gemini CLI',
    vendor: 'Google',
    env: 'GEMINI_API_KEY',
    keyPlaceholder: 'AIza…',
    loginCommand: 'gemini',
    apiHost: 'generativelanguage.googleapis.com',
    loginHosts: ['accounts.google.com'],
  },
};

/**
 * The sign-in link of a login, or null when it is not safe to show and open. The link comes from the machine (which
 * may be compromised): it must parse, be https on the default port, carry no credentials and sit on a host of the
 * provider's own sign-in pages (the exact host, not a subdomain). The returned string is the normalised URL.
 */
export const safeLoginUrl = (provider: Provider, raw: string | null | undefined): string | null => {
  if (!raw) return null;
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' || url.port !== '' || url.username !== '' || url.password !== '') return null;
  return PROVIDER_INFO[provider].loginHosts.includes(url.hostname) ? url.href : null;
};

export type RowKind = 'soon' | 'missing' | 'installing' | 'failed' | 'needsLogin' | 'ready';

export interface RowView {
  kind: RowKind;
  status: AgentStatus | null;
  /** The login state could not be read: «Нужно войти» but with a «Проверить» next to it. */
  unknown: boolean;
}

/** `starting`: the install was asked for and its first event has not arrived yet. */
export const rowView = (
  provider: Provider,
  status: AgentStatus | null,
  job: Job | null,
  starting: boolean,
): RowView => {
  const base = { status, unknown: false };
  if (provider === 'gemini' || status?.soon) return { ...base, kind: 'soon' };
  if (starting || job?.state === 'running') return { ...base, kind: 'installing' };
  if (!status || !status.installed) {
    return { ...base, kind: job?.state === 'failed' ? 'failed' : 'missing' };
  }
  if (status.login === 'logged_in') return { ...base, kind: 'ready' };
  return { ...base, kind: 'needsLogin', unknown: status.login === 'unknown' };
};

export const agentOf = (machine: Machine, provider: Provider): AgentStatus | null =>
  machine.agents.find((a) => a.provider === provider) ?? null;

/** An agent that can start a chat: installed and signed in. */
export const isReady = (a: AgentStatus): boolean => a.installed && a.login === 'logged_in' && !a.soon;

export const readyProviders = (machine: Machine): Provider[] =>
  machine.agents.filter(isReady).map((a) => a.provider);

export const FINAL_LOGIN: ReadonlySet<Login['state']> = new Set(['done', 'failed', 'expired', 'cancelled']);

/** A login that still waits for the person or the provider. */
export const loginActive = (login: Login | null): boolean => !!login && !FINAL_LOGIN.has(login.state);

/** Whether the install failed at the step that checks the provider's servers, and the check says why. */
export const providerStepFailed = (job: Job | null): boolean =>
  !!job && job.state === 'failed' && job.steps.some((s) => s.id === 'provider' && s.state === 'fail');
