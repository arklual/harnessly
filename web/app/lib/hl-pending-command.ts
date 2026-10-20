/**
 * The first line of a new conversation that is a slash command: the shell stores it right after the
 * conversation is created, the composer of that conversation takes it once the agent is ready.
 * sessionStorage is shared by the shell and the same-origin runtime iframes of one browser tab.
 * Tab ids repeat across machines, so the key carries the machine: the shell passes `conversation.machineId`,
 * the composer reads its own with `useContext(RuntimeMachineContext)` (the shell's default machine, or the
 * iframe's `?machine`); both are undefined outside the fleet.
 */

const PREFIX = 'hl:pending-command:';
/** A command older than this belongs to a start that never reached a composer; it is not sent any more. */
const MAX_AGE_MS = 10 * 60_000;
const MAX_LENGTH = 20_000;

interface IPendingCommand {
  line: string;
  at: number;
}

const keyOf = (machineId: string | undefined, tabId: string) => `${PREFIX}${machineId ?? ''}:${tabId}`;

export const setPendingFirstCommand = (
  machineId: string | undefined,
  tabId: string,
  line: string,
): boolean => {
  const text = line.trim();
  if (!tabId || !text || text.length > MAX_LENGTH) return false;
  try {
    sessionStorage.setItem(
      keyOf(machineId, tabId),
      JSON.stringify({ line: text, at: Date.now() } satisfies IPendingCommand),
    );
    return true;
  } catch {
    return false;
  }
};

/** Returns the command once: the entry is removed on read, expired or not. */
export const takePendingFirstCommand = (
  machineId: string | undefined,
  tabId: string,
  now = Date.now(),
): string | null => {
  try {
    const key = keyOf(machineId, tabId);
    const raw = sessionStorage.getItem(key);
    if (raw === null) return null;
    sessionStorage.removeItem(key);
    const saved = JSON.parse(raw) as Partial<IPendingCommand>;
    if (typeof saved.line !== 'string' || typeof saved.at !== 'number' || now - saved.at > MAX_AGE_MS)
      return null;
    return saved.line;
  } catch {
    return null;
  }
};
