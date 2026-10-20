/**
 * Links to the design library: a project or a board of the shared library has one address in the app, the
 * same for everyone who may open this page, and this is where it is made, read and where the choice between a new
 * window and the current one is made. Nothing here imports node modules.
 */
import { APP_BASE_PATH } from '@/lib/app-path';
import {
  CENTRE_VIEW,
  parseAppLocation,
  searchForWanted,
  type IDesignLocation,
} from '@/lib/hl-conversation-url';

/** The parts of the address that make it a link of this app and nothing else: the machine and debug flags of the sender stay out. */
export const designLinkPath = (location: IDesignLocation): string =>
  `${APP_BASE_PATH}/${searchForWanted('', { key: null, view: CENTRE_VIEW.design, provisional: false, design: location })}`;

/** The link to send: the origin of the page the person is on, the base path, the place. */
export const designLinkUrl = (origin: string, location: IDesignLocation): string =>
  new URL(designLinkPath(location), origin).href;

/** The place a link or a push `url` names, or null for anything that is not a page of the design library in this app. */
export const designLocationOfUrl = (url: string | undefined): IDesignLocation | null => {
  if (!url) return null;
  let parsed: URL;
  try {
    parsed = new URL(url, 'https://hl.invalid');
  } catch {
    return null;
  }
  if (!parsed.pathname.startsWith(`${APP_BASE_PATH}/`)) return null;
  const { view, design } = parseAppLocation(parsed.search);
  return view === CENTRE_VIEW.design && design?.project ? design : null;
};

/** What the page runs on, as far as opening and sharing care. */
export interface IDeviceEnv {
  /** The installed app: no browser tabs, a popup goes nowhere. */
  standalone: boolean;
  /** The main pointer is a finger. */
  touch: boolean;
  narrow: boolean;
  canShare: boolean;
}

/** Below it the page is laid out for a phone. */
export const NARROW_PX = 768;

export const OPEN_MODE = { window: 'window', here: 'here' } as const;
export type TOpenMode = (typeof OPEN_MODE)[keyof typeof OPEN_MODE];

export const SHARE_MODE = { sheet: 'sheet', copy: 'copy' } as const;
export type TShareMode = (typeof SHARE_MODE)[keyof typeof SHARE_MODE];

/**
 * A phone or an installed app on a touch screen has no second window to give: popups are blocked, and a window of
 * another site is handed to another browser. There the board takes over the current window; a computer opens a tab.
 */
export const chooseOpenMode = ({ standalone, touch, narrow }: IDeviceEnv): TOpenMode =>
  touch && (standalone || narrow) ? OPEN_MODE.here : OPEN_MODE.window;

/** The system share sheet is for a touch device; a computer's «share» would be a lesser menu than the clipboard. */
export const chooseShareMode = ({ touch, canShare }: IDeviceEnv): TShareMode =>
  touch && canShare ? SHARE_MODE.sheet : SHARE_MODE.copy;

const matches = (query: string): boolean => window.matchMedia(query).matches;

export const readDeviceEnv = (): IDeviceEnv => ({
  standalone:
    matches('(display-mode: standalone)') || (navigator as { standalone?: boolean }).standalone === true,
  touch: matches('(pointer: coarse)'),
  narrow: window.innerWidth < NARROW_PX,
  canShare: typeof navigator.share === 'function',
});
