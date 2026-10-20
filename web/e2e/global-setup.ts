import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import { freePort, startRuntime } from './runtime';
import { type Services, servicesWanted, startServices } from './services';

export default async function globalSetup() {
  // A page that already runs (the compose workspace, say) is tested as it is: E2E_URL and, for the side pane,
  // E2E_BOARDS_URL, E2E_ARTIFACTS_URL and E2E_ARTIFACTS_READ_TOKEN name it.
  if (process.env.E2E_URL) return async () => undefined;
  const root = mkdtempSync(join(tmpdir(), 'hs-web-e2e-'));
  const port = await freePort();
  const wanted = servicesWanted();
  let services: Services | null = null;
  if (wanted) {
    services = await startServices({
      root,
      ...wanted,
      boardsPort: await freePort(),
      artifactsPort: await freePort(),
      embedder: `http://127.0.0.1:${port}`,
    });
    process.env.E2E_BOARDS_URL = services.boards;
    process.env.E2E_ARTIFACTS_URL = services.artifacts;
    process.env.E2E_ARTIFACTS_READ_TOKEN = services.readToken;
  }
  const rt = await startRuntime({ root, port, env: services?.env });
  process.env.E2E_URL = rt.url;
  process.env.E2E_HOME = rt.home;
  process.env.E2E_LOG = rt.log;
  return async () => {
    await services?.stop();
    await rt.stop();
  };
}
