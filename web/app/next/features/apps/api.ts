// The Apps screen over v2: what the machine reads from the tracker and GitLab with the person's own tokens
// (GET .../apps). Nothing here changes anything, so there is no command.
import { getPage } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { parsePage, type MachineId } from '@/next/protocol/v2';

/** The machine reads two services one after another, each within its own term: the page waits for the slowest. */
const APPS_TIMEOUT_MS = 20_000;

export const loadApps = (m: MachineId) =>
  getPage(machineApi(m).http('/apps'), parsePage.apps, APPS_TIMEOUT_MS);
