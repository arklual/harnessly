import { spawn, type ChildProcess } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { existsSync, mkdirSync, openSync } from 'node:fs';
import { join, resolve } from 'node:path';

// The sibling services for the integration test: boards and artifacts from their checkouts (E2E_BOARDS_DIR,
// E2E_ARTIFACTS_DIR, each with its dependencies installed and boards built), on loopback, with the fs storage driver
// in the test's temporary folder. Without both checkouts the integration test is skipped.

export interface Services {
  boards: string;
  artifacts: string;
  env: Record<string, string>;
  /** A viewer token of the catalog, for signing in to it in the browser. */
  readToken: string;
  stop: () => Promise<void>;
}

// Fresh for every run, so nothing that looks like a credential is committed.
const secret = () => randomBytes(24).toString('hex');
const tokens = {
  boardsRuntime: secret(),
  boardsAgent: secret(),
  artifactsEditor: secret(),
  artifactsViewer: secret(),
};

export function servicesWanted(): { boardsDir: string; artifactsDir: string } | null {
  const boardsDir = process.env.E2E_BOARDS_DIR;
  const artifactsDir = process.env.E2E_ARTIFACTS_DIR;
  if (!boardsDir || !artifactsDir) return null;
  if (
    !existsSync(join(boardsDir, 'bin', 'boards.mjs')) ||
    !existsSync(join(artifactsDir, 'bin', 'artifacts.mjs'))
  )
    throw new Error('E2E_BOARDS_DIR and E2E_ARTIFACTS_DIR must be checkouts of boards and artifacts');
  return { boardsDir: resolve(boardsDir), artifactsDir: resolve(artifactsDir) };
}

async function healthy(url: string, ms: number) {
  const until = Date.now() + ms;
  for (;;) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // not listening yet
    }
    if (Date.now() > until) throw new Error(`timed out waiting for ${url}`);
    await new Promise((r) => setTimeout(r, 200));
  }
}

export async function startServices(opts: {
  root: string;
  boardsDir: string;
  artifactsDir: string;
  boardsPort: number;
  artifactsPort: number;
  /** The runtime's origin: the page that embeds the services. */
  embedder: string;
}): Promise<Services> {
  const boards = `http://127.0.0.1:${opts.boardsPort}`;
  const artifacts = `http://127.0.0.1:${opts.artifactsPort}`;
  const children: ChildProcess[] = [];
  const run = (dir: string, script: string, name: string, env: Record<string, string>) => {
    mkdirSync(join(opts.root, name), { recursive: true });
    const out = openSync(join(opts.root, `${name}.log`), 'a');
    const child = spawn('nice', ['-n', '10', process.execPath, script, 'serve'], {
      cwd: dir,
      env: { PATH: process.env.PATH ?? '/usr/bin:/bin', HOME: opts.root, NODE_ENV: 'production', ...env },
      stdio: ['ignore', out, out],
      detached: true,
    });
    children.push(child);
  };
  run(opts.boardsDir, 'bin/boards.mjs', 'boards', {
    PORT: String(opts.boardsPort),
    BOARDS_HOST: '127.0.0.1',
    BOARDS_PUBLIC_URL: boards,
    BOARDS_STORAGE: 'fs',
    BOARDS_DATA_DIR: join(opts.root, 'boards'),
    BOARDS_AUTH: 'token',
    BOARDS_API_TOKENS: `${tokens.boardsRuntime},${tokens.boardsAgent}`,
    BOARDS_SESSION_SECRET: secret(),
    BOARDS_METRICS_PORT: '0',
    BOARDS_FRAME_ANCESTORS: `'self' ${opts.embedder}`,
    BOARDS_CORS_ORIGINS: opts.embedder,
  });
  run(opts.artifactsDir, 'bin/artifacts.mjs', 'artifacts', {
    ARTIFACTS_PORT: String(opts.artifactsPort),
    ARTIFACTS_HOST: '127.0.0.1',
    ARTIFACTS_PUBLIC_URL: artifacts,
    ARTIFACTS_STORAGE: 'fs',
    ARTIFACTS_DATA_DIR: join(opts.root, 'artifacts'),
    ARTIFACTS_AUTH: 'token',
    ARTIFACTS_TOKEN: tokens.artifactsEditor,
    ARTIFACTS_READ_TOKEN: tokens.artifactsViewer,
    ARTIFACTS_SECRET: secret(),
    ARTIFACTS_FRAME_ANCESTORS: opts.embedder,
    ARTIFACTS_LOG_FORMAT: 'text',
  });
  const stop = async () => {
    await Promise.all(
      children.map(
        (c) =>
          new Promise((r) => {
            if (c.exitCode !== null || !c.pid) return r(null);
            c.once('exit', () => r(null));
            // nice execs node, so the pid is the server's own
            process.kill(c.pid, 'SIGTERM');
            setTimeout(() => {
              if (c.exitCode === null && c.pid) process.kill(c.pid, 'SIGKILL');
              r(null);
            }, 5000);
          }),
      ),
    );
  };
  try {
    await healthy(`${boards}/healthz`, 30_000);
    await healthy(`${artifacts}/healthz`, 30_000);
  } catch (err) {
    await stop();
    throw err;
  }
  return {
    boards,
    artifacts,
    readToken: tokens.artifactsViewer,
    env: {
      RUNTIME_BOARDS_URL: boards,
      RUNTIME_BOARDS_TOKEN: tokens.boardsRuntime,
      RUNTIME_BOARDS_AGENT_TOKEN: tokens.boardsAgent,
      RUNTIME_ARTIFACTS_URL: artifacts,
      RUNTIME_ARTIFACTS_TOKEN: tokens.artifactsEditor,
      RUNTIME_ARTIFACTS_READ_TOKEN: tokens.artifactsViewer,
      RUNTIME_OWNER_NAME: 'Ada',
    },
    stop,
  };
}
