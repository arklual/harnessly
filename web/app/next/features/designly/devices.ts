// What the open design is looked at on: the window itself («По окну»), an exact size, or a device in its case. The
// viewport is the page's CSS size; the box is what is drawn and `fit` scales it into the area so all of it shows. A phone
// or a tablet in a case also draws its own interface (a status bar, the browser's bars) which takes room from the page.

export const PHONE_OS = { ios: 'ios', android: 'android', ipad: 'ipad' } as const;
export type TPhoneOs = (typeof PHONE_OS)[keyof typeof PHONE_OS];

/** The groups of the device menu; the last three are also the chips of «Все экраны». */
export const DEVICE_GROUP = {
  view: 'view',
  phones: 'phones',
  tablets: 'tablets',
  computers: 'computers',
} as const;
export type TDeviceGroup = (typeof DEVICE_GROUP)[keyof typeof DEVICE_GROUP];

export interface IDevice {
  id: string;
  icon: string;
  group: TDeviceGroup;
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

// The menu's order: the view, then the phones (the bare one last), the tablet and the computers.
export const DEVICES: readonly IDevice[] = [
  { id: 'fill', icon: 'monitor', group: DEVICE_GROUP.view },
  { id: 'overview', icon: 'layout-grid', group: DEVICE_GROUP.view },
  {
    id: 'ip14',
    icon: 'smartphone',
    group: DEVICE_GROUP.phones,
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
    group: DEVICE_GROUP.phones,
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
    group: DEVICE_GROUP.phones,
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
    group: DEVICE_GROUP.phones,
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
    group: DEVICE_GROUP.phones,
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
  { id: 'bare', icon: 'smartphone', group: DEVICE_GROUP.phones, vw: 393, vh: 852 },
  {
    id: 'ipad',
    icon: 'tablet',
    group: DEVICE_GROUP.tablets,
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
  { id: 'lap', icon: 'laptop', group: DEVICE_GROUP.computers, vw: 1280, vh: 800 },
  { id: 'fhd', icon: 'monitor', group: DEVICE_GROUP.computers, vw: 1920, vh: 1080 },
];

/** A phone or a tablet turns (lying: its width and height swapped); «По окну» and the computers do not. */
export const canRotate = (device: IDevice): boolean =>
  device.group === DEVICE_GROUP.phones || device.group === DEVICE_GROUP.tablets;

export const deviceOf = (id: string): IDevice =>
  DEVICES.find((d) => d.id === id) ?? DEVICES.find((d) => d.id === 'fill') ?? DEVICES[0];

/** The menu's groups in their order, each with its devices in the order of DEVICES. */
export const deviceGroups = (): { group: TDeviceGroup; devices: IDevice[] }[] =>
  Object.values(DEVICE_GROUP).map((group) => ({ group, devices: DEVICES.filter((d) => d.group === group) }));

/** The size a menu row shows on its right («393×852»); null for the view's rows, which have none of their own. */
export const deviceSizeText = (device: IDevice): string | null =>
  device.vw && device.vh ? `${device.vw}×${device.vh}` : null;

export interface ISize {
  w: number;
  h: number;
}

/** The sizes «По окну» takes: whatever was typed, dragged, turned or kept stays within them. */
export const VIEWPORT_SIZE = { min: 200, max: 3840 } as const;
/** The least scale a box is drawn at: an area measured empty (a hidden or squeezed pane) must not take it to 0%. */
export const MIN_FIT = 0.05;

/** The size within the limits, or null when a side is below the least (a box measured in a squeezed area). */
export const keptSize = (size: ISize | null): ISize | null =>
  size === null || !(size.w >= VIEWPORT_SIZE.min && size.h >= VIEWPORT_SIZE.min)
    ? null
    : {
        w: Math.min(VIEWPORT_SIZE.max, Math.round(size.w)),
        h: Math.min(VIEWPORT_SIZE.max, Math.round(size.h)),
      };

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
/** «По окну» is the design set into the window edge to edge, with no canvas round it; the area's padding in
 *  site.css is the same. */
export const FILL_INSET = 0;

/** The viewport for a device in an area; `size` is the person's own size of «По окну», `rot` turns a device. */
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
  const fitOf = (w: number, h: number) =>
    Math.max(MIN_FIT, Math.min(1, (area.w - PAD) / w, (area.h - padBottom) / h));
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

/** The devices «Все экраны» shows, by group (the bare phone is the size of iPhone 15 Pro, so it is left out). */
export const OVERVIEW_IDS = ['ip14', 'ip15', 'ip16', 'px7', 's24', 'ipad', 'lap', 'fhd'] as const;
/** The groups «Все экраны» can show: its chips. */
export const OVERVIEW_GROUPS = [DEVICE_GROUP.phones, DEVICE_GROUP.tablets, DEVICE_GROUP.computers] as const;
export type TOverviewGroup = (typeof OVERVIEW_GROUPS)[number];
/** The gap between two screens of a group, and the wider one between two groups (site.css .hx-ovw). */
const OVERVIEW_GAP = 28;
const OVERVIEW_GROUP_GAP = 64;
/** The row's padding above the screens (the chips stand there) and at the sides, a caption with its gap, the room
 *  kept under the screens for the field. */
const OVERVIEW_TOP = 60;
const OVERVIEW_SIDE = 24;
const OVERVIEW_CAPTION = 32;
const OVERVIEW_BOTTOM = 20;
const OVERVIEW_FIELD = 96;
/** The least scale the row starts at however many screens it has: smaller, a page is unreadable, and the canvas pans. */
const OVERVIEW_LEAST = 0.35;

export interface IOverviewCard {
  device: IDevice;
  w: number;
  h: number;
  /** Shared by all the cards: the devices keep their true proportions. */
  scale: number;
  /** The first card of a group after another: the wider gap is before it. */
  lead: boolean;
}

/**
 * The cards of «Все экраны» for an area and the groups shown, at one scale. The whole row fits when it can; with many
 * screens it stays at a readable scale, as tall as the area lets it, and runs past the area's right edge to be panned to.
 */
export const overviewOf = (
  area: ISize,
  underField: boolean,
  groups: readonly TOverviewGroup[] = OVERVIEW_GROUPS,
): IOverviewCard[] => {
  const devices = OVERVIEW_IDS.map(deviceOf).filter((d) => groups.some((g) => g === d.group));
  if (devices.length === 0) return [];
  const leads = devices.filter((d, i) => i > 0 && devices[i - 1].group !== d.group).length;
  const room = area.h - OVERVIEW_TOP - OVERVIEW_CAPTION - (underField ? OVERVIEW_FIELD : OVERVIEW_BOTTOM);
  const across =
    area.w - 2 * OVERVIEW_SIDE - OVERVIEW_GAP * (devices.length - 1 - leads) - OVERVIEW_GROUP_GAP * leads;
  const sum = devices.reduce((total, d) => total + (d.vw ?? 0), 0);
  const tallest = Math.max(...devices.map((d) => d.vh ?? 0));
  const scale = Math.max(MIN_FIT, Math.min(1, room / tallest, Math.max(across / sum, OVERVIEW_LEAST)));
  return devices.map((device, i) => ({
    device,
    w: device.vw ?? 0,
    h: device.vh ?? 0,
    scale,
    lead: i > 0 && devices[i - 1].group !== device.group,
  }));
};

const gapBefore = (card: IOverviewCard): number => (card.lead ? OVERVIEW_GROUP_GAP : OVERVIEW_GAP);

/** The row of «Все экраны» as drawn: the padding, a caption over each screen, the gaps, and room for the field. */
export const overviewBox = (cards: readonly IOverviewCard[], underField: boolean): ISize => {
  if (cards.length === 0) return { w: 0, h: 0 };
  const across = cards.reduce((total, c, i) => total + c.w * c.scale + (i === 0 ? 0 : gapBefore(c)), 0);
  const tallest = Math.max(...cards.map((c) => c.h * c.scale));
  return {
    w: 2 * OVERVIEW_SIDE + across,
    h: OVERVIEW_TOP + OVERVIEW_CAPTION + tallest + (underField ? OVERVIEW_FIELD : OVERVIEW_BOTTOM),
  };
};

/** Where the other screens go when one is scrolled to `y`: the same CSS pixels from the top (each page stops at its own
 *  end), never above it, in whole pixels so a page told the same place twice does not move. */
export const syncedScroll = (y: number): number => Math.max(0, Math.round(y));

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
