// The gallery: `/?dev=<scenario>` runs the real screens against an in-memory hub (fake-hub.ts), `/?dev` lists the
// scenarios. The e-mail code in every scenario is 123456. This chunk is fetched only with `?dev`.
import type { HubEnv } from '../lib/env';
import { agentOf, FakeHub, fakeEnv, machineOf, READY, type Scenario } from './fake-hub';

const online = (over: Parameters<typeof machineOf>[0] = {}) => machineOf({ name: 'build-01', ...over });

const readyAgents = [agentOf('claude', READY), agentOf('codex', READY), agentOf('gemini')];
const half = [agentOf('claude', { ...READY, login: 'logged_out' }), agentOf('codex'), agentOf('gemini')];

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
  new: {
    title: 'Название пространства и приглашения',
    path: '/new',
    scenario: { signedIn: true, spaces: [] },
  },
  'machines-empty': {
    title: 'Машины, пока нет ни одной',
    path: '/s/acme/setup/machines',
    scenario: { signedIn: true, spaces: [SPACE('machines', [])] },
  },
  'machines-cloud': {
    title: 'Машины, облачная редакция',
    path: '/s/acme/setup/machines',
    scenario: {
      signedIn: true,
      spaces: [SPACE('machines', [])],
      config: {
        edition: 'cloud',
        connect: {
          local: { available: false },
          command: true,
          ssh: { available: false, publicKey: null },
          teleport: false,
        },
      },
    },
  },
  'machines-one': {
    title: 'Машины, одна подключена',
    path: '/s/acme/setup/machines',
    scenario: { signedIn: true, spaces: [SPACE('machines', [online()])] },
  },
  'agents-none': {
    title: 'Агенты, ничего не стоит',
    path: '/s/acme/setup/agents',
    scenario: {
      signedIn: true,
      spaces: [
        SPACE('agents', [online({ agents: [agentOf('claude'), agentOf('codex'), agentOf('gemini')] })]),
      ],
    },
  },
  'agents-half': {
    title: 'Агенты, Claude стоит, но не вошёл',
    path: '/s/acme/setup/agents',
    scenario: { signedIn: true, spaces: [SPACE('agents', [online({ agents: half })])] },
  },
  'agents-blocked': {
    title: 'Агенты, серверы провайдера недоступны',
    path: '/s/acme/setup/agents',
    scenario: {
      signedIn: true,
      blocked: ['claude'],
      spaces: [
        SPACE('agents', [online({ agents: [agentOf('claude'), agentOf('codex'), agentOf('gemini')] })]),
      ],
    },
  },
  'agents-two': {
    title: 'Агенты, две машины',
    path: '/s/acme/setup/agents',
    scenario: {
      signedIn: true,
      spaces: [
        SPACE('agents', [online({ agents: readyAgents }), online({ name: 'ci-runner', agents: half })]),
      ],
    },
  },
  access: {
    title: 'Доступы',
    path: '/s/acme/setup/access',
    scenario: { signedIn: true, spaces: [SPACE('access', [online({ agents: readyAgents })])] },
  },
  done: {
    title: 'Всё готово',
    path: '/s/acme/setup/done',
    scenario: {
      signedIn: true,
      spaces: [SPACE('done', [online({ agents: readyAgents, env: ['GITHUB_TOKEN', 'GH_TOKEN'] })])],
    },
  },
  home: {
    title: 'Пространство, владелец',
    path: '/s/acme',
    scenario: {
      signedIn: true,
      spaces: [
        SPACE('done', [
          online({ agents: readyAgents }),
          online({ name: 'ci-runner', status: 'offline', agents: half }),
        ]),
      ],
    },
  },
  'home-member': {
    title: 'Пространство, участник',
    path: '/s/acme',
    scenario: { signedIn: true, spaces: [SPACE('done', [online({ agents: readyAgents })], 'member')] },
  },
  'home-empty': {
    title: 'Пространство без машин',
    path: '/s/acme',
    scenario: { signedIn: true, spaces: [SPACE('done', [])] },
  },
  settings: {
    title: 'Настройки пространства',
    path: '/s/acme/settings',
    scenario: { signedIn: true, spaces: [SPACE('done', [online({ agents: readyAgents })])] },
  },
  account: { title: 'Аккаунт', path: '/settings', scenario: { signedIn: true, spaces: [SPACE('done', [])] } },
  slow: {
    title: 'Медленные ответы (состояния загрузки)',
    path: '/s/acme',
    scenario: { signedIn: true, latency: 2500, spaces: [SPACE('done', [online({ agents: readyAgents })])] },
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
