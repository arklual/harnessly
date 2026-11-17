// Where a live effect of a look runs (docs/looks.md, «Живые эффекты»): the frame document the server keeps at
// /api/v2/me/looks/fx-frame (internal/wire/looks.go) — a sandbox with no allow-same-origin (an opaque origin: no
// cookies, no storage, no reach into the page) under a policy of its own with no network at all. A frame of its own URL
// does not inherit the page's policy, which on the hub allows no inline script (a srcdoc frame would). The page sends it
// the hx SDK (fx/runtime.js) and the effect's code in one message; whatever the effect does, all it sees is what
// fx-layer.tsx posts it and all it can change is its own transparent canvas.
import runtime from '@/next/features/looks/fx/runtime.js?raw';
import { sendBlob } from '@/next/core/net/http';
import { clusterApi } from '@/next/core/net/machine';
import type { TFxPreset } from '@/next/features/looks/types';

export const FX_SANDBOX = 'allow-scripts';

type TRaw = Promise<{ default: string }>;
const join = (parts: TRaw[]): Promise<string> =>
  Promise.all(parts).then((mods) => mods.map((m) => m.default).join('\n;\n'));

/** The built-in effects that also paint under the chat (they run a back layer whatever the look's fx.back says). */
export const FX_BACK: ReadonlySet<TFxPreset> = new Set(['newyear', 'elves', 'weather']);

/** The code of the built-in effects, each loaded only when a look wears it: its libraries, its pictures, itself. */
export const FX_LOAD: Record<TFxPreset, () => Promise<string>> = {
  snowplay: () =>
    join([
      import('@/next/features/looks/fx/snow-lib.js?raw'),
      import('@/next/features/looks/fx/art-snow.js?raw'),
      import('@/next/features/looks/fx/snowplay.js?raw'),
    ]),
  nightlamp: () => join([import('@/next/features/looks/fx/nightlamp.js?raw')]),
  laser: () => join([import('@/next/features/looks/fx/laser.js?raw')]),
  newyear: () =>
    join([
      import('@/next/features/looks/fx/snow-lib.js?raw'),
      import('@/next/features/looks/fx/scene-lib.js?raw'),
      import('@/next/features/looks/fx/art-bow.js?raw'),
      import('@/next/features/looks/fx/art-newyear.js?raw'),
      import('@/next/features/looks/fx/newyear.js?raw'),
    ]),
  elves: () =>
    join([
      import('@/next/features/looks/fx/snow-lib.js?raw'),
      import('@/next/features/looks/fx/scene-lib.js?raw'),
      import('@/next/features/looks/fx/art-bow.js?raw'),
      import('@/next/features/looks/fx/art-elves.js?raw'),
      import('@/next/features/looks/fx/elves.js?raw'),
    ]),
  weather: () =>
    join([
      import('@/next/features/looks/fx/snow-lib.js?raw'),
      import('@/next/features/looks/fx/art-weather.js?raw'),
      import('@/next/features/looks/fx/weather.js?raw'),
    ]),
};

/** The first line a script kept by /api/v2/me/looks starts with (internal/wire/looks.go). */
export const FX_MAGIC = '//hx-fx v1\n';
/** The largest effect a person uploads, in bytes (the server takes the magic line on top). */
export const MAX_FX = 256 * 1024 - FX_MAGIC.length;

/** The frame's address. */
export const fxFrameUrl = (): string => clusterApi('/me/looks/fx-frame');

/** What the page sends the frame once it boots: the SDK first, then the effect (without the stored script's magic line). */
export const fxCodeMessage = (code: string, layer: 'front' | 'back') => ({
  hx: 'code',
  layer,
  code: `${runtime}\n;\n${code.startsWith(FX_MAGIC) ? code.slice(FX_MAGIC.length) : code}\n`,
});

/** Keeps an effect's code with the person's pictures (internal/wire/looks.go): its hash, or null. */
export const uploadFx = async (code: string): Promise<string | null> => {
  if (new TextEncoder().encode(code).length > MAX_FX) return null;
  const res = await sendBlob(
    clusterApi('/me/looks'),
    new Blob([FX_MAGIC + code], { type: 'text/plain' }),
    (v) =>
      typeof v === 'object' && v !== null && typeof (v as { hash?: unknown }).hash === 'string'
        ? (v as { hash: string }).hash
        : null,
    30000,
  );
  return 'ok' in res ? res.value : null;
};
