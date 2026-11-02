# Third-party notices

The built page (`web/dist`, and the runtime binary when it is built with the `webui` tag) bundles these packages. Their
licences are kept in `node_modules/<package>/LICENSE`; `pnpm licenses list --prod` lists every bundled package.

| Package                                                                        | Licence                                                                     |
| ------------------------------------------------------------------------------ | --------------------------------------------------------------------------- |
| react, react-dom, scheduler                                                    | MIT                                                                         |
| @xterm/xterm, @xterm/addon-fit                                                 | MIT                                                                         |
| react-markdown, remark-gfm and their unified/micromark/mdast/hast dependencies | MIT                                                                         |
| lucide-react                                                                   | ISC                                                                         |
| motion                                                                         | MIT                                                                         |
| zustand, swr, sonner, nanoid, dayjs                                            | MIT                                                                         |
| @base-ui/react, @floating-ui/*                                                 | MIT                                                                         |
| cmdk (with the @radix-ui dialog parts it uses)                                 | MIT                                                                         |
| use-stick-to-bottom, react-hotkeys-hook, socket.io-client                      | MIT                                                                         |
| @git-diff-view/react, @git-diff-view/core                                      | MIT                                                                         |
| highlight.js, lowlight                                                         | BSD-3-Clause, MIT                                                           |
| echarts, zrender                                                               | Apache-2.0, BSD-3-Clause                                                    |
| mermaid and its d3, dagre, cytoscape and khroma dependencies (diagrams)        | MIT, ISC, BSD-3-Clause, Apache-2.0                                          |
| web-vitals                                                                     | Apache-2.0                                                                  |
| @huggingface/transformers, onnxruntime-web (speech recognition in the browser) | Apache-2.0, MIT                                                             |
| react-icons (brand marks from Simple Icons)                                    | MIT (icons CC0-1.0)                                                         |
| tailwindcss (generated styles), @tailwindcss/typography                        | MIT                                                                         |
| @fontsource-variable/inter (Inter)                                             | SIL Open Font License 1.1, see `public/licenses/inter-OFL-1.1.txt`          |
| @fontsource-variable/jetbrains-mono (JetBrains Mono)                           | SIL Open Font License 1.1, see `public/licenses/jetbrains-mono-OFL-1.1.txt` |

The fonts' licence texts are copied into the built page under `licenses/`, next to the font files.

## Test fixtures

Not in the built page: `app/next/features/app-views/fixtures/ext-apps/` keeps two examples of
[MCP Apps](https://github.com/modelcontextprotocol/ext-apps) (tag `v2.0.4`, MIT/Apache-2.0) as unchanged foreign views
for the bridge's tests. Their bundled `mcp-app.js` files carry `@modelcontextprotocol/ext-apps`,
`@modelcontextprotocol/sdk` and `zod` (MIT). The fixtures' README says how they were built.
