// What the open design is looked at on: the window itself («Резина»), an exact size, or a device in its case. The
// viewport is the page's CSS size; the box is what is drawn and `fit` scales it into the area so all of it shows. A phone
// or a tablet in a case also draws its own interface (a status bar, the browser's bars) which takes room from the page.

export const PHONE_OS = { ios: 'ios', android: 'android', ipad: 'ipad' } as const;
export type TPhoneOs = (typeof PHONE_OS)[keyof typeof PHONE_OS];

export interface IDevice {
  id: string;
  icon: string;
  /** The case of Devices.css and its colour; none for a bare size. */
  cls?: string;
  color?: string;
  vw?: number;
  vh?: number;
  /** The case and its screen in pixels of the case. */
  fw?: number;
  fh?: number;
  sw?: number;
  sh?: number;
  /** The family of the interface drawn in the case, the system it belongs to and the status bar's height in CSS pixels. */
  ui?: 'safari' | 'chrome' | 'ipad';
  os?: TPhoneOs;
  sb?: number;
  /** The browser the device starts with; without it the first of its system's. */
  browser?: string;
}

export const DEVICES: readonly IDevice[] = [
  { id: 'overview', icon: 'layout-grid' },
  { id: 'fill', icon: 'monitor' },
  { id: 'bare', icon: 'smartphone', vw: 393, vh: 852 },
  {
    id: 'ip14',
    icon: 'smartphone',
    vw: 390,
    vh: 844,
    cls: 'device-iphone-14',
    color: '',
    ui: 'safari',
    os: 'ios',
    sb: 47,
    fw: 428,
    fh: 868,
    sw: 390,
    sh: 830,
  },
  {
    id: 'ip15',
    icon: 'smartphone',
    vw: 393,
    vh: 852,
    cls: 'device-iphone-14-pro',
    color: 'device-black',
    ui: 'safari',
    os: 'ios',
    sb: 54,
    fw: 428,
    fh: 868,
    sw: 390,
    sh: 830,
  },
  {
    id: 'ip16',
    icon: 'smartphone',
    vw: 402,
    vh: 874,
    cls: 'device-iphone-14-pro',
    color: 'device-silver',
    ui: 'safari',
    os: 'ios',
    sb: 54,
    fw: 428,
    fh: 868,
    sw: 390,
    sh: 830,
  },
  {
    id: 'px7',
    icon: 'smartphone',
    vw: 412,
    vh: 915,
    cls: 'device-google-pixel-6-pro',
    color: 'device-black',
    ui: 'chrome',
    os: 'android',
    browser: 'chrome',
    sb: 36,
    fw: 404,
    fh: 862,
    sw: 376,
    sh: 816,
  },
  {
    id: 's24',
    icon: 'smartphone',
    vw: 360,
    vh: 780,
    cls: 'device-google-pixel-6-pro',
    color: 'device-black',
    ui: 'chrome',
    os: 'android',
    browser: 'sam85',
    sb: 36,
    fw: 404,
    fh: 862,
    sw: 376,
    sh: 816,
  },
  {
    id: 'ipad',
    icon: 'tablet',
    vw: 834,
    vh: 1194,
    cls: 'device-ipad-pro',
    color: 'device-spacegray',
    ui: 'ipad',
    os: 'ipad',
    sb: 24,
    fw: 560,
    fh: 778,
    sw: 506,
    sh: 724,
  },
  { id: 'lap', icon: 'laptop', vw: 1280, vh: 800 },
  { id: 'fhd', icon: 'monitor', vw: 1920, vh: 1080 },
];

export const deviceOf = (id: string): IDevice =>
  DEVICES.find((d) => d.id === id) ?? DEVICES.find((d) => d.id === 'fill') ?? DEVICES[0];

export interface ISize {
  w: number;
  h: number;
}

export const VP_KIND = { fill: 'fill', size: 'size', frame: 'frame', overview: 'overview' } as const;
export type TVpKind = (typeof VP_KIND)[keyof typeof VP_KIND];

export interface IViewport {
  kind: TVpKind;
  rot: boolean;
  /** The page's CSS viewport. */
  vw: number;
  vh: number;
  /** The CSS height of the screen in a case (the page is scaled by k into it). */
  cvh: number;
  /** The drawn box before fit. */
  bw: number;
  bh: number;
  fit: number;
  k: number;
}

// The field (island, notes) floats over the page: a device keeps the whole area, its bottom as narrow as the sides.
const PAD = 48;
/** «Адаптивно» leaves this much canvas round the page (the area's padding in site.css): there is always some to grab. */
export const FILL_INSET = 12;

/** The viewport for a device in an area; `size` is the person's own size of «Резина», `rot` turns a device. */
export const viewportOf = (
  device: IDevice,
  area: ISize,
  size: ISize | null,
  rot: boolean,
  padBottom = PAD,
): IViewport => {
  if (device.id === 'overview') {
    return {
      kind: VP_KIND.overview,
      rot: false,
      vw: area.w,
      vh: area.h,
      cvh: area.h,
      bw: area.w,
      bh: area.h,
      fit: 1,
      k: 1,
    };
  }
  if (device.id === 'fill' && !size) {
    const w = Math.max(0, area.w - 2 * FILL_INSET);
    const h = Math.max(0, area.h - 2 * FILL_INSET);
    return { kind: VP_KIND.fill, rot: false, vw: w, vh: h, cvh: h, bw: w, bh: h, fit: 1, k: 1 };
  }
  const custom = device.id === 'fill';
  const turned = !custom && rot;
  const base: ISize = custom && size ? size : { w: device.vw ?? area.w, h: device.vh ?? area.h };
  const vw = turned ? base.h : base.w;
  const vh = turned ? base.w : base.h;
  const fitOf = (w: number, h: number) => Math.min(1, (area.w - PAD) / w, (area.h - padBottom) / h);
  if (!device.cls || !device.fw || !device.fh || !device.sw || !device.sh) {
    return { kind: VP_KIND.size, rot: false, vw, vh, cvh: vh, bw: vw, bh: vh, fit: fitOf(vw, vh), k: 1 };
  }
  // In a case the screen is fixed: the page of width vw is scaled into it, its height is what fits.
  const k = (turned ? device.sh : device.sw) / vw;
  const cvh = Math.round((turned ? device.sw : device.sh) / k);
  const bw = turned ? device.fh : device.fw;
  const bh = turned ? device.fw : device.fh;
  return { kind: VP_KIND.frame, rot: turned, vw, vh, cvh, bw, bh, fit: fitOf(bw, bh), k };
};

/** The custom properties the viewport's CSS reads (--bw, --vw, --k, --fit and the case's sizes). */
export const viewportVars = (vp: IViewport, device: IDevice): Record<string, string> => {
  const vars: Record<string, string> = {
    '--bw': `${vp.bw}px`,
    '--bh': `${vp.bh}px`,
    '--vw': `${vp.vw}px`,
    '--vh': `${vp.cvh}px`,
    '--k': vp.k.toFixed(4),
    '--fit': vp.fit.toFixed(4),
  };
  if (vp.kind === VP_KIND.frame && device.fw && device.fh && device.sw && device.sh) {
    vars['--dw'] = `${device.fw}px`;
    vars['--dh'] = `${device.fh}px`;
    vars['--sw'] = `${device.sw}px`;
    vars['--sh'] = `${device.sh}px`;
    if (device.sb !== undefined) vars['--sb'] = `${device.sb}px`;
  }
  return vars;
};

/** The devices «Обзор» shows side by side, and the least scale they are drawn at. */
export const OVERVIEW_IDS = ['ip15', 'px7', 'ipad', 'lap'] as const;
const OVERVIEW_GAP = 28;
const OVERVIEW_MIN_SCALE = 0.2;

export interface IOverviewCard {
  device: IDevice;
  w: number;
  h: number;
  /** Shared by all the cards: the devices keep their true proportions. */
  scale: number;
}

/** The cards of «Обзор» for an area: one scale makes them all fit its width and height (under the field it keeps room). */
export const overviewOf = (area: ISize, underField: boolean): IOverviewCard[] => {
  const devices = OVERVIEW_IDS.map(deviceOf);
  // A card's label takes 28 px above its screen and the area's padding 40.
  const room = Math.max(200, area.h - 40 - 28 - (underField ? 80 : 0));
  const across = area.w - 48 - OVERVIEW_GAP * (devices.length - 1);
  const sum = devices.reduce((total, d) => total + (d.vw ?? 0), 0);
  const tallest = Math.max(...devices.map((d) => d.vh ?? 0));
  const scale = Math.max(OVERVIEW_MIN_SCALE, Math.min(1, across / sum, room / tallest));
  return devices.map((device) => ({ device, w: device.vw ?? 0, h: device.vh ?? 0, scale }));
};

/**
 * How a phone's browser spends the screen: heights in CSS pixels as [unfolded, folded]. `top` and `bot` bars push the
 * page (it grows when they fold), `over` hangs over the page under glass, `gest` is the gesture strip of Android.
 */
export interface IBrowser {
  id: string;
  top?: readonly [number, number];
  bot?: readonly [number, number];
  over?: readonly [number, number];
  gest?: number;
}

export const BROWSERS: Record<TPhoneOs, readonly IBrowser[]> = {
  ios: [
    { id: 'ios26', over: [76, 40] },
    { id: 'ios18', over: [92, 44] },
    { id: 'iostop', top: [50, 26], bot: [80, 0] },
    { id: 'pwa' },
  ],
  android: [
    { id: 'chrome', top: [56, 0], gest: 24 },
    { id: 'sam85', over: [72, 0], gest: 24 },
    { id: 'samsung', top: [56, 0], bot: [52, 0], gest: 24 },
    { id: 'pwa', gest: 24 },
  ],
  ipad: [{ id: 'ipad', top: [50, 50] }],
};

/** The browser a device shows: the one asked for if it is the device's system's, else the device's own. */
export const browserOf = (device: IDevice, wanted: string | null): IBrowser | null => {
  const list = device.os ? BROWSERS[device.os] : [];
  return list.find((b) => b.id === wanted) ?? list.find((b) => b.id === device.browser) ?? list[0] ?? null;
};

export const BARS = { full: 'full', min: 'min' } as const;
export type TBars = (typeof BARS)[keyof typeof BARS];

/** Where the page was scrolled to, how far it has gone in one direction, and so what the bars do. */
export interface IScrollTrack {
  y: number;
  acc: number;
  bars: TBars;
}

export const SCROLL_START: IScrollTrack = { y: 0, acc: 0, bars: BARS.full };
/** A run of this many pixels one way folds or unfolds the bars: a trembling finger does not move them. */
const SCROLL_STEP = 12;

export const trackScroll = (prev: IScrollTrack, y: number): IScrollTrack => {
  const moved = y - prev.y;
  const acc = Math.sign(moved) === Math.sign(prev.acc) ? prev.acc + moved : moved;
  let bars = prev.bars;
  if (y < SCROLL_STEP || acc < -SCROLL_STEP) bars = BARS.full;
  else if (acc > SCROLL_STEP) bars = BARS.min;
  return { y, acc, bars };
};
