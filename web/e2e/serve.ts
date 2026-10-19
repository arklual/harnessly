// Starts a standalone runtime with the page for a person to look at: `pnpm exec tsx`-free, run with
// `node --experimental-strip-types e2e/serve.ts`. Stops on Ctrl-C.
import { startRuntime } from './runtime.ts';

const rt = await startRuntime();
console.log(`runtime: ${rt.url}  (log ${rt.log}, home ${rt.home})`);
const stop = () => void rt.stop().then(() => process.exit(0));
process.on('SIGINT', stop);
process.on('SIGTERM', stop);
setInterval(() => undefined, 1 << 30);
