import { execFileSync, spawn, type ChildProcess } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, openSync, rmSync, writeFileSync } from 'node:fs';
import { createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// A standalone runtime for the browser tests: the real binary with the page compiled in, a temporary home, a private
// tmux server (hs-test-*) and stand-ins for the agent CLIs. Nothing of the person's own tmux, ~/.claude or ~/.codex
// is touched.

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, '..', '..');

export interface Runtime {
  url: string;
  root: string;
  home: string;
  socket: string;
  log: string;
  stop: () => Promise<void>;
}

export function freePort(): Promise<number> {
  return new Promise((ok, fail) => {
    const srv = createServer();
    srv.once('error', fail);
    // A high port, away from the services a shared machine runs.
    srv.listen(0, '127.0.0.1', () => {
      const addr = srv.address();
      srv.close(() => (typeof addr === 'object' && addr ? ok(addr.port) : fail(new Error('no port'))));
    });
  });
}

async function waitFor(url: string, ms: number) {
  const until = Date.now() + ms;
  for (;;) {
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch {
      // not listening yet
    }
    if (Date.now() > until) throw new Error(`timed out waiting for ${url}`);
    await new Promise((r) => setTimeout(r, 200));
  }
}

export async function startRuntime(
  opts: {
    binary?: string;
    root?: string;
    port?: number;
    /** More environment for the runtime (the integrations' addresses and tokens). */
    env?: Record<string, string>;
  } = {},
): Promise<Runtime> {
  const root = opts.root ?? mkdtempSync(join(tmpdir(), 'hs-web-e2e-'));
  const home = join(root, 'home');
  const data = join(home, '.harnessly');
  const bin = join(home, 'bin');
  const project = join(home, 'project');
  const tmuxDir = join(root, 'tmux');
  for (const d of [bin, project, tmuxDir, join(home, '.claude'), join(home, '.codex')])
    mkdirSync(d, { recursive: true });
  for (const f of ['claude', 'codex']) copyFileSync(join(here, 'fixtures', f), join(bin, f));
  execFileSync('chmod', ['+x', join(bin, 'claude'), join(bin, 'codex')]);
  writeFileSync(join(project, 'README.md'), '# demo project\n');
  // A neutral prompt in the panes' login shells instead of the machine's user and host names.
  writeFileSync(join(home, '.bash_profile'), "PS1='\\w \\$ '\nexport PS1\n");

  let binary = opts.binary ?? process.env.RUNTIME_BIN;
  if (!binary) {
    binary = join(root, 'runtime');
    execFileSync('go', ['build', '-tags', 'webui', '-o', binary, './cmd/runtime'], {
      cwd: repo,
      stdio: 'inherit',
      env: { ...process.env, GOFLAGS: '-mod=readonly' },
    });
  }
  const port = opts.port ?? (await freePort());
  const socket = `hs-test-web-${process.pid}-${port}`;
  const env: Record<string, string> = {
    HOME: home,
    USER: 'agent',
    LOGNAME: 'agent',
    SHELL: '/bin/bash',
    LANG: 'C.UTF-8',
    TERM: 'xterm-256color',
    TMUX_TMPDIR: tmuxDir,
    PATH: `${bin}:/usr/local/bin:/usr/bin:/bin`,
    RUNTIME_GATE_LISTEN: `127.0.0.1:${port}`,
    RUNTIME_GATE_UPSTREAM: '',
    AGENT_DATA_DIR: data,
    RUNTIME_TMUX_SOCKET: socket,
    RUNTIME_WORKSPACE_DIR: project,
    RUNTIME_FIRST_WORKSPACE_NAME: 'Demo',
    RUNTIME_CLAUDE_LAUNCHER: join(bin, 'claude'),
    RUNTIME_AGENT_NICE: '10',
    RUNTIME_GATE_COUNT_SECONDS: '3600',
    ...opts.env,
  };
  // A page built since the binary was (pnpm build) is served from disk.
  if (process.env.RUNTIME_WEB_DIR) env.RUNTIME_WEB_DIR = resolve(process.env.RUNTIME_WEB_DIR);
  const log = join(root, 'runtime.log');
  const out = openSync(log, 'a');
  const child: ChildProcess = spawn(binary, [], { env, stdio: ['ignore', out, out], detached: true });
  const url = `http://127.0.0.1:${port}`;
  await waitFor(`${url}/api/health`, 30_000);

  const stop = async () => {
    if (child.exitCode === null) {
      child.kill('SIGTERM');
      await new Promise((r) => {
        const t = setTimeout(() => {
          child.kill('SIGKILL');
          r(null);
        }, 10_000);
        child.once('exit', () => {
          clearTimeout(t);
          r(null);
        });
      });
    }
    // The panes' shells write into the temporary home as they exit: the server goes, then its panes, then the files.
    const tmuxEnv: NodeJS.ProcessEnv = { ...process.env, TMUX_TMPDIR: tmuxDir };
    delete tmuxEnv.TMUX;
    let pids: string[] = [];
    try {
      pids = execFileSync('tmux', ['-L', socket, 'list-panes', '-a', '-F', '#{pane_pid}'], { env: tmuxEnv })
        .toString()
        .split(/\s+/)
        .filter(Boolean);
      execFileSync('tmux', ['-L', socket, 'kill-server'], { env: tmuxEnv, stdio: 'ignore' });
    } catch {
      // no server left
    }
    for (
      const until = Date.now() + 10_000;
      Date.now() < until && pids.some((p) => existsSync(`/proc/${p}`));
    ) {
      await new Promise((r) => setTimeout(r, 100));
    }
    if (!process.env.E2E_KEEP) rmSync(root, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 });
  };
  return { url, root, home, socket, log, stop };
}
