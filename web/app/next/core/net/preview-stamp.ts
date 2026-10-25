// Who opened a preview slot last. A slot's cookie is one per slot number, so whoever opens it last owns it: every
// opening (the new interface's and the older panels') stamps its slot here, and a page that keeps a slot open reads the
// stamp to learn that it was taken. No imports: both interfaces use it.

const nonces = new Map<string, string>();
// No machine in the key: the older panels do not always know the machine's id. Another machine's same slot number costs
// one extra reopen; a missed takeover would show the wrong app.
const nonceKey = (slot: number) => `hl-preview-slot:${slot}`;

/** The nonce of the slot's latest opening, from any window of this browser; null when none is known. */
export const slotNonce = (slot: number): string | null => {
  try {
    return localStorage.getItem(nonceKey(slot));
  } catch {
    return nonces.get(nonceKey(slot)) ?? null;
  }
};

/** Marks the slot as just opened: a fresh nonce in localStorage, in memory when storage throws. */
export const stampSlot = (slot: number): string => {
  const nonce = `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`;
  nonces.set(nonceKey(slot), nonce);
  try {
    localStorage.setItem(nonceKey(slot), nonce);
  } catch {
    // The in-memory stamp still tells this window's openers apart.
  }
  return nonce;
};
