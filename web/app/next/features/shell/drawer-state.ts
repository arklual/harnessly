// The phone drawer's state as the page lives it: the drawer is a phone's layer, so on a wide window an
// «open» flag left over from a narrower one dims nothing and shuts nothing: it must not make the window inert (the
// scrim and the sidebar it stands for are not there to close it).
export interface IDrawerState {
  /** What `data-drawer` says. */
  drawer: 'open' | 'closed';
  /** The window under the drawer takes no clicks, only while a drawer is really drawn over it. */
  inert: boolean;
  /** The desktop rail's folded look belongs to a wide window: the phone's drawer is always the full sidebar. */
  rail: boolean;
}

export const drawerState = (open: boolean, phone: boolean, rail: boolean): IDrawerState => ({
  drawer: open && phone ? 'open' : 'closed',
  inert: open && phone,
  rail: rail && !phone,
});
