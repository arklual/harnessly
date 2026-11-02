import type { Role } from '../types';

export interface MockCtx {
  role: Role;
}

/** A mock answer arrives after a short pause, so loading states are real in the mock too. */
export const later = <T>(value: T, ms = 280): Promise<T> =>
  new Promise((resolve) => globalThis.setTimeout(() => resolve(value), ms));
