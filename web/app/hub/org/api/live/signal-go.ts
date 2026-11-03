// Where the button of a signal leads: the hub's action names the thing it is about, and every kind opens the
// card of that thing on its screen, not just the screen. One table for «Обзор» and «Аналитика».
import type { Signal as ApiSignal } from '../../../../hub-api/analytics';
import type { SignalGo } from '../overview';
import { KPI } from './analytics';

type Action = NonNullable<ApiSignal['action']>;

const q = (pairs: Record<string, string>): string => new URLSearchParams(pairs).toString();

export const signalGo = (a: Action | null): SignalGo => {
  if (!a) return { label: 'Открыть', section: 'overview' };
  const label = a.label;
  switch (a.kind) {
    case 'raise_limit':
      if (a.userId) return { label, section: 'teams', query: q({ p: a.userId }) };
      if (a.scope.kind === 'team')
        return { label, section: 'teams', query: q({ t: a.scope.id, dlg: 'limit' }) };
      return { label, section: 'teams' };
    case 'move_account':
      return { label, section: 'accounts', query: q({ a: a.fromAccountId }) };
    case 'register_account':
      return { label, section: 'accounts', query: q({ a: a.accountId, dlg: 'register' }) };
    case 'open_machine':
      return { label, section: 'machines', query: q({ m: a.machineId }) };
    case 'resend_invite':
      return { label, section: 'teams', query: q({ dlg: 'invite' }) };
    case 'open_kpi': {
      const id = KPI[a.kpi]?.id;
      return { label, section: 'analytics', ...(id ? { query: q({ k: id }) } : {}) };
    }
    case 'open_chat':
      return {
        label,
        section: 'machines',
        query: q({ m: a.machineId }),
        chat: { machineId: a.machineId, sessionKey: a.sessionKey },
      };
    case 'open_prices':
      return { label, section: 'overview', query: q({ spend: 'model', hl: a.model }) };
  }
};
