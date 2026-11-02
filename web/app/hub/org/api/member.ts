// The participant's own view (HRN-541): only their limit, their chats, their machine. Aligns with `GET /api/me/limits`,
// `/api/me/sessions`, `/api/me/usage`, `POST /api/me/limit-requests` of the plan (§2.11).
export interface MyLimit {
  spentRub: number;
  limitRub: number;
  /** Where the month ends at the current pace. */
  forecastRub: number;
  warnPct: number;
  /** What the hub does about a limit: off, it only counts; warn, it tells; stop, it stops the agents. */
  mode: 'off' | 'warn' | 'stop';
  /** The team lead who set the limit (the one «Попросить больше» goes to). */
  setBy: { userId: string; name: string };
  /** ISO time of the reset. */
  resetsAt: string;
  breakdown: { label: string; kind: 'model' | 'machine'; rub: number }[];
}

export type ChatEnd = 'running' | 'merged' | 'review' | 'abandoned';

export interface MyChat {
  id: string;
  title: string;
  machine: string;
  end: ChatEnd;
  /** «MR !412»: the result, when there is one. */
  mr: string | null;
  /** ISO time of the last activity; null: running now. */
  endedAt: string | null;
  costUsd: number;
}

export interface MyOverview {
  /** Null: no limit binds this person. */
  limit: MyLimit | null;
  /** This month, on the organization's accounts and the person's own. */
  spentRub: number;
  /** The spend is at list prices: the accounts have no month's price, and money limits do not bind them. */
  estimated: boolean;
  breakdown: MyLimit['breakdown'];
  chats: MyChat[];
  tiles: {
    chats: { week: number; running: number; bars: number[] };
    /** `merged`: null while the hub does not tell it per session. */
    mr: { week: number; merged: number | null; bars: number[] };
    machine: { name: string; online: boolean; loadPct: number; bars: number[] } | null;
  };
}

export interface MemberApi {
  myOverview(period: 'week' | 'month'): Promise<MyOverview>;
  /** Asks the lead for a bigger limit; the lead gets a signal. Resolves when the hub accepted it. */
  requestMoreLimit(): Promise<{ sent: true }>;
}
