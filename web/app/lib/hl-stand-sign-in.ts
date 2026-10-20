/**
 * A feature stand is behind the team's sign-in (oauth2-proxy and Keycloak). The slot never follows that chain:
 * the session must land in the person's own browser, on the stands' cookie domain, where every slot request picks it up.
 * So the sign-in is a real top-level visit to the stand's own start address, and the preview opens again once the
 * person is back.
 */

export const SIGN_IN_WHERE = {
  /** A new tab of the browser: the app stays where it is and reloads the stand when the person comes back. */
  tab: 'tab',
  /** This window: an installed app keeps its own cookies, which a tab of the browser would not fill. */
  here: 'here',
} as const;
export type TSignInWhere = (typeof SIGN_IN_WHERE)[keyof typeof SIGN_IN_WHERE];

/** What the sign-in needs from the page's window; a test passes its own. */
export interface ISignInWindow {
  matchMedia: (query: string) => { matches: boolean };
  navigator: object;
  open: (url: string, target: string, features: string) => unknown;
  location: { assign: (url: string) => void };
}

const STANDALONE = '(display-mode: standalone)';

/** `navigator.standalone` is Safari's mark of an app added to the home screen; the display mode covers the others. */
export const isInstalledApp = (win: Pick<ISignInWindow, 'matchMedia' | 'navigator'>): boolean =>
  (win.navigator as { standalone?: boolean }).standalone === true || win.matchMedia(STANDALONE).matches;

export const signInWhereOf = (win: Pick<ISignInWindow, 'matchMedia' | 'navigator'>): TSignInWhere =>
  isInstalledApp(win) ? SIGN_IN_WHERE.here : SIGN_IN_WHERE.tab;

/**
 * Starts the sign-in at `url` (see `standSignInUrl`). `noopener`: the pages of the sign-in, and the stand it ends on, get
 * no handle on the app. Returns where it went, so the caller knows whether to wait for the person to come back.
 */
export const beginStandSignIn = (url: string, win: ISignInWindow): TSignInWhere => {
  const where = signInWhereOf(win);
  if (where === SIGN_IN_WHERE.here) win.location.assign(url);
  else win.open(url, '_blank', 'noopener');
  return where;
};
