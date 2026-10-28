// A hub in memory, for the gallery (?dev=<scenario>): it answers the contract's endpoints with plausible data and
// pushes the SSE events a real hub would, so the real screens can be looked at (and clicked through) without a
// hub. Nothing here is the product: the numbers, names and the code `123456` are examples.
import { ApiError, type ApiErrorCode, createApi } from '../lib/api';
import type { EventSourceLike, HubEnv } from '../lib/env';
import type {
  AgentStatus,
  Config,
  Invite,
  Job,
  JobStep,
  Login,
  Machine,
  Member,
  Me,
  OnboardingStep,
  Passkey,
  Policy,
  Profile,
  Provider,
  ProviderCheck,
  ProxySummary,
  Role,
  Space,
  User,
} from '../lib/types';

export const DEV_CODE = '123456';

export interface Scenario {
  config?: Partial<Config>;
  signedIn: boolean;
  /** Spaces of the person, and the machines of each. */
  spaces: { name: string; role: Role; step: OnboardingStep; machines: Machine[] }[];
  /** The provider servers do not answer from the machines (until a proxy is set). */
  blocked?: Provider[];
  /** The config call fails. */
  down?: boolean;
  /** An invitation token that is valid. */
  invite?: { token: string; kind: 'email' | 'link' };
  /** Milliseconds the answers wait, to see the loading states. */
  latency?: number;
  /** Every endpoint answers this code until the scenario ends. */
  failAll?: ApiErrorCode;
}

const iso = (offsetMs = 0) => new Date(Date.now() + offsetMs).toISOString();
let seq = 0;
const uid = (p: string) => `${p}_${(++seq).toString(36)}${Math.random().toString(36).slice(2, 6)}`;

const POLICY: Policy = {
  terminal: true,
  agentLogin: true,
  keys: true,
  env: true,
  install: true,
  runtimeSession: true,
  denyRoutes: [],
  source: 'default',
};

const NO_PROXY: ProxySummary = { all: null, claude: null, codex: null };

export const agentOf = (provider: Provider, over: Partial<AgentStatus> = {}): AgentStatus => ({
  provider,
  installed: false,
  version: null,
  login: 'unknown',
  authMode: null,
  overriddenBy: null,
  access: null,
  ...(provider === 'gemini' ? { soon: true as const } : {}),
  ...over,
});

export const READY: Pick<AgentStatus, 'installed' | 'version' | 'login' | 'authMode'> = {
  installed: true,
  version: '2.1.4',
  login: 'logged_in',
  authMode: 'subscription',
};

export const machineOf = (over: Partial<Machine> = {}): Machine => ({
  id: uid('m'),
  spaceId: null,
  name: 'build-01',
  hostname: 'build-01.example.com',
  os: 'linux',
  arch: 'amd64',
  unixUser: 'harnessly',
  profile: 'dir',
  via: 'command',
  status: 'online',
  connectedAt: iso(-3_600_000),
  lastSeenAt: iso(-5_000),
  agentVersion: '0.1.0',
  encKey: 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',
  policy: POLICY,
  preflight: {
    at: Date.now() - 60_000,
    osName: 'Ubuntu 22.04',
    os: 'linux',
    arch: 'amd64',
    cpus: 8,
    memTotal: 32 * 1024 ** 3,
    memAvailable: 21 * 1024 ** 3,
    disk: { path: '/home', total: 500 * 1024 ** 3, free: 412 * 1024 ** 3 },
    tools: {
      tmux: { installed: true, version: '3.4', compatible: true },
      git: { installed: true, version: '2.43' },
      node: { installed: true, version: '22.1' },
      claude: { installed: false, version: null },
      codex: { installed: false, version: null },
    },
    checks: [],
  },
  agents: [agentOf('claude'), agentOf('codex'), agentOf('gemini')],
  proxy: NO_PROXY,
  env: [],
  createdAt: iso(-86_400_000),
  ...over,
});

const slugOf = (name: string): string =>
  name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '') || 'space';

const checkOf = (
  provider: Provider,
  verdict: ProviderCheck['verdict'],
  via: 'direct' | 'proxy',
): ProviderCheck => {
  const host = provider === 'claude' ? 'api.anthropic.com' : 'api.openai.com';
  const ok = verdict === 'ok';
  return {
    provider,
    verdict,
    at: Date.now(),
    via,
    host,
    status: verdict === 'rejected' ? 403 : null,
    detail: null,
    hosts: [
      { host, dns: ok, tcp: ok, tls: ok, ms: ok ? 84 : 3000, error: ok ? null : 'i/o timeout' },
      {
        host: `statsig.${host.split('.').slice(1).join('.')}`,
        dns: ok,
        tcp: ok,
        tls: ok,
        ms: ok ? 91 : 3000,
        error: ok ? null : 'i/o timeout',
      },
    ],
  };
};

export class FakeHub {
  private user: User | null;
  private spaces: Space[] = [];
  private roles = new Map<string, Role>();
  private machines = new Map<string, Machine[]>();
  private invites = new Map<string, Invite[]>();
  private passkeys: Passkey[] = [];
  private blocked: Set<Provider>;
  private sources = new Map<string, Set<(type: string, data: unknown) => void>>();
  private keyChecks = new Map<string, number>();
  readonly config: Config;

  constructor(private readonly scenario: Scenario) {
    this.blocked = new Set(scenario.blocked);
    this.config = {
      version: '0.1.0',
      publicUrl: 'https://hub.example.com',
      edition: 'self-hosted',
      signup: 'open',
      auth: { email: { mode: 'smtp' }, passkey: true, github: true, yandex: true, oidc: [] },
      connect: {
        local: { available: true },
        command: true,
        ssh: { available: true, publicKey: null },
        teleport: false,
      },
      profiles: { dir: true, container: true, full: true },
      runtimeMode: 'path',
      termsUrl: 'https://example.com/terms',
      ...scenario.config,
    } as Config;
    this.user = scenario.signedIn ? this.newUser('anna@example.com') : null;
    for (const s of scenario.spaces) {
      const space = this.addSpace(s.name, s.role, s.step);
      this.machines.set(
        space.id,
        s.machines.map((m) => ({ ...m, spaceId: space.id })),
      );
    }
  }

  private newUser(email: string): User {
    return { id: uid('u'), email, name: email.split('@')[0] ?? email, isAdmin: false, createdAt: iso(-1e9) };
  }

  private addSpace(name: string, role: Role, step: OnboardingStep): Space {
    const space: Space = {
      id: uid('s'),
      slug: slugOf(name),
      name,
      role,
      createdAt: iso(),
      onboardingStep: step,
    };
    this.spaces.push(space);
    this.roles.set(space.id, role);
    this.machines.set(space.id, []);
    this.invites.set(space.id, []);
    return space;
  }

  // ---- events

  subscribe(spaceId: string, fn: (type: string, data: unknown) => void): () => void {
    const set = this.sources.get(spaceId) ?? new Set();
    set.add(fn);
    this.sources.set(spaceId, set);
    return () => set.delete(fn);
  }

  private emit(spaceId: string, type: string, data: unknown) {
    this.sources.get(spaceId)?.forEach((fn) => fn(type, data));
  }

  private later(ms: number, fn: () => void) {
    window.setTimeout(fn, ms);
  }

  private spaceOf(machineId: string): string {
    for (const [sid, list] of this.machines) if (list.some((m) => m.id === machineId)) return sid;
    throw this.err(404, 'not_found');
  }

  private put(machine: Machine): Machine {
    const sid = machine.spaceId;
    if (!sid) throw this.err(404, 'not_found');
    const list = this.machines.get(sid) ?? [];
    const at = list.findIndex((m) => m.id === machine.id);
    if (at < 0) list.push(machine);
    else list[at] = machine;
    this.machines.set(sid, list);
    this.emit(sid, 'machine', { machine });
    return machine;
  }

  private find(id: string): Machine {
    for (const list of this.machines.values()) {
      const m = list.find((x) => x.id === id);
      if (m) return m;
    }
    throw this.err(404, 'not_found');
  }

  private err(status: number, code: ApiErrorCode, details: Record<string, unknown> = {}) {
    return new ApiError(status, code, '', details);
  }

  private job(
    machine: Machine,
    over: Pick<Job, 'kind' | 'provider'>,
    ids: JobStep['id'][],
    opts: { failAt?: JobStep['id']; error?: { code: string; message: string }; detail?: string } = {},
  ): string {
    const id = uid('j');
    const steps: JobStep[] = ids.map((s) => ({ id: s, state: 'wait', detail: null }));
    const send = (state: Job['state'], check: ProviderCheck | null = null) =>
      this.emit(machine.spaceId ?? '', 'job', {
        machineId: machine.id,
        job: {
          id,
          ...over,
          state,
          steps: steps.map((s) => ({ ...s })),
          error: state === 'failed' ? (opts.error ?? { code: 'provider_unreachable', message: '' }) : null,
          check,
        },
      });
    let i = 0;
    const step = () => {
      const cur = steps[i];
      if (!cur) return;
      cur.state = 'run';
      send('running');
      this.later(900, () => {
        if (opts.failAt === cur.id) {
          cur.state = 'fail';
          if (opts.detail) cur.detail = opts.detail;
          send('failed', over.provider ? checkOf(over.provider, 'network', 'direct') : null);
          return;
        }
        cur.state = 'ok';
        i += 1;
        if (i < steps.length) step();
        else send('done');
      });
    };
    this.later(300, step);
    return id;
  }

  // ---- the endpoints

  async handle(method: string, url: string, body: Record<string, unknown>): Promise<unknown> {
    if (this.scenario.latency) await new Promise((r) => window.setTimeout(r, this.scenario.latency));
    const [path = '', queryText = ''] = url.split('?');
    const query = new URLSearchParams(queryText);
    const sc = this.scenario;
    if (path === '/api/config') {
      if (sc.down) throw this.err(503, 'internal');
      return this.config;
    }
    if (sc.failAll && path !== '/api/me') throw this.err(500, sc.failAll);
    const m = (re: RegExp): RegExpMatchArray | null => path.match(re);
    let r: RegExpMatchArray | null;
    const need = (): User => {
      if (!this.user) throw this.err(401, 'unauthenticated');
      return this.user;
    };

    // auth
    if (method === 'POST' && path === '/api/auth/email/start') return { resendAfter: 30, expiresIn: 600 };
    if (method === 'POST' && path === '/api/auth/email/verify') {
      if (body.code !== DEV_CODE) throw this.err(422, 'invalid_code', { attemptsLeft: 4 });
      const isNew = !this.user && this.spaces.length === 0;
      this.user = this.user ?? this.newUser(String(body.email));
      return { user: this.user, isNew, next: this.spaces.length ? `/s/${this.spaces[0]?.slug}` : '/new' };
    }
    if (method === 'POST' && path === '/api/auth/passkey/login/begin')
      return { challengeId: 'c1', publicKey: {} };
    if (method === 'POST' && path === '/api/auth/passkey/login/finish') {
      this.user = this.user ?? this.newUser('anna@example.com');
      return { user: this.user, isNew: false, next: '/' };
    }
    if (method === 'POST' && path === '/api/auth/logout') {
      this.user = null;
      return undefined;
    }

    // me
    if (path === '/api/me') {
      const user = need();
      if (method === 'PATCH') {
        this.user = { ...user, name: String(body.name) };
        return { user: this.user };
      }
      return {
        user,
        identities: [
          { provider: 'email', email: user.email },
          { provider: 'github', email: user.email },
        ],
        passkeys: this.passkeys,
        spaces: this.spaces.map((s) => ({
          id: s.id,
          slug: s.slug,
          name: s.name,
          role: this.roles.get(s.id) ?? 'member',
        })),
      } satisfies Me;
    }
    if (method === 'POST' && path === '/api/me/passkeys/begin') return { challengeId: 'c2', publicKey: {} };
    if (method === 'POST' && path === '/api/me/passkeys/finish') {
      const passkey: Passkey = { id: uid('pk'), name: String(body.name), createdAt: iso(), lastUsedAt: null };
      this.passkeys.push(passkey);
      return { passkey };
    }
    if ((r = m(/^\/api\/me\/passkeys\/([^/]+)$/)) && method === 'DELETE') {
      this.passkeys = this.passkeys.filter((p) => p.id !== r?.[1]);
      return undefined;
    }
    if ((r = m(/^\/api\/me\/identities\/([^/]+)$/)) && method === 'DELETE') return undefined;

    // invites by token
    if ((r = m(/^\/api\/invites\/([^/]+)$/))) {
      const inv = sc.invite;
      const valid = inv?.token === r[1];
      return {
        spaceName: 'Acme',
        inviterName: 'Anna',
        kind: inv?.kind ?? 'link',
        email: inv?.kind === 'email' ? 'anna@example.com' : null,
        valid,
      };
    }
    if ((r = m(/^\/api\/invites\/([^/]+)\/accept$/)) && method === 'POST') {
      need();
      const space = this.spaces[0] ?? this.addSpace('Acme', 'member', 'done');
      return { space: { id: space.id, slug: space.slug, name: space.name, role: 'member' } };
    }

    // spaces
    if (path === '/api/spaces' && method === 'POST') {
      need();
      const space = this.addSpace(String(body.name), 'owner', 'machines');
      return { space };
    }
    if ((r = m(/^\/api\/spaces\/by-slug\/([^/]+)$/))) {
      need();
      const space = this.spaces.find((s) => s.slug === r?.[1]);
      if (!space) throw this.err(404, 'not_found');
      return { space: { ...space, role: this.roles.get(space.id) } };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)$/))) {
      need();
      const i = this.spaces.findIndex((s) => s.id === r?.[1]);
      const space = this.spaces[i];
      if (!space) throw this.err(404, 'not_found');
      if (method === 'PATCH') {
        const next = {
          ...space,
          ...(typeof body.name === 'string' ? { name: body.name } : {}),
          ...(typeof body.onboardingStep === 'string'
            ? { onboardingStep: body.onboardingStep as OnboardingStep }
            : {}),
        };
        this.spaces[i] = next;
        return { space: next };
      }
      return { space: { ...space, role: this.roles.get(space.id) } };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/members$/))) {
      const user = need();
      const members: Member[] = [
        {
          userId: user.id,
          email: user.email,
          name: user.name,
          role: this.roles.get(r[1] ?? '') ?? 'owner',
          joinedAt: iso(-1e9),
        },
        { userId: 'u_b', email: 'boris@example.com', name: 'Boris', role: 'member', joinedAt: iso(-5e8) },
      ];
      return { members };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/members\/([^/]+)$/))) {
      if (method === 'PATCH') {
        return {
          member: {
            userId: r[2],
            email: 'boris@example.com',
            name: 'Boris',
            role: body.role,
            joinedAt: iso(-5e8),
          },
        };
      }
      return undefined;
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/invites$/))) {
      const sid = r[1] ?? '';
      if (method === 'POST') {
        const emails = (body.emails as string[]) ?? [];
        const made = emails.map<Invite>((email) => ({
          id: uid('i'),
          kind: 'email',
          email,
          role: 'member',
          sent: true,
          maxUses: 1,
          uses: 0,
          expiresAt: iso(7 * 864e5),
          createdAt: iso(),
        }));
        this.invites.set(sid, [...(this.invites.get(sid) ?? []), ...made]);
        this.emit(sid, 'members', {});
        return { invites: made };
      }
      return { invites: this.invites.get(sid) ?? [] };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/invites\/link$/)) && method === 'POST') {
      const sid = r[1] ?? '';
      const invite: Invite = {
        id: uid('i'),
        kind: 'link',
        email: null,
        role: 'member',
        url: `https://hub.example.com/invite/${uid('t')}`,
        maxUses: null,
        uses: 0,
        expiresAt: iso(7 * 864e5),
        createdAt: iso(),
      };
      this.invites.set(sid, [...(this.invites.get(sid) ?? []), invite]);
      return { invite };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/invites\/([^/]+)$/)) && method === 'DELETE') {
      const sid = r[1] ?? '';
      this.invites.set(
        sid,
        (this.invites.get(sid) ?? []).filter((i) => i.id !== r?.[2]),
      );
      return undefined;
    }

    // machines of a space
    if ((r = m(/^\/api\/spaces\/([^/]+)\/machines$/))) {
      need();
      return { machines: this.machines.get(r[1] ?? '') ?? [] };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/machines\/codes$/)) && method === 'POST') {
      const sid = r[1] ?? '';
      const profile = body.profile as Profile;
      const code = `HL-${Math.random().toString(36).slice(2, 6).toUpperCase()}-${Math.random().toString(36).slice(2, 6).toUpperCase()}`;
      this.later(5000, () => {
        const pending = this.put(
          machineOf({
            spaceId: sid,
            name: 'new-server',
            status: 'pending',
            profile,
            lastSeenAt: null,
            connectedAt: null,
          }),
        );
        this.later(1800, () =>
          this.put({ ...pending, status: 'online', connectedAt: iso(), lastSeenAt: iso() }),
        );
      });
      return {
        code,
        expiresAt: iso(15 * 60_000),
        profile,
        command: `curl -fsSL https://hub.example.com/install.sh | sh -s -- --code ${code}`,
      };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/machines\/local$/)) && method === 'POST') {
      const sid = r[1] ?? '';
      return {
        machine: this.put(machineOf({ spaceId: sid, name: 'hub-host', via: 'local', profile: 'dir' })),
      };
    }
    if (m(/^\/api\/spaces\/([^/]+)\/machines\/ssh$/) && method === 'GET') {
      return { publicKey: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample harnessly' };
    }
    if ((r = m(/^\/api\/spaces\/([^/]+)\/machines\/ssh$/)) && method === 'POST') {
      const sid = r[1] ?? '';
      const machine = machineOf({
        spaceId: sid,
        name: String(body.host),
        via: 'ssh',
        status: 'pending',
        profile: body.profile as Profile,
        lastSeenAt: null,
        connectedAt: null,
      });
      if (!body.hostKeyFingerprint) {
        // The first request only shakes hands: the owner sees the server's fingerprint and repeats it.
        const fp = 'SHA256:' + 'Zm9yIHRoZSBtb2NrdXAgb25seSwgbm90IGEgcmVhbCBrZXk'.slice(0, 43);
        const jobId = this.job(machine, { kind: 'ssh-install', provider: null }, ['ssh'], {
          failAt: 'ssh',
          error: { code: 'host_key_unconfirmed', message: `ssh-ed25519 ${fp}` },
          detail: `ssh-ed25519 ${fp}`,
        });
        return { jobId };
      }
      this.put(machine);
      const jobId = this.job(machine, { kind: 'ssh-install', provider: null }, [
        'ssh',
        'preflight',
        'install',
        'online',
      ]);
      this.later(5000, () =>
        this.put({ ...machine, status: 'online', connectedAt: iso(), lastSeenAt: iso() }),
      );
      return { jobId };
    }

    // one machine
    if ((r = m(/^\/api\/machines\/([^/]+)$/))) {
      const machine = this.find(r[1] ?? '');
      if (method === 'PATCH') return { machine: this.put({ ...machine, name: String(body.name) }) };
      if (method === 'DELETE') {
        const sid = this.spaceOf(machine.id);
        this.machines.set(
          sid,
          (this.machines.get(sid) ?? []).filter((x) => x.id !== machine.id),
        );
        this.emit(sid, 'machineRemoved', { machineId: machine.id });
        return undefined;
      }
      return { machine };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/open$/))) return { url: `https://example.com/m/${r[1]}/` };
    if ((r = m(/^\/api\/machines\/([^/]+)\/preflight$/)) && method === 'POST')
      return { preflight: this.find(r[1] ?? '').preflight };
    if ((r = m(/^\/api\/machines\/([^/]+)\/agents$/))) {
      const machine = this.find(r[1] ?? '');
      if (machine.status !== 'online') throw this.err(409, 'machine_offline');
      return { agents: machine.agents };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/agents\/(claude|codex|gemini)\/install$/)) && method === 'POST') {
      const machine = this.find(r[1] ?? '');
      const provider = r[2] as Provider;
      if (provider === 'gemini') throw this.err(422, 'provider_off');
      const blocked = this.blocked.has(provider) && !this.proxied(machine, provider);
      const jobId = this.job(
        machine,
        { kind: 'agent-install', provider },
        ['system', 'provider', 'download', 'install', 'verify'],
        blocked ? { failAt: 'provider' } : {},
      );
      if (!blocked) {
        this.later(5200, () => {
          const cur = this.find(machine.id);
          this.put({
            ...cur,
            agents: cur.agents.map((a) =>
              a.provider === provider
                ? {
                    ...a,
                    installed: true,
                    version: '2.1.4',
                    login: 'logged_out',
                    access: checkOf(provider, 'ok', 'direct'),
                  }
                : a,
            ),
          });
        });
      }
      return { jobId };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/agents\/(claude|codex|gemini)\/check$/)) && method === 'POST') {
      const provider = r[2] as Provider;
      const machine = this.find(r[1] ?? '');
      const proxied =
        (typeof body.proxyUrl === 'string' && body.proxyUrl !== '') || this.proxied(machine, provider);
      const ok = !this.blocked.has(provider) || proxied;
      return { check: checkOf(provider, ok ? 'ok' : 'network', proxied ? 'proxy' : 'direct') };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/agents\/(claude|codex|gemini)\/login$/)) && method === 'POST') {
      const machine = this.find(r[1] ?? '');
      const provider = r[2] as Provider;
      const login: Login = {
        id: uid('l'),
        provider,
        method: body.method as 'link' | 'console',
        state: 'starting',
        url: null,
        userCode: null,
        needsCode: false,
        consoleUrl: null,
        expiresAt: null,
        error: null,
        status: null,
      };
      const send = (over: Partial<Login>) => {
        Object.assign(login, over);
        this.emit(machine.spaceId ?? '', 'login', { machineId: machine.id, login: { ...login } });
      };
      this.later(500, () => {
        if (login.method === 'console') {
          send({
            state: 'waiting',
            consoleUrl: `/m/${machine.id}/console/0123456789abcdef`,
            expiresAt: Date.now() + 10 * 60_000,
          });
          this.later(9000, () => this.finishLogin(machine.id, provider, send));
        } else {
          send({
            state: 'url',
            url: `https://claude.example.com/oauth/authorize?code=true&state=${login.id}`,
            userCode: provider === 'codex' ? 'ABCD-1234' : null,
            needsCode: provider === 'claude',
            expiresAt: Date.now() + 10 * 60_000,
          });
          if (provider === 'codex') this.later(9000, () => this.finishLogin(machine.id, provider, send));
        }
      });
      this.logins.set(login.id, send);
      return { login };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/logins\/([^/]+)\/code$/)) && method === 'POST') {
      const send = this.logins.get(r[2] ?? '');
      const machine = this.find(r[1] ?? '');
      if (!send) throw this.err(404, 'not_found');
      if (body.code === 'bad') {
        send({ state: 'failed', error: 'rejected' });
        return undefined;
      }
      send({ state: 'verifying' });
      this.later(1200, () => this.finishLogin(machine.id, 'claude', send));
      return undefined;
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/logins\/([^/]+)$/)) && method === 'DELETE') {
      this.logins.get(r[2] ?? '')?.({ state: 'cancelled' });
      return undefined;
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/agents\/(claude|codex|gemini)\/key$/)) && method === 'POST') {
      const provider = r[2] as Provider;
      const machine = this.find(r[1] ?? '');
      this.put({
        ...machine,
        agents: machine.agents.map((a) =>
          a.provider === provider ? { ...a, ...READY, authMode: 'api_key' } : a,
        ),
      });
      return { check: { provider, present: true, verdict: 'accepted', status: 200, saved: true } };
    }
    if (
      (r = m(/^\/api\/machines\/([^/]+)\/agents\/(claude|codex|gemini)\/key\/verify$/)) &&
      method === 'POST'
    ) {
      const provider = r[2] as Provider;
      const key = `${r[1]}:${provider}`;
      const n = (this.keyChecks.get(key) ?? 0) + 1;
      this.keyChecks.set(key, n);
      return {
        check: {
          provider,
          present: n > 1,
          verdict: n > 1 ? 'accepted' : 'absent',
          status: n > 1 ? 200 : null,
        },
      };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/proxy$/))) {
      const machine = this.find(r[1] ?? '');
      if (method === 'GET') return { proxy: machine.proxy };
      const scope = (method === 'PUT' ? body.scope : query.get('scope')) as keyof ProxySummary;
      let proxy: ProxySummary;
      if (method === 'PUT') {
        const u = new URL(String(body.url));
        const scheme = u.protocol.replace(':', '').replace('socks5h', 'socks5') as
          'http' | 'https' | 'socks5';
        proxy = {
          ...machine.proxy,
          [scope]: { scheme, host: u.host, bridge: scheme === 'socks5' ? '127.0.0.1:18181' : null },
        };
      } else proxy = { ...machine.proxy, [scope]: null };
      this.put({ ...machine, proxy });
      return { proxy };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/env$/)) && method === 'POST') {
      const machine = this.find(r[1] ?? '');
      const env = [...new Set([...machine.env, String(body.name)])];
      this.put({ ...machine, env });
      return { env };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/env\/([^/]+)$/)) && method === 'DELETE') {
      const machine = this.find(r[1] ?? '');
      const env = machine.env.filter((n) => n !== decodeURIComponent(r?.[2] ?? ''));
      this.put({ ...machine, env });
      return { env };
    }
    if ((r = m(/^\/api\/machines\/([^/]+)\/gitkey$/)) && method === 'POST') {
      return { publicKey: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleGitKey harnessly@build-01' };
    }
    throw this.err(404, 'not_found');
  }

  /** A proxy of the machine covers the provider (its own scope or all). */
  private proxied(machine: Machine, provider: Provider): boolean {
    const p = this.find(machine.id).proxy;
    return p.all !== null || (provider !== 'gemini' && p[provider] !== null);
  }

  private logins = new Map<string, (over: Partial<Login>) => void>();

  private finishLogin(machineId: string, provider: Provider, send: (over: Partial<Login>) => void) {
    const machine = this.find(machineId);
    const agents = machine.agents.map((a) => (a.provider === provider ? { ...a, ...READY } : a));
    const updated = this.put({ ...machine, agents });
    send({ state: 'done', status: updated.agents.find((a) => a.provider === provider) ?? null });
  }
}

const responseOf = (status: number, body: unknown): Response =>
  new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });

export const fakeEnv = (hub: FakeHub): HubEnv => {
  const fetcher = async (input: string, init?: RequestInit): Promise<Response> => {
    const body = init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : {};
    try {
      const out = await hub.handle(init?.method ?? 'GET', input, body);
      return out === undefined ? responseOf(204, undefined) : responseOf(200, out);
    } catch (e) {
      if (e instanceof ApiError)
        return responseOf(e.status, { error: e.code, message: e.message, details: e.details });
      throw e;
    }
  };
  return {
    api: createApi(fetcher),
    eventSource: (url): EventSourceLike => {
      const spaceId = decodeURIComponent(url.split('/')[3] ?? '');
      const listeners = new Map<string, ((ev: MessageEvent<string>) => void)[]>();
      const source: EventSourceLike = {
        onopen: null,
        onerror: null,
        addEventListener: (type, fn) => listeners.set(type, [...(listeners.get(type) ?? []), fn]),
        close: () => off(),
      };
      const off = hub.subscribe(spaceId, (type, data) => {
        const ev = { data: JSON.stringify(data) } as MessageEvent<string>;
        listeners.get(type)?.forEach((fn) => fn(ev));
      });
      window.setTimeout(() => source.onopen?.(new Event('open')), 50);
      return source;
    },
    passkeys: { supported: () => true, get: async () => ({}), create: async () => ({}) },
    // The real sealing is exercised in seal.test.ts; the gallery only needs an envelope-shaped value.
    seal: async () => ({ v: 1, epk: 'AA', nonce: 'AA', ct: 'AA' }),
    canSeal: async () => true,
    copyText: async () => true,
    assign: (url) => console.info('[dev] assign', url),
    openBlank: () => ({ go: (url) => console.info('[dev] open', url), close: () => undefined }),
    openTab: (url) => console.info('[dev] open tab', url),
    now: () => Date.now(),
    origin: () => 'https://hub.example.com',
  };
};
