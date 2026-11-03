import type { Money } from './common';

// The resources, load and operations of a machine, as docs/hub/machines-resources.md writes them. They exist only for an
// organization with the `machine.resources` feature on (404 `not_enabled` otherwise). Times are RFC 3339 UTC; a price
// the provider did not give, and every number nobody measured, is null: the screens show a dash.

// An amount of the provider's own currency, in its minor units (kopecks, cents).
export type MachinePrice = { amount: number; currency: string; period: string };

export type MachineSize = { cpu: number | null; ramGB: number | null; diskGB: number | null };

export type MachinePlan = {
  id: string;
  name: string;
  cpu: number;
  ramGB: number;
  diskGB: number;
  price: MachinePrice | null;
  current: boolean;
};

// What the machine said of itself in `hello`.
export type MachineHardware = {
  cores: number | null;
  ramBytes: number | null;
  diskBytes: number | null;
  model: string | null;
  laptop: boolean;
  os: string | null;
  arch: string | null;
  updatedAt: string;
};

// What a machine runs on, as its provider says (a row of the machines list).
export type MachineCloud = {
  provider: string;
  plan: string | null;
  cpu: number | null;
  ramGB: number | null;
  diskGB: number | null;
  price: MachinePrice | null;
  externalId: string | null;
};

// The last reading of the machine's load, in percent; null for a number it could not read.
export type MachineLoad = {
  cpuPct: number | null;
  ramPct: number | null;
  diskPct: number | null;
  at: string;
};

// The machine's calls of the last seven days. `spend` is the list cost of the priced calls (null when none was priced).
export type MachineWeek = {
  chats: number;
  calls: number;
  unpriced: number;
  spend: Money | null;
};

// Which parts of the machine the hub controls.
export type MachineManaged = { budget: boolean; env: boolean; resources: boolean; access: boolean };
export type ManagedCapability = keyof MachineManaged;

export type MachineOpKind =
  'resize' | 'reboot' | 'power_off' | 'power_on' | 'restart_runner' | 'restart_machine' | 'drain';
export type MachineOpStatus = 'pending' | 'running' | 'done' | 'failed' | 'cancelled';

export type MachineOp = {
  id: string;
  machineId: string;
  kind: MachineOpKind;
  status: MachineOpStatus;
  params: unknown;
  error: string | null;
  createdBy: string | null;
  createdAt: string;
  updatedAt: string;
  finishedAt: string | null;
  chatsNow: number | null; // chats a restart that waits for them still waits for
};

// GET /api/machines/{id}/resources
export type ResourcesResponse = {
  machineId: string;
  managed: boolean; // the hub controls the machine's resources
  provider: { name: string; supported: boolean };
  providerError: string | null; // the provider did not answer; the numbers are then what the hub had
  current: MachineSize | null;
  price: MachinePrice | null;
  plans: MachinePlan[];
  hardware: MachineHardware | null;
  consoleUrl: string | null;
  activeOps: MachineOp[];
  updatedAt: string | null;
};

// POST /api/machines/{id}/resources/quote and POST …/resize
export type SizeRequest = { cpu: number; ramGB: number; diskGB: number };
export type QuoteResponse = {
  machineId: string;
  want: MachineSize;
  price: MachinePrice | null;
  current: MachinePrice | null;
  delta: MachinePrice | null; // only when both are in one currency
};
export type ResizeResponse = { op: MachineOp };

// GET /api/machines/{id}/ops?active=1
export type OpsResponse = { machineId: string; ops: MachineOp[] };
export type OpResponse = { op: MachineOp };

// PUT /api/machines/{id}/managed
export type ManagedRequest = { capability: ManagedCapability; managed: boolean };
export type ManagedResponse = { machineId: string; managed: MachineManaged };

// GET /api/machines/{id}/metrics?range=
export type MetricsRange = '1h' | '6h' | '24h' | '7d';
export type MetricPoint = {
  at: string;
  cpuAvg: number;
  cpuMax: number;
  ramAvg: number;
  ramMax: number;
  diskAvg: number;
  diskMax: number;
  chatsMax: number;
  samples: number;
};
export type MetricsResponse = {
  machineId: string;
  range: MetricsRange;
  stepSeconds: number;
  from: string;
  to: string;
  points: MetricPoint[];
};

// The live event `machine.op` of the organization's stream.
export type MachineOpEvent = { machineId: string; op: MachineOp };

// Errors specific to this area (the `error` code of the answer).
export type MachineResourceError =
  | 'not_enabled'
  | 'not_supported'
  | 'not_managed'
  | 'current_unknown'
  | 'no_change'
  | 'disk_shrink'
  | 'op_busy'
  | 'not_cancellable'
  | 'op_finished'
  | 'drain_unsupported'
  | 'machine_disabled'
  | 'machine_access'
  | 'provider_not_found'
  | 'provider_timeout'
  | 'provider_error';
