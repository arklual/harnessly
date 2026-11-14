// Sinopia's home in one answer (GET <platform>/design/home). The home used to ask the machine six times — the
// library, the shelves, the design systems, the kits, the apps' document types and their documents — each a trip
// through the hub. The runtime now asks itself for all of them side by side and answers once; the readers of the parts
// stay as they are and take their part from that answer when there is a fresh one, and ask their own route when there
// is not (the switch hub-design-home is off and the hub answers 404, an older runtime, a part that failed, a poll).
import { deviceId } from '@/next/core/device';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { DEVICE_HEADER, type MachineId } from '@/next/protocol/v2';

/**
 * The machine's parts, and the hub's copy of the library from its store (storeLibrary): the hub answers with
 * the copy alone when the machine is slow or off, and the page reads the machine's parts by their own routes.
 */
export type THomePart = 'library' | 'layout' | 'systems' | 'kits' | 'kinds' | 'kindDocs' | 'storeLibrary';

interface IHomeAnswer {
  parts: Partial<Record<THomePart, unknown>>;
}

interface IHomeRead {
  /** When it was asked; another read of the whole home waits AGAIN_MS after it. */
  at: number;
  /** When the answer came; its parts are fresh for FRESH_MS after it. */
  arrived: number | null;
  answer: Promise<IHomeAnswer | null>;
  /** The parts a reader already took: a second read of a part (a poll, a reload) asks its own route. */
  taken: Set<THomePart>;
}

/** How long the parts of an answer stand for the machine's: the readers of one opening of the home take them. */
export const HOME_FRESH_MS = 3000;
/** The whole home is read again at most this often; between, the parts are read on their own, as before. */
export const HOME_AGAIN_MS = 30_000;
/** A hub that answered 404 (the switch is off, or no such route) is asked again after this long. */
export const HOME_OFF_MS = 10 * 60_000;
const TIMEOUT_MS = 30_000;

const reads = new Map<MachineId, IHomeRead>();
const offUntil = new Map<MachineId, number>();

const ask = async (m: MachineId): Promise<IHomeAnswer | null> => {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), TIMEOUT_MS);
  try {
    const res = await fetch(runtimePath(platformPath('/design/home'), m), {
      credentials: 'same-origin',
      // The parts' own routes are asked with the person's device, as the page asks them.
      headers: { [DEVICE_HEADER]: deviceId() },
      signal: ctl.signal,
    });
    if (res.status === 404) {
      offUntil.set(m, Date.now() + HOME_OFF_MS);
      return null;
    }
    if (!res.ok) return null;
    const body: unknown = await res.json();
    if (typeof body !== 'object' || body === null || !('parts' in body)) return null;
    const parts = (body as { parts: unknown }).parts;
    return typeof parts === 'object' && parts !== null ? { parts: parts as IHomeAnswer['parts'] } : null;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
};

/**
 * The home is about to be read (its first open, the link back, a reload): one answer for all its parts, unless one was
 * asked less than HOME_AGAIN_MS ago or the hub said it has none.
 */
export const primeHome = (m: MachineId, now = Date.now()): void => {
  if ((offUntil.get(m) ?? 0) > now) return;
  const last = reads.get(m);
  if (last && now - last.at < HOME_AGAIN_MS) return;
  const read: IHomeRead = { at: now, arrived: null, answer: Promise.resolve(null), taken: new Set() };
  read.answer = ask(m).then((answer) => {
    read.arrived = Date.now();
    return answer;
  });
  reads.set(m, read);
};

/** The part from a fresh answer of the home, taken once; undefined when there is none and the reader asks its route. */
const fromHome = async (m: MachineId, part: THomePart): Promise<unknown> => {
  const read = reads.get(m);
  if (!read || read.taken.has(part)) return undefined;
  if (read.arrived !== null && Date.now() - read.arrived > HOME_FRESH_MS) return undefined;
  read.taken.add(part);
  const answer = await read.answer;
  return answer?.parts[part];
};

/**
 * Whether the home that is being read for this machine carries the part (without taking it): the library reads the
 * hub's copy at once when the hub answered with it, instead of waiting MACHINE_WAIT_MS for the machine first.
 */
export const homeHas = async (m: MachineId, part: THomePart): Promise<boolean> => {
  const read = reads.get(m);
  if (!read || read.taken.has(part)) return false;
  if (read.arrived !== null && Date.now() - read.arrived > HOME_FRESH_MS) return false;
  const answer = await read.answer;
  return answer?.parts[part] !== undefined;
};

/** A part of the home: from its fresh answer when it has one, read by the part's own reader; else from its own route. */
export const viaHome = async <T>(
  m: MachineId,
  part: THomePart,
  read: (v: unknown) => T,
  own: () => Promise<T>,
): Promise<T> => {
  const pre = await fromHome(m, part);
  if (pre !== undefined) {
    try {
      return read(pre);
    } catch {
      // The part as the home gave it is not what its reader knows: its own route tells why.
    }
  }
  return own();
};

/** Forgets every answer (tests, a sign-out). */
export const forgetHome = () => {
  reads.clear();
  offUntil.clear();
};
