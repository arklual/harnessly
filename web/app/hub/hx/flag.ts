// Whether an organization gets the new hub: the `ui.hx` feature of `org_features`, switched per organization (the dev
// team first). `?hx=0` always goes back to the current hub for this browser; `?hx=1` forces the new one only on a dev
// server, so an address can never switch it on for someone whose organization does not have it.
const KEY = 'hl-hub-hx';

const remember = (v: string | null) => {
  try {
    if (v === null) localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, v);
  } catch {
    // not remembered: the flag decides
  }
};

const stored = (): string | null => {
  try {
    return localStorage.getItem(KEY);
  } catch {
    return null;
  }
};

export const HX_FEATURE = 'ui.hx';

export const hxOn = (features: readonly string[] | undefined, search = location.search): boolean => {
  const asked = new URLSearchParams(search).get('hx');
  if (asked === '0' || asked === '1') remember(asked);
  else if (asked === '') remember(null);
  const pref = stored();
  if (pref === '0') return false;
  if (pref === '1' && import.meta.env.DEV) return true;
  return (features ?? []).includes(HX_FEATURE);
};

/** The lively hub: the common motion on every screen, free periods, sortable tables, the tools screen. */
export const HX_LIVE_FEATURE = 'ui.hx.live';

export const liveOn = (features: readonly string[] | undefined): boolean =>
  (features ?? []).includes(HX_LIVE_FEATURE);

/** The fleet: one runtime for every machine a person may open (the hub's /agents/, internal/hub/proxy/workspace.go). */
export const FLEET_FEATURE = 'runtime.fleet';

/** Whether «Рабочее пространство» is the hub's own /agents/: the hub serves it and the organization has the fleet. */
export const fleetWorkspace = (
  served: boolean | undefined,
  features: readonly string[] | undefined,
): boolean => served === true && (features ?? []).includes(FLEET_FEATURE);
