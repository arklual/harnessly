// Every command the page sent, by opId: pending the moment it is sent (with «дольше обычного» after slowMs), then
// done — the runtime answered and the socket has shown the effect — or failed with whether a retry can help.

import { createKeyedStore, useKey } from '@/next/core/store';
import type { CmdResult, Channel, MachineId, Ms, Op, RefuseCode } from '@/next/protocol/v2';

export type OpFailure =
  | { r: 'refused'; code: RefuseCode }
  | { r: 'failed'; code: 'timeout' | 'network'; status: null }
  | { r: 'failed'; code: 'http'; status: number };

export type OpState =
  | { k: 'pending'; op: Op; machine: MachineId; since: Ms; slow: boolean; stage: 'request' | 'effect' }
  | { k: 'done'; op: Op; machine: MachineId; result: Extract<CmdResult, { ch: Channel }>; at: Ms }
  | { k: 'failed'; op: Op; machine: MachineId; failure: OpFailure; retryable: boolean; at: Ms };

export const opsStore = createKeyedStore<string, OpState>();

export const useOp = (opId: string | null) => useKey(opsStore, opId);

/** The button of an op is busy while it is pending. */
export const isBusy = (s: OpState | undefined): boolean => s?.k === 'pending';
