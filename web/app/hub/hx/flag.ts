// Feature switches of the hub's organizations (`org_features`).

/** The lively hub: the common motion on every screen, free periods, sortable tables, the tools screen. */
export const HX_LIVE_FEATURE = 'ui.hx.live';

export const liveOn = (features: readonly string[] | undefined): boolean =>
  (features ?? []).includes(HX_LIVE_FEATURE);

/** One server with the people's seats instead of a machine per Linux user. */
export const SEATS_FEATURE = 'machine.seats';

export const seatsOn = (features: readonly string[] | undefined): boolean =>
  (features ?? []).includes(SEATS_FEATURE);

/** The fleet: one runtime for every machine a person may open (the hub's /agents/, internal/hub/proxy/workspace.go). */
export const FLEET_FEATURE = 'runtime.fleet';

/** Whether «Рабочее пространство» is the hub's own /agents/: the hub serves it and the organization has the fleet. */
export const fleetWorkspace = (
  served: boolean | undefined,
  features: readonly string[] | undefined,
): boolean => served === true && (features ?? []).includes(FLEET_FEATURE);
