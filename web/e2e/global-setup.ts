import { startRuntime } from './runtime';

export default async function globalSetup() {
  const rt = await startRuntime();
  process.env.E2E_URL = rt.url;
  process.env.E2E_HOME = rt.home;
  process.env.E2E_LOG = rt.log;
  return async () => {
    await rt.stop();
  };
}
