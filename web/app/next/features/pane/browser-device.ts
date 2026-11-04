// What the page of a browser tab is looked at on: the pane itself («По окну»), a phone or a tablet. A device is the
// page's CSS size; the screen is scaled down to fit the pane whole, never up, and can be turned on its side.

export const BROWSER_DEVICE = { fill: 'fill', phone: 'phone', tablet: 'tablet' } as const;
export type TBrowserDevice = (typeof BROWSER_DEVICE)[keyof typeof BROWSER_DEVICE];

export const BROWSER_DEVICES: readonly TBrowserDevice[] = [
  BROWSER_DEVICE.fill,
  BROWSER_DEVICE.phone,
  BROWSER_DEVICE.tablet,
];

/** The CSS sizes, upright (the phone's is a common iPhone's, the tablet's an iPad Air's). */
const SIZES: Record<Exclude<TBrowserDevice, 'fill'>, { w: number; h: number }> = {
  phone: { w: 390, h: 844 },
  tablet: { w: 820, h: 1180 },
};

export interface IBrowserScreen {
  device: TBrowserDevice;
  /** On its side: width and height change places. */
  turned: boolean;
}

export const FILL_SCREEN: IBrowserScreen = { device: BROWSER_DEVICE.fill, turned: false };

/** The page's CSS size on the device; null for «По окну». */
export const screenSize = (screen: IBrowserScreen): { w: number; h: number } | null => {
  if (screen.device === BROWSER_DEVICE.fill) return null;
  const { w, h } = SIZES[screen.device];
  return screen.turned ? { w: h, h: w } : { w, h };
};

/** How much the screen is scaled to show whole in an area: at most 1. */
export const screenFit = (size: { w: number; h: number }, area: { w: number; h: number }): number => {
  if (area.w <= 0 || area.h <= 0) return 1;
  return Math.min(1, area.w / size.w, area.h / size.h);
};
