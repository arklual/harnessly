import { spawn, type ChildProcess } from 'child_process';
import { DEFAULT_LOCAL_PORT } from './config';
import { isRuntimeHealthy } from './health';

const delay = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

export interface LocalRuntime {
  port: number;
  stop: () => void;
}

export const localPortFromEnv = (env: Record<string, string | undefined>): number => {
  const port = Number(env.HARNESSLY_DESKTOP_PORT ?? env.PORT);
  return Number.isInteger(port) && port > 0 && port < 65536 ? port : DEFAULT_LOCAL_PORT;
};

/**
 * Local mode attaches to a runtime that already listens (the installer's launchd or systemd unit).
 * Only when HARNESSLY_RUNTIME_BIN names a binary does the app start its own and stop it on quit.
 */
export const ensureLocalRuntime = async (
  env: Record<string, string | undefined>,
  waitMs = 30_000,
): Promise<LocalRuntime | null> => {
  const port = localPortFromEnv(env);
  if (await isRuntimeHealthy(port)) return { port, stop: () => undefined };
  const bin = env.HARNESSLY_RUNTIME_BIN;
  if (!bin) return null;
  const child: ChildProcess = spawn(bin, [], {
    env: { ...env, RUNTIME_GATE_LISTEN: `127.0.0.1:${port}`, RUNTIME_GATE_UPSTREAM: '', RUNTIME_APP_BASE_PATH: '/' },
    stdio: 'ignore',
    detached: false,
  });
  child.on('error', () => undefined);
  for (let waited = 0; waited < waitMs; waited += 500) {
    if (child.exitCode !== null) return null;
    if (await isRuntimeHealthy(port)) return { port, stop: () => child.kill('SIGTERM') };
    await delay(500);
  }
  child.kill('SIGTERM');
  return null;
};
