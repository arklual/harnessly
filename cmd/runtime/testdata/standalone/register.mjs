import { register } from 'node:module';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

process.env.GOLDEN_STUB_DIR = path.dirname(fileURLToPath(import.meta.url));
register('../../../../internal/coreapi/layoutapi/testdata/gen/hooks.mjs', import.meta.url);
