# Third-party notices

The built page (`web/dist`, and the runtime binary when it is built with the `webui` tag) bundles these packages. Their
licences are kept in `node_modules/<package>/LICENSE`; `pnpm licenses list --prod` lists every bundled package.

| Package                                                                        | Licence                                                                     |
| ------------------------------------------------------------------------------ | --------------------------------------------------------------------------- |
| react, react-dom, scheduler                                                    | MIT                                                                         |
| @xterm/xterm, @xterm/addon-fit                                                 | MIT                                                                         |
| react-markdown, remark-gfm and their unified/micromark/mdast/hast dependencies | MIT                                                                         |
| lucide-react                                                                   | ISC                                                                         |
| @fontsource-variable/inter (Inter)                                             | SIL Open Font License 1.1, see `public/licenses/inter-OFL-1.1.txt`          |
| @fontsource-variable/jetbrains-mono (JetBrains Mono)                           | SIL Open Font License 1.1, see `public/licenses/jetbrains-mono-OFL-1.1.txt` |

The fonts' licence texts are copied into the built page under `licenses/`, next to the font files.
