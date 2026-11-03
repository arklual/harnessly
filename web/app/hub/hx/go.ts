// Where a signal's button leads: a section with its card open (the query), or the chat of a machine in a new tab.
import { navigate } from '../lib/router';
import type { SignalGo } from '../org/api/overview';

export const signalHref = (base: string, a: SignalGo): string =>
  a.chat
    ? `/api/machines/${encodeURIComponent(a.chat.machineId)}/open?redirect=1`
    : `${base}/${a.section}${a.query ? `?${a.query}` : ''}`;

export const goSignal = (base: string, a: SignalGo) => {
  if (a.chat) {
    window.open(signalHref(base, a), '_blank', 'noopener');
    return;
  }
  navigate(signalHref(base, a));
};
