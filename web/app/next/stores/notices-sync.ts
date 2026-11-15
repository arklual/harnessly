// The person's notices live in a database: the hub's when the hub answers and its switch is on, the
// runtime's otherwise. The page asks one path and the answerer does not matter. localStorage (stores/notices.ts) stays
// a cache for the first paint and for a page with no answer: this module loads what the database holds, sends what the
// person did here (a notice taken away is a read, the switches are saved), and applies what the stream brings from
// the other devices, so a notice read on one device goes from the others without a poll.
import { deviceId } from '@/next/core/device';
import {
  applyRemoteNotify,
  dropNotices,
  noticesSnapshot,
  notifySnapshot,
  pushNotice,
  subscribeNotices,
  subscribeNotify,
  type INotice,
} from './notices';

interface IItem {
  key: string;
  chatId: string;
  at: number;
  doc: INotice;
}
interface IState {
  items: IItem[];
  reads: { key: string }[];
  settings: { doc?: unknown; rev: number };
}
interface IEvent {
  type: 'new' | 'read' | 'settings';
  item?: IItem;
  keys?: string[];
  settings?: { doc?: unknown; rev: number };
  from?: string;
}

export interface INoticesStream {
  addEventListener: (type: string, fn: (e: { data: string }) => void) => void;
  close: () => void;
}

export interface INoticesSyncOptions {
  /** `<runtime>/api/platform/notifications`, without the query. */
  base: string;
  person: string;
  fetchImpl?: typeof fetch;
  stream?: (url: string) => INoticesStream;
  device?: string;
}

/** Starts the sync; returns its stop. A failed answer leaves the cache alone and tries again at the next change. */
export const startNoticesSync = (o: INoticesSyncOptions): (() => void) => {
  const f = o.fetchImpl ?? ((...a: Parameters<typeof fetch>) => fetch(...a));
  const device = o.device ?? deviceId();
  // The base may carry the machine's query (runtimePath adds `?machine=`): the paths go before it, the person after.
  const [root, baseQuery] = o.base.split('?', 2) as [string, string | undefined];
  const q = `?${baseQuery ? `${baseQuery}&` : ''}person=${encodeURIComponent(o.person)}`;
  let live = true;
  let rev = 0;
  /** Keys the database holds as unread: what leaves the list while in this set is a read to send. */
  const known = new Set<string>();
  let applying = false;

  const send = (path: string, method: string, body: unknown) =>
    f(`${root}${path}${q}`, {
      method,
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).catch(() => undefined);

  const apply = (fn: () => void) => {
    applying = true;
    try {
      fn();
    } finally {
      applying = false;
    }
  };
  const take = (item: IItem) => {
    known.add(item.key);
    if (!noticesSnapshot().some((n) => n.key === item.key)) apply(() => pushNotice(item.doc));
  };

  const onList = () => {
    if (applying || !live) return;
    const now = new Set(noticesSnapshot().map((n) => n.key));
    const gone = [...known].filter((k) => !now.has(k));
    for (const k of gone) known.delete(k);
    if (gone.length > 0) void send('/read', 'POST', { keys: gone, from: device });
    for (const n of noticesSnapshot()) {
      if (known.has(n.key)) continue;
      known.add(n.key);
      void send('/items', 'POST', { key: n.key, chatId: n.chatId, at: n.at, doc: n, from: device });
    }
  };

  const onSettings = () => {
    if (applying || !live) return;
    void f(`${root}/settings${q}`, {
      method: 'PUT',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ doc: notifySnapshot(), rev, from: device }),
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((s: { rev?: number } | null) => {
        if (s && typeof s.rev === 'number') rev = s.rev;
      })
      .catch(() => undefined);
  };

  void f(`${root}${q}`, { credentials: 'include' })
    .then((r) => (r.ok ? (r.json() as Promise<IState>) : null))
    .then((st) => {
      if (!st || !live) return;
      const reads = new Set(st.reads.map((r) => r.key));
      for (const item of st.items) if (!reads.has(item.key)) take(item);
      apply(() => dropNotices((n) => reads.has(n.key)));
      for (const k of reads) known.delete(k);
      rev = st.settings.rev;
      if (st.settings.rev > 0 && st.settings.doc) apply(() => applyRemoteNotify(st.settings.doc));
      onList(); // what this page has and the database lacks goes up
    })
    .catch(() => undefined);

  const unList = subscribeNotices(onList);
  const unNotify = subscribeNotify(onSettings);

  let es: INoticesStream | null = null;
  try {
    es =
      o.stream?.(`${root}/stream${q}`) ??
      (typeof EventSource === 'undefined'
        ? null
        : (new EventSource(`${root}/stream${q}`, { withCredentials: true }) as unknown as INoticesStream));
  } catch {
    es = null;
  }
  const on = (type: IEvent['type'], fn: (e: IEvent) => void) =>
    es?.addEventListener(type, (m) => {
      if (!live) return;
      try {
        const e = JSON.parse(m.data) as IEvent;
        if (e.from === device) return; // this page's own echo
        fn(e);
      } catch {
        // A line that is not an event is no news.
      }
    });
  on('new', (e) => e.item && take(e.item));
  on('read', (e) => {
    const keys = new Set(e.keys ?? []);
    for (const k of keys) known.delete(k);
    apply(() => dropNotices((n) => keys.has(n.key)));
  });
  on('settings', (e) => {
    if (!e.settings?.doc) return;
    rev = e.settings.rev;
    apply(() => applyRemoteNotify(e.settings?.doc));
  });

  return () => {
    live = false;
    unList();
    unNotify();
    es?.close();
  };
};
