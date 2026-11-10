// What the machines endpoints of the hub carry beyond the old `HubMachine` (machine resources, HRN-633): the hardware,
// the load now, the week's spend, the provider's server, the managed capabilities and the operations in flight. Every
// field is optional here: a hub that does not send one leaves the screen with «—», never with a made-up number.
import { type Money, type MachineBusyKind, type MachineManaged, type OrgMachine } from '../machines';

export interface WireMoney {
  amount: number;
  currency: string;
  period?: string;
}

export interface WireOp {
  id: string;
  machineId?: string;
  kind: 'resize' | 'reboot' | 'power_off' | 'power_on' | 'restart_runner' | 'restart_machine' | 'drain';
  status: 'pending' | 'running' | 'done' | 'failed' | 'cancelled';
  error?: string | null;
  chatsNow?: number | null;
}

export interface WireExtras {
  cloud?: unknown;
  hardware?: {
    cores: number;
    ramBytes: number;
    diskBytes: number;
    model?: string;
    laptop?: boolean;
    os?: string;
  } | null;
  load?: { cpuPct: number | null; ramPct: number | null; diskPct: number | null; at?: string } | null;
  week?: {
    chats: number;
    calls: number;
    unpriced: number;
    spend: { usd: number; shown: number } | null;
  } | null;
  managed?: MachineManaged | null;
  activeOps?: WireOp[];
}

export interface WirePoint {
  at: string;
  cpuAvg: number | null;
  ramAvg: number | null;
  diskAvg: number | null;
}

const GB = 2 ** 30;

export const gbOf = (bytes: number | null | undefined): number =>
  bytes && bytes > 0 ? Math.round(bytes / GB) : 0;

/** The pair `{kind, chats}` the card shows for the operation in flight; the newest unfinished one wins. */
export const busyOf = (ops: readonly WireOp[] | undefined): OrgMachine['busy'] => {
  const op = [...(ops ?? [])].reverse().find((o) => o.status === 'pending' || o.status === 'running');
  if (!op) return null;
  const kinds: Record<WireOp['kind'], MachineBusyKind> = {
    resize: 'resize',
    reboot: 'reboot',
    power_off: 'off',
    power_on: 'on',
    restart_runner: 'restart',
    restart_machine: 'reboot',
    drain: 'drain',
  };
  const cancellable = op.kind === 'drain' || op.kind === 'restart_runner' || op.kind === 'restart_machine';
  return { kind: kinds[op.kind], chats: op.chatsNow ?? 0, opId: op.id, cancellable };
};

export interface Rent {
  provider: string;
  plan: string;
  price: Money | null;
  cpu: number;
  ramGb: number;
  diskGb: number;
}

const num = (v: unknown): number => (typeof v === 'number' && Number.isFinite(v) ? v : 0);
const str = (v: unknown): string => (typeof v === 'string' ? v : '');
const moneyOf = (v: unknown): Money | null => {
  if (!v || typeof v !== 'object') return null;
  const m = v as Record<string, unknown>;
  return typeof m.amount === 'number' && typeof m.currency === 'string'
    ? { amount: m.amount, currency: m.currency }
    : null;
};

/** The server the hub rents for the machine; reads the provider's view and tolerates the older shape. */
export const rentOf = (cloud: unknown): Rent | null => {
  if (!cloud || typeof cloud !== 'object') return null;
  const c = cloud as Record<string, unknown>;
  return {
    provider: str(c.provider),
    plan: str(c.plan) || str(c.flavor),
    price: moneyOf(c.price),
    cpu: num(c.cpu) || num(c.vcpus),
    ramGb: num(c.ramGB) || num(c.ramGb),
    diskGb: num(c.diskGB) || num(c.diskGb),
  };
};

/** The rent in roubles per month; null when the provider prices it in another currency or does not price it. */
export const monthRub = (price: Money | null): number | null =>
  price && price.currency.toUpperCase() === 'RUB' ? Math.round(price.amount / 100) : null;

/** The last 24 hours as 24 values 0..100: the points are split into equal runs and each run is averaged. */
export const hourly = (points: readonly WirePoint[], pick: 'cpuAvg' | 'ramAvg' | 'diskAvg'): number[] => {
  const vals = points.map((p) => p[pick]);
  if (vals.every((v) => v === null)) return [];
  const n = 24;
  const out: number[] = [];
  let last = 0;
  for (let i = 0; i < n; i++) {
    const from = Math.floor((i * vals.length) / n);
    const to = Math.max(from + 1, Math.floor(((i + 1) * vals.length) / n));
    const run = vals.slice(from, to).filter((v): v is number => v !== null);
    if (run.length > 0) last = run.reduce((a, b) => a + b, 0) / run.length;
    out.push(Math.max(0, Math.min(100, Math.round(last))));
  }
  return out;
};
