import { t } from '@/lib/i18n';

/**
 * Device frames of the preview. A fixed device is drawn as a body around its screen, the browser
 * chrome takes its share of the screen, and the page frame gets exactly the viewport the real browser leaves visible.
 * The whole device is scaled down to fit the stage, never up.
 */
export interface ISize {
  width: number;
  height: number;
}

export interface IPoint {
  x: number;
  y: number;
}

export const DEVICE_FRAME = { none: 'none', notch: 'notch', island: 'island', punch: 'punch' } as const;
export type TDeviceFrame = (typeof DEVICE_FRAME)[keyof typeof DEVICE_FRAME];

export const DEVICE_PLATFORM = { desktop: 'desktop', ios: 'ios', android: 'android' } as const;
export type TDevicePlatform = (typeof DEVICE_PLATFORM)[keyof typeof DEVICE_PLATFORM];

export const PREVIEW_BROWSER = { none: 'none', safari: 'safari', chrome: 'chrome' } as const;
export type TPreviewBrowserId = (typeof PREVIEW_BROWSER)[keyof typeof PREVIEW_BROWSER];

export const PREVIEW_BROWSER_LABELS = {
  none: t('lib.hlPreviewDevices.noBrowserBars'),
  safari: 'Safari iOS',
  chrome: 'Chrome Android',
} satisfies Record<TPreviewBrowserId, string>;

export interface IDeviceSpec {
  id: string;
  label: string;
  platform: TDevicePlatform;
  /** The screen in CSS pixels (portrait); null for the fluid frame that fills the stage. */
  screen: ISize | null;
  frame: TDeviceFrame;
  /** Body thickness around the screen. */
  bezel: number;
  /** Corner radius of the screen. */
  radius: number;
  /** Status bar height: the safe area at the top of the screen. */
  statusBar: number;
  /** Safe area at the bottom of the screen (home indicator, gesture bar). */
  homeInset: number;
  /** Width of the notch or island, the gap the status bar leaves in the middle. */
  cutout: number;
  browsers: readonly TPreviewBrowserId[];
}

/*
 * Screens and safe areas follow Apple's published iPhone specs (iPhone 14: 390×844, insets 47/34; 15 Pro: 393×852,
 * 59/34; 16 Pro: 402×874, 62/34) and Samsung Galaxy S24 (1080×2340 at DPR 3 → 360×780). The status bar of the Galaxy
 * is about 32dp with the punch hole.
 */
export const PREVIEW_DEVICES = [
  {
    id: 'fluid',
    label: t('lib.hlPreviewDevices.fluid'),
    platform: DEVICE_PLATFORM.desktop,
    screen: null,
    frame: DEVICE_FRAME.none,
    bezel: 0,
    radius: 0,
    statusBar: 0,
    homeInset: 0,
    cutout: 0,
    browsers: [PREVIEW_BROWSER.none],
  },
  {
    id: 'plain-phone',
    label: t('lib.hlPreviewDevices.plainPhone'),
    platform: DEVICE_PLATFORM.desktop,
    screen: { width: 393, height: 852 },
    frame: DEVICE_FRAME.none,
    bezel: 0,
    radius: 12,
    statusBar: 0,
    homeInset: 0,
    cutout: 0,
    browsers: [PREVIEW_BROWSER.none],
  },
  {
    id: 'iphone-14',
    label: t('lib.hlPreviewDevices.iphone14'),
    platform: DEVICE_PLATFORM.ios,
    screen: { width: 390, height: 844 },
    frame: DEVICE_FRAME.notch,
    bezel: 11,
    radius: 47,
    statusBar: 47,
    homeInset: 34,
    cutout: 162,
    browsers: [PREVIEW_BROWSER.safari, PREVIEW_BROWSER.none],
  },
  {
    id: 'iphone-15-pro',
    label: t('lib.hlPreviewDevices.iphone15Pro'),
    platform: DEVICE_PLATFORM.ios,
    screen: { width: 393, height: 852 },
    frame: DEVICE_FRAME.island,
    bezel: 9,
    radius: 55,
    statusBar: 59,
    homeInset: 34,
    cutout: 124,
    browsers: [PREVIEW_BROWSER.safari, PREVIEW_BROWSER.none],
  },
  {
    id: 'iphone-16-pro',
    label: t('lib.hlPreviewDevices.iphone16Pro'),
    platform: DEVICE_PLATFORM.ios,
    screen: { width: 402, height: 874 },
    frame: DEVICE_FRAME.island,
    bezel: 8,
    radius: 62,
    statusBar: 62,
    homeInset: 34,
    cutout: 125,
    browsers: [PREVIEW_BROWSER.safari, PREVIEW_BROWSER.none],
  },
  {
    id: 'galaxy-s24',
    label: t('lib.hlPreviewDevices.galaxyS24'),
    platform: DEVICE_PLATFORM.android,
    screen: { width: 360, height: 780 },
    frame: DEVICE_FRAME.punch,
    bezel: 8,
    radius: 36,
    statusBar: 32,
    homeInset: 24,
    cutout: 12,
    browsers: [PREVIEW_BROWSER.chrome, PREVIEW_BROWSER.none],
  },
  {
    id: 'ipad',
    label: 'iPad',
    platform: DEVICE_PLATFORM.desktop,
    screen: { width: 820, height: 1180 },
    frame: DEVICE_FRAME.none,
    bezel: 0,
    radius: 12,
    statusBar: 0,
    homeInset: 0,
    cutout: 0,
    browsers: [PREVIEW_BROWSER.none],
  },
] as const satisfies readonly IDeviceSpec[];

export type TPreviewDevice = (typeof PREVIEW_DEVICES)[number];
export type TPreviewDeviceId = TPreviewDevice['id'];

export const deviceById = (id: TPreviewDeviceId): TPreviewDevice =>
  PREVIEW_DEVICES.find((device) => device.id === id) ?? PREVIEW_DEVICES[0];

/** Safari with the address bar at the bottom: a 56 address pill over a 44 toolbar, above the home indicator area. */
export const SAFARI_BOTTOM_BAR = 100;
/** Chrome Android: the toolbar under the status bar. */
export const CHROME_TOP_BAR = 56;

/** The browser a device shows: the person's choice when the device supports it, the device's own browser otherwise. */
export const browserFor = (device: IDeviceSpec, chosen?: TPreviewBrowserId): TPreviewBrowserId =>
  chosen && device.browsers.includes(chosen) ? chosen : device.browsers[0];

export interface IDeviceLayout {
  /** Outer size of the body with the bezel. */
  body: ISize;
  screen: ISize;
  viewport: ISize;
  /** Where the page frame starts inside the screen; the viewport spans the full width. */
  viewportTop: number;
  /** Chrome above and below the viewport, within the screen. */
  topBar: number;
  bottomBar: number;
}

/**
 * The page's visible viewport on a device. Without browser chrome the page runs the whole screen and the status bar and
 * the home indicator draw over it. Safari leaves the status bar and its bottom bar; Chrome leaves the status bar, its
 * toolbar and the gesture bar.
 */
export const deviceLayout = (device: IDeviceSpec, browser: TPreviewBrowserId): IDeviceLayout | null => {
  const { screen } = device;
  if (!screen) return null;
  let topBar = 0;
  let bottomBar = 0;
  if (browser === PREVIEW_BROWSER.safari) {
    topBar = device.statusBar;
    bottomBar = SAFARI_BOTTOM_BAR + device.homeInset;
  } else if (browser === PREVIEW_BROWSER.chrome) {
    topBar = device.statusBar + CHROME_TOP_BAR;
    bottomBar = device.homeInset;
  }
  return {
    body: { width: screen.width + device.bezel * 2, height: screen.height + device.bezel * 2 },
    screen,
    viewport: { width: screen.width, height: screen.height - topBar - bottomBar },
    viewportTop: topBar,
    topBar,
    bottomBar,
  };
};

/** Where the page frame's top-left corner sits inside the device body: the origin for picks reported in page pixels. */
export const viewportOrigin = (device: IDeviceSpec, layout: IDeviceLayout): IPoint => ({
  x: device.bezel,
  y: device.bezel + layout.viewportTop,
});

/** Scale and on-screen size of a device in the stage. An unmeasured stage (0×0) gets a tiny positive scale, not 0. */
export const fitDevice = (device: ISize, stage: ISize): ISize & { scale: number } => {
  const scale = Math.max(0.05, Math.min(1, stage.width / device.width, stage.height / device.height));
  return { scale, width: Math.floor(device.width * scale), height: Math.floor(device.height * scale) };
};
