# Two foreign MCP Apps views, unchanged

Both are examples of the public MCP Apps SDK, https://github.com/modelcontextprotocol/ext-apps, tag `v2.0.4`
(`examples/quickstart` and `examples/basic-server-vanillajs`), license MIT/Apache-2.0. They know nothing of Harnessly:
they use `App` from `@modelcontextprotocol/ext-apps`, which posts to `window.parent` and listens to `message`.

- `*/src/*.upstream` and `*/mcp-app.upstream.html` are the upstream files byte for byte (renamed so the web build does not compile them).
- `*/mcp-app.js` is the upstream `src/mcp-app.ts` bundled with esbuild (`--bundle --format=iife --minify
  --target=es2022`, `@modelcontextprotocol/ext-apps@2.0.4`), the build step the upstream does with Vite. Nothing of the
  code is edited.
- `*/mcp-app.html` is the upstream page with the one change a view server needs: the upstream inlines its script
  with vite-plugin-singlefile (the view CSP is `script-src 'self'`, which blocks inline code), so the page points at
  the bundled file with a classic `<script src>` (an opaque origin cannot load a module script) and, for the second
  one, a `<link>` to its stylesheet.
