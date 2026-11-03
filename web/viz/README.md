# viz

Time-series charts for the hub: a smooth wave chart on a canvas with HTML overlays (change markers, hint bubbles),
a legend, a zoom control, a range navigator and a status layer. Ported from the internal design system viz addon;
the design board's bundle of that addon is the reference for how it looks and moves.

Licensed under Apache-2.0, like the rest of the repository.

## Use

```tsx
import { VizWave, createSource } from '@viz';

const source = createSource({ hourly, extent, latency: 0 });

<VizWave
  source={source}
  metric="cost"
  granularity="day"
  series={[{ id: 'now', label: 'This period', slot: 0 }]}
  options={{ mode: 'overlay', unit: { kind: 'currency', currency: 'USD' }, points: 'none', zones: false }}
  navigator
  zoomControls
  size="l"
/>;
```

Exports of `@viz`: `VizWave`, `createSource`, `dailySource`, `formatBucketTitle`, `formatValue`,
`renderMarkerIcon`, `seriesColor` and the types of their props.

The module is heavy for a page that does not draw a chart (canvas engine, scales, tooltips): load it with a dynamic
`import('@viz')` so it lands in a chunk of its own. It imports its stylesheet (`viz.css`) itself.

## Layout

| Folder                  | What is in it                                                                                               |
| ----------------------- | ----------------------------------------------------------------------------------------------------------- |
| `model/`                | pure logic: time grid and granularity, aggregation, scales, curves, markers, effect of a change, formatting |
| `data/`                 | the `source` contract (`createSource`, `dailySource`) and the hooks that load frames                        |
| `engine/`               | the canvas engines (wave, navigator), animation, colours                                                    |
| `ui/`                   | React components: board, chart, legend, zoom, navigator, tooltip content, marker icons                      |
| `internal/`             | the few small pieces the addon took from the design system (hint bubble, icon button, sheet, class helper)  |
| `viz.css`, `tokens.css` | plain CSS; `tokens.css` defines the `--t-*` values the chart reads under `.viz-root`, light and dark        |

## Theme

The chart follows the page theme: `data-theme` or `data-hx-theme` on `<html>`, or the `dark` class there. Without any of
them it is light. The canvas listens to the same attributes and repaints on change.

## Motion

The canvas is redrawn by `requestAnimationFrame` only while something moves; hovering does not render React again.
With `prefers-reduced-motion: reduce` the transitions are skipped and the chart settles at once.

## Pixel parity

`web/e2e/parity/viz/` renders the design board's own bundle and this module with the same data and compares the
screenshots (rest, a day hovered, a marker hovered; teams and compare modes; light and dark):

```sh
cd web
BOARD_DIR=/path/to/board VENDOR_DIR=/path/to/react18-umd pnpm exec playwright test -c e2e/parity/viz/playwright.config.ts
```

`VENDOR_DIR` holds `react.production.min.js` and `react-dom.production.min.js` of React 18.3 (the reference bundle
expects them as globals). The check is not part of the regular e2e run and is left out of the public export.
