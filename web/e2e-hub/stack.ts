import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  openSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { freePort } from '../e2e/runtime';

// The stack of the hub journey: a real hub (Postgres when E2E_DATABASE_URL is set, else SQLite), a fake OpenID
// Connect provider, and one machine: the real agent and runtime with a stand-in for the agent CLI. One cell of the
// matrix is one set of the hub's settings (CELLS below); the journey itself is the same in every cell.

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');

export type CellName = 'cloud' | 'selfhosted' | 'installer' | 'dev';

export interface Cell {
  name: CellName;
  /** The hub's host: it has to resolve for the agent too, which signs its dial for it, so localhost; only the machines' domain is mapped by the browser. */
  hubHost: string;
  /** The hub's settings beyond the common ones. */
  env: (o: { oidc: string }) => Record<string, string>;
  /** Machines open at <id>.<domain> instead of /m/<id>/. */
  runtimeDomain?: string;
  oidc: boolean;
  /** The first account signs up by itself, the second needs an invitation. */
  signup: 'open' | 'invite';
}

export const CELLS: Record<CellName, Cell> = {
  // The hosted service: its own domain for machines, no OIDC, organizations on.
  cloud: {
    name: 'cloud',
    hubHost: 'localhost',
    runtimeDomain: 'run.test',
    oidc: false,
    signup: 'open',
    env: () => ({
      HUB_EDITION: 'cloud',
      HUB_SIGNUP: 'open',
      HUB_ORGS: 'on',
      HUB_FEATURES_DEFAULT: 'ui.hx',
      HUB_RUNTIME_DOMAIN: 'run.test',
      HUB_ALLOW_SAME_SITE_RUNTIME: '0',
    }),
  },
  // A company's own hub: people come by invitation or through the company's provider; machines in path mode.
  selfhosted: {
    name: 'selfhosted',
    hubHost: 'localhost',
    oidc: true,
    signup: 'invite',
    env: ({ oidc }) => ({
      HUB_EDITION: 'self-hosted',
      HUB_SIGNUP: 'invite',
      HUB_ORGS: 'on',
      HUB_FEATURES_DEFAULT: 'ui.hx',
      HUB_OIDC_PROVIDERS: 'sso',
      HUB_OIDC_SSO_NAME: 'Example SSO',
      HUB_OIDC_SSO_ISSUER: oidc,
      HUB_OIDC_SSO_CLIENT_ID: 'e2e',
      HUB_OIDC_SSO_CLIENT_SECRET: 'e2e-secret-not-real',
      HUB_OIDC_EMAIL_TRUSTED: 'on',
    }),
  },
  // A clean install with the defaults: nothing set but where to listen and where the pages are.
  installer: {
    name: 'installer',
    hubHost: 'localhost',
    oidc: false,
    signup: 'open',
    env: () => ({ HUB_FEATURES_DEFAULT: 'ui.hx' }),
  },
  // What the repository's own Makefile and compose file give a developer.
  dev: {
    name: 'dev',
    hubHost: 'localhost',
    oidc: false,
    signup: 'open',
    env: () => ({
      HUB_SIGNUP: 'open',
      HUB_ORGS: 'shadow',
      HUB_FEATURES_DEFAULT: 'ui.hx',
      HUB_MACHINE_TRUST: 'on',
    }),
  },
};

export interface Stack {
  cell: Cell;
  hub: string;
  root: string;
  oidc: string;
  logs: { hub: string; runtime: string; agent: string; oidc: string };
  /** The newest sign-in code the hub wrote to its log for this address. */
  emailCode: (email: string) => string;
  /** Registers the machine with a one-time code (the owner asked the hub for it in the browser's session) and runs it. */
  registerMachine: (o: { code: string; name: string }) => Promise<{ machineId: string; basePath: string }>;
  stop: () => Promise<void>;
}

async function waitFor(url: string, ms: number) {
  const until = Date.now() + ms;
  for (;;) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // not listening yet
    }
    if (Date.now() > until) throw new Error(`timed out waiting for ${url}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

function build(out: string, pkg: string) {
  execFileSync('go', ['build', '-o', out, pkg], {
    cwd: repo,
    stdio: 'inherit',
    env: { ...process.env, CGO_ENABLED: '0', GOFLAGS: '-mod=readonly' },
  });
}

export async function startStack(name: CellName): Promise<Stack> {
  const cell = CELLS[name];
  const root = mkdtempSync(join(tmpdir(), `hub-journey-${name}-`));
  const procs: ChildProcess[] = [];
  const spawnLogged = (cmd: string, args: string[], env: Record<string, string>, log: string) => {
    const fd = openSync(log, 'a');
    const p = spawn(cmd, args, { env, stdio: ['ignore', fd, fd], detached: true });
    procs.push(p);
    return p;
  };
  const bin = join(root, 'bin');
  mkdirSync(bin, { recursive: true });
  const [hubBin, agentBin, runtimeBin] = [join(bin, 'hub'), join(bin, 'agent'), join(bin, 'runtime')];
  if (process.env.E2E_BIN_DIR) {
    for (const [dst, n] of [
      [hubBin, 'hub'],
      [agentBin, 'agent'],
      [runtimeBin, 'runtime'],
    ] as const)
      copyFileSync(join(process.env.E2E_BIN_DIR, n), dst);
  } else {
    build(hubBin, './cmd/hub');
    build(agentBin, './cmd/agent');
    build(runtimeBin, './cmd/runtime');
  }

  // 1. the provider
  const oidcPort = await freePort();
  const oidc = `http://127.0.0.1:${oidcPort}`;
  const logs = {
    hub: join(root, 'hub.log'),
    runtime: join(root, 'runtime.log'),
    agent: join(root, 'agent.log'),
    oidc: join(root, 'oidc.log'),
  };
  spawnLogged(
    'node',
    [join(here, 'fake-oidc.mjs')],
    { PATH: process.env.PATH ?? '', FAKE_OIDC_PORT: String(oidcPort), FAKE_OIDC_CLIENT_ID: 'e2e' },
    logs.oidc,
  );
  await waitFor(`${oidc}/health`, 15_000);

  // 2. the hub
  const hubPort = await freePort();
  const hub = `http://${cell.hubHost}:${hubPort}`;
  const hubData = join(root, 'hub');
  mkdirSync(hubData, { recursive: true });
  const hubEnv: Record<string, string> = {
    PATH: process.env.PATH ?? '',
    HOME: root,
    HUB_LISTEN: `127.0.0.1:${hubPort}`,
    HUB_PUBLIC_URL: hub,
    HUB_DATA_DIR: hubData,
    HUB_LOG_LEVEL: 'info',
    HUB_WEB_DIR: resolve(repo, 'web', 'dist-hub'),
    HUB_SECRET_KEY: 'e2e-journey-secret-key-32-bytes-long!!',
    HUB_ADMIN_EMAILS: 'owner@example.com',
    // The sign-in codes are read from the hub's log; the cloud edition refuses that without this.
    HUB_MAIL_LOG: 'allow',
    ...cell.env({ oidc }),
  };
  if (process.env.E2E_DATABASE_URL) hubEnv.HUB_DATABASE_URL = process.env.E2E_DATABASE_URL;
  spawnLogged(hubBin, [], hubEnv, logs.hub);
  try {
    await waitFor(`http://127.0.0.1:${hubPort}/api/config`, 150_000);
  } catch (e) {
    // The job's own output has to say why: the log files are in the artifacts, but this is what one reads first.
    let tail = '';
    try {
      tail = readFileSync(logs.hub, 'utf8').split('\n').slice(-25).join('\n');
    } catch {
      /* no log */
    }
    throw new Error(`${(e as Error).message}\n--- hub log (tail) ---\n${tail}`, { cause: e });
  }

  const emailCode = (email: string) => {
    const lines = readFileSync(logs.hub, 'utf8')
      .split('\n')
      .filter((l) => l.includes('DEV email code') && l.includes(email));
    const m = lines.at(-1)?.match(/code=(\d+)/);
    if (!m) throw new Error(`no sign-in code for ${email} in the hub's log`);
    return m[1];
  };

  // 3. the machine: registered with a code, then the runtime and the agent
  const registerMachine: Stack['registerMachine'] = async ({ code, name: machineName }) => {
    const direct = hub;
    const home = join(root, 'machine');
    const work = join(home, 'work');
    const tmux = join(root, 'tmux');
    for (const d of [work, tmux, join(home, 'bin'), join(home, '.claude'), join(home, '.codex')])
      mkdirSync(d, { recursive: true });
    chmodSync(tmux, 0o700);
    for (const f of ['claude', 'codex']) {
      copyFileSync(join(repo, 'web', 'e2e', 'fixtures', f), join(home, 'bin', f));
      chmodSync(join(home, 'bin', f), 0o755);
    }
    writeFileSync(join(work, 'README.md'), '# demo project\n');
    const menv: Record<string, string> = {
      PATH: `${join(home, 'bin')}:/usr/local/bin:/usr/bin:/bin`,
      HOME: home,
      USER: 'agent',
      LANG: 'C.UTF-8',
      TERM: 'xterm-256color',
      SHELL: '/bin/bash',
      TMUX_TMPDIR: tmux,
      AGENT_DATA_DIR: join(home, '.harnessly'),
      AGENT_POLICY_FILE: join(home, 'policy.toml'),
      AGENT_WORK_DIR: work,
    };
    // The agent signs its dial for the hub's own host (the public URL), so that name has to resolve for it: localhost.
    execFileSync(
      agentBin,
      [
        'register',
        '--hub',
        direct,
        '--token',
        code,
        '--profile',
        'full',
        '--via',
        'command',
        '--name',
        machineName,
      ],
      { env: menv, stdio: 'pipe' },
    );
    const mf = JSON.parse(readFileSync(join(home, '.harnessly', 'machine.json'), 'utf8')) as {
      machineId: string;
      runtimeBasePath: string;
    };
    const [rt, ag] = [await freePort(), await freePort()];
    spawnLogged(
      runtimeBin,
      [],
      {
        ...menv,
        RUNTIME_GATE_LISTEN: `127.0.0.1:${rt}`,
        RUNTIME_GATE_UPSTREAM: '',
        RUNTIME_APP_BASE_PATH: mf.runtimeBasePath,
        RUNTIME_HUB_URL: direct,
        RUNTIME_PREVIEW_LISTEN: 'off',
        RUNTIME_WORKSPACE_DIR: work,
        RUNTIME_WEB_DIR: resolve(repo, 'web', 'dist'),
        RUNTIME_CLAUDE_LAUNCHER: join(home, 'bin', 'claude'),
        RUNTIME_LOCALE: 'en',
        RUNTIME_TMUX_SOCKET: `hs-journey-${process.pid}-${rt}`,
      },
      logs.runtime,
    );
    spawnLogged(agentBin, [], { ...menv, AGENT_PORT: String(ag), RUNTIME_PORT: String(rt) }, logs.agent);
    return { machineId: mf.machineId, basePath: mf.runtimeBasePath };
  };

  const stop = async () => {
    for (const p of procs)
      if (p.pid)
        try {
          process.kill(-p.pid, 'SIGTERM');
        } catch {
          /* gone */
        }
    await new Promise((r) => setTimeout(r, 1500));
    for (const p of procs)
      if (p.pid)
        try {
          process.kill(-p.pid, 'SIGKILL');
        } catch {
          /* gone */
        }
    try {
      execFileSync('tmux', ['-S', join(root, 'tmux'), 'kill-server'], { stdio: 'ignore' });
    } catch {
      /* none */
    }
    // The logs stay for the CI artifacts (E2E_KEEP=1, or E2E_LOGS_DIR to copy them out).
    if (process.env.E2E_LOGS_DIR) {
      mkdirSync(process.env.E2E_LOGS_DIR, { recursive: true });
      for (const [k, f] of Object.entries(logs))
        try {
          copyFileSync(f, join(process.env.E2E_LOGS_DIR, `${name}-${k}.log`));
        } catch {
          /* no log */
        }
    }
    if (!process.env.E2E_KEEP) rmSync(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
  };
  return { cell, hub, root, oidc, logs, emailCode, registerMachine, stop };
}
