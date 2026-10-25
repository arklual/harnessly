// The look the owner has given the design (device, browser bars, fit, lock, page) as a guest link carries it: the
// view the browser remembers for this design, flattened to scalars and kept under 2 KB. The gateway stores it as it is
// and hands it back in index.json. Without a remembered view there is nothing to carry.
import type { IGuestView } from '@/next/features/designly/access/api';
import { DEVICES } from '@/next/features/designly/devices';
import { readView, VIEW_DEFAULT } from '@/next/features/designly/view-memory';

const MAX_BYTES = 2048;

const bytes = (view: IGuestView): number => new TextEncoder().encode(JSON.stringify(view)).length;

/** The view of a design as a guest link takes it; undefined when the design has no remembered view. */
export const guestViewOf = (design: string): IGuestView | undefined => {
  const memory = readView(design);
  // A design with nothing kept (or nothing valid) reads as the default itself.
  if (memory === VIEW_DEFAULT) return undefined;
  const device = DEVICES.find((d) => d.id === memory.device);
  let width: number | null = null;
  let height: number | null = null;
  if (memory.device === 'fill' && memory.size) {
    width = memory.size.w;
    height = memory.size.h;
  } else if (device?.vw !== undefined && device.vh !== undefined) {
    width = memory.rot ? device.vh : device.vw;
    height = memory.rot ? device.vw : device.vh;
  }
  const free = memory.fit === null;
  const view: IGuestView = {
    v: 1,
    device: memory.device,
    rotated: memory.rot,
    width,
    height,
    chrome: memory.chrome,
    browser: memory.browser,
    fit: memory.fit,
    zoom: free ? memory.zoom.s : null,
    panX: free ? memory.zoom.x : null,
    panY: free ? memory.zoom.y : null,
    locked: memory.locked,
    page: memory.page,
  };
  return bytes(view) <= MAX_BYTES ? view : { ...view, page: null };
};
