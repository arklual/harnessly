// The Apps screen over v2: what the machine reads from the tracker and GitLab with the person's own tokens
// (GET .../apps). Nothing here changes anything, so there is no command.
import { getPage } from '@/next/core/net/http';
import { machineApi } from '@/next/core/net/machine';
import { parsePage, type MachineId } from '@/next/protocol/v2';

/** The machine reads two services one after another, each within its own term: the page waits for the slowest. */
const APPS_TIMEOUT_MS = 20_000;

/** `only`: the apps to read (the review start reads GitLab alone, without waiting for the tracker); none: all. */
export const loadApps = (m: MachineId, only: readonly string[] = []) =>
  getPage(
    machineApi(m).http(
      only.length ? `/apps?${only.map((id) => `app=${encodeURIComponent(id)}`).join('&')}` : '/apps',
    ),
    parsePage.apps,
    APPS_TIMEOUT_MS,
  );
