// Puts every store of the core back to empty between tests: the stores are module singletons, as on the page.

import { resetClockOffsets, setPageClock, systemClock } from '@/next/core/clock';
import { resetDeviceId } from '@/next/core/device';
import { setFetch } from '@/next/core/net/http';
import { clearPersonData } from '@/next/core/session';

export function resetCore(): void {
  clearPersonData();
  resetClockOffsets();
  resetDeviceId();
  setPageClock(systemClock);
  setFetch((url, init) => fetch(url, init));
}
