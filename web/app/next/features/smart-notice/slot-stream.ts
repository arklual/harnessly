// The slot's event stream: the runtime says "the slot moved" over SSE, so the page does not ask every
// second and a half. While the stream is open the page only takes a slow look as a safety net; with no stream (an old
// runtime, a proxy that cuts it) the calm poll of slot-api stays as it was.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

export interface ISlotSource {
  addEventListener: (type: string, fn: () => void) => void;
  close: () => void;
  onopen: (() => void) | null;
  onerror: (() => void) | null;
}

export type TSlotStreamFactory = (url: string) => ISlotSource;

let factory: TSlotStreamFactory = (url) =>
  new EventSource(url, { withCredentials: true }) as unknown as ISlotSource;

/** For tests: the stream the page opens. Returns the one to put back. */
export const setSlotStreamFactory = (next: TSlotStreamFactory): TSlotStreamFactory => {
  const was = factory;
  factory = next;
  return was;
};

/** How slowly the page looks while the stream is open: the stream is the news, this only catches a missed one. */
export const SLOT_LIVE_MS = 30_000;

/**
 * Opens the stream; `moved` is called on each news, `live` when the stream opens or breaks.
 * Returns its stop. A page with no EventSource simply never goes live.
 */
export const openSlotStream = (
  m: MachineId,
  moved: () => void,
  live: (open: boolean) => void,
): (() => void) => {
  let source: ISlotSource;
  try {
    source = factory(runtimePath(platformPath('/notices/stream'), m));
  } catch {
    return () => {};
  }
  source.onopen = () => live(true);
  source.onerror = () => live(false);
  source.addEventListener('slot', moved);
  return () => {
    source.close();
    live(false);
  };
};
