// Prints the static theme stylesheet of PrimeNG's preset (the --p-* variables PrimeNG would inject in a browser).
import { createRequire } from 'node:module';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
const dir = path.resolve(process.argv[2]);
const req = createRequire(path.join(dir, 'noop.js'));
const imp = (p) => import(pathToFileURL(req.resolve(p)).href);
const { Theme } = await imp('@primeuix/styled');
const Aura = (await imp('@primeuix/themes/aura')).default;
Theme.setTheme({ preset: Aura, options: {} });
const c = Theme.getCommon();
process.stdout.write([c.primitive?.css, c.semantic?.css, c.global?.css].filter(Boolean).join('\n'));
