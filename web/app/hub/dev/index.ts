// The gallery: `/?dev=<scenario>` runs the real screens against an in-memory hub (fake-hub.ts), `/?dev` lists the
// scenarios. The e-mail code in every scenario is 123456. This chunk is fetched only with `?dev`.
import type { HubEnv } from '../lib/env';
import { agentOf, FakeHub, fakeEnv, machineOf, READY, type Scenario } from './fake-hub';

const online = (over: Parameters<typeof machineOf>[0] = {}) => machineOf({ name: 'build-01', ...over });

const readyAgents = [agentOf('claude', READY), agentOf('codex', READY), agentOf('gemini')];

interface Entry {
  title: string;
  path: string;
  scenario: Scenario;
}

const SPACE = (
  step: 'machines' | 'agents' | 'access' | 'done',
  machines: ReturnType<typeof machineOf>[],
  role: 'owner' | 'member' = 'owner',
) => ({
  name: 'Acme',
  role,
  step,
  machines,
});

export const SCENARIOS: Record<string, Entry> = {
  signin: { title: 'Вход', path: '/signin', scenario: { signedIn: false, spaces: [] } },
  signup: { title: 'Регистрация', path: '/signup', scenario: { signedIn: false, spaces: [] } },
  'signup-closed': {
    title: 'Регистрация только по приглашению',
    path: '/signup',
    scenario: { signedIn: false, spaces: [], config: { signup: 'invite' } },
  },
  'signin-log': {
    title: 'Вход, почта без SMTP',
    path: '/signin',
    scenario: {
      signedIn: false,
      spaces: [],
      config: {
        auth: {
          email: { mode: 'log' },
          passkey: false,
          github: false,
          yandex: false,
          oidc: [{ id: 'corp', name: 'Corp SSO' }],
        },
      },
    },
  },
  'signin-oidc': {
    title: 'Вход только через провайдера (HUB_AUTH_EMAIL=off)',
    path: '/signin',
    scenario: {
      signedIn: false,
      spaces: [],
      config: {
        auth: {
          email: { mode: 'off' },
          passkey: false,
          github: false,
          yandex: false,
          oidc: [{ id: 'corp', name: 'Corp SSO' }],
        },
      },
    },
  },
  invite: {
    title: 'Приглашение по ссылке',
    path: '/invite/abc',
    scenario: { signedIn: false, spaces: [], invite: { token: 'abc', kind: 'link' } },
  },
  'invite-gone': {
    title: 'Приглашение недействительно',
    path: '/invite/zzz',
    scenario: { signedIn: false, spaces: [] },
  },
  switches: {
    title: 'Переключатели выката (администратор)',
    path: '/switches',
    scenario: { signedIn: true, admin: true, spaces: [SPACE('machines', [online()])] },
  },
  org: {
    title: 'Организация: хаб на моках (?as=admin|lead|member)',
    path: '/o/acme/all/overview',
    scenario: { signedIn: true, spaces: [SPACE('done', [online({ agents: readyAgents })])] },
  },
  'org-step': {
    title: 'Шаг «Организация» (HRN-536, моки)',
    path: '/org/new',
    scenario: { signedIn: true, spaces: [SPACE('done', [online({ agents: readyAgents })])] },
  },
  down: { title: 'Hub не отвечает', path: '/', scenario: { signedIn: false, spaces: [], down: true } },
};

export const devEnv = (name: string): HubEnv => {
  const entry = SCENARIOS[name] ?? SCENARIOS.signin;
  if (!entry) throw new Error('no scenarios');
  // The scenario's first screen, unless the address already says where to go.
  if (location.pathname === '/' || location.pathname === '/index.html') {
    history.replaceState(null, '', `${entry.path}${location.search}`);
  }
  return fakeEnv(new FakeHub(entry.scenario));
};

/** `/?dev` with no name: a plain list of the scenarios. */
export const renderIndex = (root: HTMLElement) => {
  const list = document.createElement('ul');
  list.className = 'hub-dev-index';
  for (const [name, e] of Object.entries(SCENARIOS)) {
    const li = document.createElement('li');
    const a = document.createElement('a');
    a.href = `${e.path}?dev=${name}`;
    a.textContent = `${name} — ${e.title}`;
    li.append(a);
    list.append(li);
  }
  root.append(list);
};
