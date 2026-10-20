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
| web-vitals                                                                     | Apache-2.0                                                                  |
| @huggingface/transformers, onnxruntime-web (speech recognition in the browser) | Apache-2.0, MIT                                                             |
| react-icons (brand marks from Simple Icons)                                    | MIT (icons CC0-1.0)                                                         |
| tailwindcss (generated styles), @tailwindcss/typography                        | MIT                                                                         |
| @fontsource-variable/inter (Inter)                                             | SIL Open Font License 1.1, see `public/licenses/inter-OFL-1.1.txt`          |
| @fontsource-variable/jetbrains-mono (JetBrains Mono)                           | SIL Open Font License 1.1, see `public/licenses/jetbrains-mono-OFL-1.1.txt` |

The fonts' licence texts are copied into the built page under `licenses/`, next to the font files.
