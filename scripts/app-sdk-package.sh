#!/bin/sh
# Builds the npm package @harnessly/app-sdk (the app SDK's types and ES module) from internal/apps/sdk into a folder.
#   scripts/app-sdk-package.sh [OUT_DIR] [VERSION]     OUT_DIR defaults to dist/app-sdk-npm, VERSION to the latest v* tag
# It never publishes: the CI job app-sdk-npm (manual, on a tag) runs `npm publish` on the folder with the project's NPM_TOKEN.
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
out=${1:-"$root/dist/app-sdk-npm"}
version=${2:-$(git -C "$root" describe --tags --abbrev=0 --match 'v*' 2>/dev/null | sed 's/^v//')}
[ -n "$version" ] || { echo "app-sdk-package: no version: pass it as the second argument" >&2; exit 1; }
src="$root/internal/apps/sdk"
rm -rf "$out"
mkdir -p "$out"
cp "$src/app-sdk.js" "$out/index.js"
cp "$src/app-sdk.d.ts" "$out/index.d.ts"
cat > "$out/package.json" <<JSON
{
  "name": "@harnessly/app-sdk",
  "version": "$version",
  "description": "SDK of a Harnessly app's view: the bridge to the host (chat, storage, files, agents, ...), with types",
  "type": "module",
  "main": "index.js",
  "module": "index.js",
  "types": "index.d.ts",
  "exports": { ".": { "types": "./index.d.ts", "default": "./index.js" } },
  "sideEffects": false,
  "files": ["index.js", "index.d.ts", "README.md"],
  "license": "Apache-2.0",
  "publishConfig": { "access": "public" }
}
JSON
cat > "$out/README.md" <<'MD'
# @harnessly/app-sdk

The SDK of a view of a Harnessly app, with types. In a view the host already serves it as `window.harnessly`
(`<script src="/__hl/app-sdk.js">`); this package is for a bundler (Vite, esbuild) and for the editor's hints.

```ts
import { connect } from '@harnessly/app-sdk';

const hl = await connect({ protocol: 2 });
const answer = await hl.chat.send('Hello');
if (hl.refused(answer)) console.warn(answer.message);
```

Write an app: `harnessly app docs`. The version of the package is the version of Harnessly it was built from.
MD
echo "app-sdk-package: $out (@harnessly/app-sdk $version)"
