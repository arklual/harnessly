// The scene of a look's background, drawn off the page's thread on the OffscreenCanvas the page handed over
// (backdrop.tsx): the feed, the typing and the scroll never wait for it.
import { SceneRunner, type ISceneSize, type ISceneSpec } from '@/next/features/looks/runner';

export type TSceneMessage =
  | { k: 'init'; canvas: OffscreenCanvas }
  | { k: 'set'; spec: ISceneSpec | null; size: ISceneSize }
  | { k: 'play'; on: boolean; still: boolean }
  | { k: 'stop' };

/** What the worker tells the page: the detail went down (the page marks it on the canvas). */
export type TSceneReply = { k: 'detail'; detail: number };

let runner: SceneRunner | null = null;

self.onmessage = (event: MessageEvent<TSceneMessage>) => {
  const msg = event.data;
  switch (msg.k) {
    case 'init':
      runner = new SceneRunner(msg.canvas, 'worker', (detail) =>
        self.postMessage({ k: 'detail', detail } satisfies TSceneReply),
      );
      break;
    case 'set':
      runner?.set(msg.spec, msg.size);
      break;
    case 'play':
      runner?.play(msg.on, msg.still);
      break;
    case 'stop':
      runner?.stop();
      break;
  }
};
