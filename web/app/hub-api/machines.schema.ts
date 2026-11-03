// Runtime schemas of machines.ts, for the fixture test.

import { money } from './common.schema';
import { arr, bool, lit, nullable, num, obj, str, time, unknownValue } from './schema';

export const machinePrice = obj({ amount: num, currency: str, period: str });
export const machineSize = obj({ cpu: nullable(num), ramGB: nullable(num), diskGB: nullable(num) });
export const machinePlan = obj({
  id: str,
  name: str,
  cpu: num,
  ramGB: num,
  diskGB: num,
  price: nullable(machinePrice),
  current: bool,
});
export const machineHardware = obj({
  cores: nullable(num),
  ramBytes: nullable(num),
  diskBytes: nullable(num),
  model: nullable(str),
  laptop: bool,
  os: nullable(str),
  arch: nullable(str),
  updatedAt: time,
});
export const machineCloud = obj({
  provider: str,
  plan: nullable(str),
  cpu: nullable(num),
  ramGB: nullable(num),
  diskGB: nullable(num),
  price: nullable(machinePrice),
  externalId: nullable(str),
});
export const machineLoad = obj({
  cpuPct: nullable(num),
  ramPct: nullable(num),
  diskPct: nullable(num),
  at: time,
});
export const machineWeek = obj({
  chats: num,
  calls: num,
  unpriced: num,
  spend: nullable(money),
});
export const machineManaged = obj({ budget: bool, env: bool, resources: bool, access: bool });
export const managedCapability = lit('budget', 'env', 'resources', 'access');

export const machineOp = obj({
  id: str,
  machineId: str,
  kind: lit('resize', 'reboot', 'power_off', 'power_on', 'restart_runner', 'restart_machine', 'drain'),
  status: lit('pending', 'running', 'done', 'failed', 'cancelled'),
  params: unknownValue,
  error: nullable(str),
  createdBy: nullable(str),
  createdAt: time,
  updatedAt: time,
  finishedAt: nullable(time),
  chatsNow: nullable(num),
});

export const resourcesResponse = obj({
  machineId: str,
  managed: bool,
  provider: obj({ name: str, supported: bool }),
  providerError: nullable(str),
  current: nullable(machineSize),
  price: nullable(machinePrice),
  plans: arr(machinePlan),
  hardware: nullable(machineHardware),
  consoleUrl: nullable(str),
  activeOps: arr(machineOp),
  updatedAt: nullable(time),
});
export const sizeRequest = obj({ cpu: num, ramGB: num, diskGB: num });
export const quoteResponse = obj({
  machineId: str,
  want: machineSize,
  price: nullable(machinePrice),
  current: nullable(machinePrice),
  delta: nullable(machinePrice),
});
export const resizeResponse = obj({ op: machineOp });
export const opsResponse = obj({ machineId: str, ops: arr(machineOp) });
export const opResponse = obj({ op: machineOp });
export const managedRequest = obj({ capability: managedCapability, managed: bool });
export const managedResponse = obj({ machineId: str, managed: machineManaged });

export const metricPoint = obj({
  at: time,
  cpuAvg: num,
  cpuMax: num,
  ramAvg: num,
  ramMax: num,
  diskAvg: num,
  diskMax: num,
  chatsMax: num,
  samples: num,
});
export const metricsResponse = obj({
  machineId: str,
  range: lit('1h', '6h', '24h', '7d'),
  stepSeconds: num,
  from: time,
  to: time,
  points: arr(metricPoint),
});
export const machineOpEvent = obj({ machineId: str, op: machineOp });
