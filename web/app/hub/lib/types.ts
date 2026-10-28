// The hub API's types, as docs/hub/api.md writes them. Times are RFC 3339 strings on the hub API and epoch
// milliseconds inside the agent types (PreflightReport.at, ProviderCheck.at, Login.expiresAt).

export type User = { id: string; email: string; name: string; isAdmin: boolean; createdAt: string };
export type Role = 'owner' | 'member';
export type OnboardingStep = 'machines' | 'agents' | 'access' | 'done';
export type Profile = 'dir' | 'container' | 'full';
export type Provider = 'claude' | 'codex' | 'gemini';

export type SpaceRef = { id: string; slug: string; name: string; role: Role };
export type Space = SpaceRef & { createdAt: string; onboardingStep: OnboardingStep };

export type Policy = {
  terminal: boolean;
  agentLogin: boolean;
  keys: boolean;
  env: boolean;
  install: boolean;
  runtimeSession: boolean;
  denyRoutes: string[];
  source: string;
};

export type Check = { id: string; level: 'ok' | 'warn' | 'fail'; message: string };

export type PreflightReport = {
  at: number;
  osName: string;
  os: 'linux' | 'darwin';
  arch: 'amd64' | 'arm64';
  cpus: number;
  memTotal: number;
  memAvailable: number;
  disk: { path: string; total: number; free: number };
  tools: {
    tmux: { installed: boolean; version: string | null; compatible: boolean };
    git: { installed: boolean; version: string | null };
    node: { installed: boolean; version: string | null };
    claude: { installed: boolean; version: string | null };
    codex: { installed: boolean; version: string | null };
  };
  checks: Check[];
};

export type ProviderHost = {
  host: string;
  dns: boolean;
  tcp: boolean;
  tls: boolean;
  ms: number;
  error: string | null;
};

export type ProviderCheck = {
  provider: Provider;
  verdict: 'ok' | 'network' | 'rejected';
  at: number;
  via: 'direct' | 'proxy';
  host: string;
  status: number | null;
  detail: string | null;
  hosts: ProviderHost[];
};

export type AgentStatus = {
  provider: Provider;
  soon?: true;
  installed: boolean;
  version: string | null;
  login: 'logged_in' | 'logged_out' | 'unknown';
  authMode: 'subscription' | 'api_key' | 'cloud' | null;
  overriddenBy: string | null;
  access: ProviderCheck | null;
};

export type ProxyScope = 'all' | 'claude' | 'codex';
export type ProxyEntry = { scheme: 'http' | 'https' | 'socks5'; host: string; bridge: string | null };
export type ProxySummary = { all: ProxyEntry | null; claude: ProxyEntry | null; codex: ProxyEntry | null };

export type Machine = {
  id: string;
  spaceId: string | null;
  name: string;
  hostname: string;
  os: 'linux' | 'darwin';
  arch: 'amd64' | 'arm64';
  unixUser: string;
  profile: Profile;
  via: 'command' | 'ssh' | 'local' | 'container';
  status: 'pending' | 'online' | 'offline';
  connectedAt: string | null;
  lastSeenAt: string | null;
  agentVersion: string;
  encKey: string;
  policy: Policy | null;
  preflight: PreflightReport | null;
  agents: AgentStatus[];
  proxy: ProxySummary;
  env: string[];
  createdAt: string;
};

export type JobStepId =
  'system' | 'provider' | 'download' | 'install' | 'verify' | 'ssh' | 'preflight' | 'online';
export type JobStep = { id: JobStepId; state: 'wait' | 'run' | 'ok' | 'fail'; detail: string | null };
export type Job = {
  id: string;
  kind: 'agent-install' | 'ssh-install';
  provider: Provider | null;
  state: 'running' | 'done' | 'failed';
  steps: JobStep[];
  error: { code: string; message: string } | null;
  check: ProviderCheck | null;
};

export type LoginState =
  'starting' | 'url' | 'waiting' | 'verifying' | 'done' | 'failed' | 'expired' | 'cancelled';
export type Login = {
  id: string;
  provider: Provider;
  method: 'link' | 'console';
  state: LoginState;
  url: string | null;
  userCode: string | null;
  needsCode: boolean;
  consoleUrl: string | null;
  expiresAt: number | null;
  error: string | null;
  status: AgentStatus | null;
};

export type KeyCheck = {
  provider: Provider;
  present: boolean;
  verdict: 'accepted' | 'rejected' | 'network' | 'absent';
  status: number | null;
  saved?: boolean;
};

export type Envelope = { v: 1; epk: string; nonce: string; ct: string };

export type Invite = {
  id: string;
  kind: 'email' | 'link';
  email: string | null;
  role: Role;
  url?: string;
  sent?: boolean;
  maxUses: number | null;
  uses: number;
  expiresAt: string;
  createdAt: string;
};

export type Member = { userId: string; email: string; name: string; role: Role; joinedAt: string };
export type Passkey = { id: string; name: string; createdAt: string; lastUsedAt: string | null };

export type Config = {
  version: string;
  publicUrl: string;
  edition: 'self-hosted' | 'cloud';
  signup: 'open' | 'invite';
  auth: {
    email: { mode: 'smtp' | 'log' };
    passkey: boolean;
    github: boolean;
    yandex: boolean;
    oidc: { id: string; name: string }[];
  };
  connect: {
    local: { available: boolean };
    command: true;
    ssh: { available: boolean; publicKey: null };
    teleport: false;
  };
  profiles: { dir: true; container: boolean; full: true };
  runtimeMode: 'path' | 'subdomain';
  termsUrl: string | null;
  sites?: { enabled: boolean; scheme?: 'subdomain' | 'path'; domain?: string };
};

export type SiteAccess = 'team' | 'link';
export type SiteVersion = {
  version: number;
  url: string;
  fileCount: number;
  totalBytes: number;
  source: string;
  createdBy: string;
  createdAt: string;
  current: boolean;
};
export type Site = {
  id: string;
  space: string;
  name: string;
  address: string;
  access: SiteAccess;
  spa: boolean;
  currentVersion: number;
  createdBy: string;
  updatedBy: string;
  createdAt: string;
  updatedAt: string;
  versions?: SiteVersion[];
};
export type SiteDeploy = {
  version: number;
  url: string;
  versionUrl: string;
  current: boolean;
  fileCount: number;
  totalBytes: number;
};
export type SiteToken = {
  id: string;
  name: string;
  createdBy: string;
  createdAt: string;
  lastUsedAt: string | null;
  token?: string;
};

export type SignedIn = { user: User; isNew: boolean; next: string };

export type Me = {
  user: User;
  identities: { provider: string; email: string }[];
  passkeys: Passkey[];
  spaces: SpaceRef[];
};

export type MachineCode = { code: string; expiresAt: string; profile: Profile; command: string };

/** An event of a space's SSE stream (docs/hub/api.md, "Space events"). */
export type SpaceEvent =
  | { type: 'machine'; data: { machine: Machine } }
  | { type: 'machineRemoved'; data: { machineId: string } }
  | { type: 'job'; data: { machineId: string; job: Job } }
  | { type: 'login'; data: { machineId: string; login: Login } }
  | { type: 'members'; data: Record<string, never> };
