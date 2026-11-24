// /events: the journal of events, live (docs/hub/event-bus.md). What reached the hub — a connector's event of a machine,
// an outside webhook, a schedule that fired — appears at the top as it is written; «Раньше» reads the past. The level
// (the person's own, a space, an organization, the whole installation) is what the hub lets the person read. A click
// on an event shows its envelope and its chain: the events of the same thread. The second tab is the subscriptions:
// who reads the journal, how far behind, and why one stopped.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

import { Button } from '@kit';

import { useApi } from '../lib/env';
import { errorText } from '../lib/errors';
import {
  type BusEvent,
  type BusLevel,
  type BusSubscription,
  type BusTrigger,
  chainOf,
  domainOf,
  domains,
  EVENTS_API,
  lag,
  matches,
  merge,
  TRIGGER_MODES,
  type TriggerMode,
} from '../lib/events';
import { currentLang, t } from '../lib/i18n';
import { useMe } from '../lib/session';
import { PlainPage } from '../ui/frame';
import { ListSkeleton, Msg, Note, Pill, Segmented } from '../ui/parts';

type Tab = 'journal' | 'triggers' | 'subs';
type Live = 'connecting' | 'live' | 'paused' | 'lost';

const time = (iso: string): string => {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const today = new Date().toDateString() === d.toDateString();
  return d.toLocaleString(currentLang(), {
    ...(today ? {} : { day: 'numeric', month: 'short' }),
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
};

const levelName = (l: BusLevel): string =>
  l.kind === 'me'
    ? t('events.level.me')
    : l.kind === 'all'
      ? t('events.level.all')
      : `${t(`events.level.${l.kind}`)}: ${l.name}`;

export const EventsScreen = () => {
  const [tab, setTab] = useState<Tab>('journal');
  return (
    <PlainPage title={t('events.title')} wide>
      <Note>{t('events.lead')}</Note>
      <Segmented<Tab>
        label={t('events.title')}
        value={tab}
        onChange={setTab}
        options={[
          { value: 'journal', label: t('events.tab.journal') },
          { value: 'triggers', label: t('events.tab.triggers') },
          { value: 'subs', label: t('events.tab.subs') },
        ]}
      />
      {tab === 'journal' ? <Journal /> : tab === 'triggers' ? <Triggers /> : <Subscriptions />}
    </PlainPage>
  );
};

const Journal = () => {
  const api = useApi();
  const [levels, setLevels] = useState<BusLevel[] | null>(null);
  const [scope, setScope] = useState('me');
  const [list, setList] = useState<BusEvent[]>([]);
  const [fresh, setFresh] = useState<Set<number>>(new Set());
  const [live, setLive] = useState<Live>('connecting');
  const [paused, setPaused] = useState(false);
  const [search, setSearch] = useState('');
  const [domain, setDomain] = useState('');
  const [picked, setPicked] = useState<BusEvent | null>(null);
  const [error, setError] = useState('');
  const [more, setMore] = useState(true);
  const [loadingOld, setLoadingOld] = useState(false);
  const [heldN, setHeldN] = useState(0);
  const held = useRef<BusEvent[]>([]);
  const pausedRef = useRef(paused);
  useEffect(() => {
    pausedRef.current = paused;
  }, [paused]);

  useEffect(() => {
    api.events
      .scopes()
      .then((r) => setLevels(r.scopes))
      .catch((e: unknown) => setError(errorText(e)));
  }, [api]);

  // The past first, then the stream from where the past ends: nothing is missed between the two.
  useEffect(() => {
    let es: EventSource | null = null;
    let alive = true;
    api.events
      .page({ scope })
      .then((r) => {
        if (!alive) return;
        setList(r.events);
        setMore(r.events.length === 200);
        const head = r.events[0]?.seq ?? 0;
        const url = `${EVENTS_API}/stream?scope=${encodeURIComponent(scope)}${head ? `&after=${head}` : ''}`;
        es = new EventSource(url, { withCredentials: true });
        es.onopen = () => alive && setLive(pausedRef.current ? 'paused' : 'live');
        es.onerror = () => alive && setLive('lost');
        es.addEventListener('bus', (m) => {
          if (!alive) return;
          let e: BusEvent;
          try {
            e = JSON.parse((m as MessageEvent<string>).data) as BusEvent;
          } catch {
            return;
          }
          if (pausedRef.current) {
            held.current.push(e);
            setHeldN(held.current.length);
            return;
          }
          setList((l) => merge(l, [e]));
          setFresh((f) => new Set(f).add(e.seq));
          window.setTimeout(() => {
            setFresh((f) => {
              const n = new Set(f);
              n.delete(e.seq);
              return n;
            });
          }, 2400);
        });
      })
      .catch((e: unknown) => alive && setError(errorText(e)));
    return () => {
      alive = false;
      es?.close();
    };
  }, [api, scope]);

  const togglePause = () => {
    if (paused) {
      const out = held.current;
      held.current = [];
      setHeldN(0);
      setList((l) => merge(l, out));
      setLive('live');
    } else {
      setLive('paused');
    }
    setPaused(!paused);
  };

  const older = useCallback(() => {
    const last = list[list.length - 1];
    if (!last) return;
    setLoadingOld(true);
    api.events
      .page({ scope, before: last.seq })
      .then((r) => {
        setList((l) => [...l, ...r.events.filter((e) => !l.some((x) => x.seq === e.seq))]);
        setMore(r.events.length === 200);
      })
      .catch((e: unknown) => setError(errorText(e)))
      .finally(() => setLoadingOld(false));
  }, [api, list, scope]);

  const shown = useMemo(
    () => list.filter((e) => (!domain || domainOf(e.type) === domain) && matches(e, search)),
    [list, domain, search],
  );
  const counts = useMemo(() => domains(list), [list]);

  if (error) return <Msg tone="warn">{error}</Msg>;
  if (!levels) return <ListSkeleton rows={4} />;

  return (
    <div className="hub-ev">
      <div className="hub-ev__bar">
        <select
          className="hub-ev__scope"
          value={scope}
          onChange={(e) => {
            // A new level starts empty: its past is read again and its stream opened anew.
            setList([]);
            setPicked(null);
            setMore(true);
            setLive('connecting');
            setScope(e.target.value);
          }}
          aria-label={t('events.level.label')}
        >
          {levels.map((l) => (
            <option key={l.scope} value={l.scope}>
              {levelName(l)}
            </option>
          ))}
        </select>
        <input
          className="hub-ev__search"
          type="search"
          placeholder={t('events.search')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <span className="hub-ev__live" data-state={live}>
          <span className="hub-ev__dot" />
          {t(`events.live.${live}`)}
          {paused && heldN > 0 && ` · ${heldN}`}
        </span>
        <Button size="xs" appearance="ghost" onClick={togglePause}>
          {paused ? t('events.resume') : t('events.pause')}
        </Button>
      </div>
      {counts.length > 0 && (
        <div className="hub-ev__domains">
          <button
            type="button"
            className="hub-ev__chip"
            data-on={domain === '' ? '' : undefined}
            onClick={() => setDomain('')}
          >
            {t('events.allDomains')} <b>{list.length}</b>
          </button>
          {counts.map(([d, n]) => (
            <button
              key={d}
              type="button"
              className="hub-ev__chip"
              data-on={domain === d ? '' : undefined}
              onClick={() => setDomain(domain === d ? '' : d)}
            >
              {d} <b>{n}</b>
            </button>
          ))}
        </div>
      )}
      <div className="hub-ev__main">
        <ol className="hub-ev__list">
          {shown.length === 0 && (
            <li className="hub-ev__empty">{t(list.length ? 'events.noMatch' : 'events.empty')}</li>
          )}
          {shown.map((e) => (
            <li key={e.seq}>
              <button
                type="button"
                className="hub-ev__row"
                data-fresh={fresh.has(e.seq) ? '' : undefined}
                data-picked={picked?.seq === e.seq ? '' : undefined}
                onClick={() => setPicked(picked?.seq === e.seq ? null : e)}
              >
                <time className="hub-ev__time">{time(e.at)}</time>
                <span className="hub-ev__type">{e.type}</span>
                <span className="hub-ev__src">{e.source}</span>
                <span className="hub-ev__who">{e.mine ? t('events.mine') : (e.actor ?? '')}</span>
                <span className="hub-ev__tags">
                  {e.via.map((v) => (
                    <Pill key={v} tone="muted">
                      {v}
                    </Pill>
                  ))}
                  {e.visibility === 'person' && (
                    <span className="hub-ev__lock" title={t('events.personal')}>
                      ●
                    </span>
                  )}
                </span>
              </button>
            </li>
          ))}
          {more && list.length > 0 && (
            <li className="hub-ev__more">
              <Button size="xs" appearance="ghost" onClick={older} disabled={loadingOld}>
                {t('events.older')}
              </Button>
            </li>
          )}
        </ol>
        {picked && <Detail e={picked} chain={chainOf(list, picked)} onPick={setPicked} />}
      </div>
    </div>
  );
};

const Detail = ({ e, chain, onPick }: { e: BusEvent; chain: BusEvent[]; onPick: (e: BusEvent) => void }) => {
  const rows: [string, string | undefined][] = [
    ['seq', String(e.seq)],
    [t('events.f.type'), e.type],
    [t('events.f.source'), e.source],
    [t('events.f.at'), time(e.at)],
    [t('events.f.received'), time(e.received)],
    [t('events.f.actor'), e.actor],
    [t('events.f.thread'), e.thread],
    [t('events.f.connection'), e.connection],
    [t('events.f.ext'), e.ext],
    [t('events.f.ref'), e.ref],
    [t('events.f.visibility'), t(`events.vis.${e.visibility}`)],
  ];
  return (
    <aside className="hub-ev__detail">
      <dl className="hub-ev__fields">
        {rows
          .filter(([, v]) => v)
          .map(([k, v]) => (
            <div key={k}>
              <dt>{k}</dt>
              <dd>{v}</dd>
            </div>
          ))}
        {e.url && (
          <div>
            <dt>{t('events.f.url')}</dt>
            <dd>
              <a href={e.url} target="_blank" rel="noreferrer noopener" className="hub-link">
                {e.url}
              </a>
            </dd>
          </div>
        )}
      </dl>
      {chain.length > 1 && (
        <>
          <h3 className="hub-ev__h">{t('events.chain', { n: chain.length })}</h3>
          <ol className="hub-ev__chain">
            {chain.map((c) => (
              <li key={c.seq} data-on={c.seq === e.seq ? '' : undefined}>
                <button type="button" onClick={() => onPick(c)}>
                  <time>{time(c.at)}</time> {c.type}
                  {c.actor ? ` · ${c.actor}` : ''}
                </button>
              </li>
            ))}
          </ol>
        </>
      )}
      <h3 className="hub-ev__h">{t('events.envelope')}</h3>
      <pre className="hub-ev__json">{JSON.stringify(e, null, 2)}</pre>
    </aside>
  );
};

const Subscriptions = () => {
  const api = useApi();
  const me = useMe();
  const [hub, setHub] = useState(false);
  const [data, setData] = useState<{ subscriptions: BusSubscription[]; head: number } | null>(null);
  const [error, setError] = useState('');
  const load = useCallback(() => {
    api.events
      .subscriptions(hub)
      .then(setData)
      .catch((e: unknown) => setError(errorText(e)));
  }, [api, hub]);
  useEffect(() => {
    load();
    const id = window.setInterval(load, 5000);
    return () => window.clearInterval(id);
  }, [load]);
  const act = (p: Promise<void>) => p.then(load).catch((e: unknown) => setError(errorText(e)));

  return (
    <div className="hub-ev">
      {me.user.isAdmin && (
        <Segmented<'me' | 'hub'>
          label={t('events.tab.subs')}
          value={hub ? 'hub' : 'me'}
          onChange={(v) => setHub(v === 'hub')}
          options={[
            { value: 'me', label: t('events.subs.mine') },
            { value: 'hub', label: t('events.subs.hub') },
          ]}
        />
      )}
      {error && <Msg tone="warn">{error}</Msg>}
      {!data ? (
        <ListSkeleton rows={2} />
      ) : data.subscriptions.length === 0 ? (
        <Note>{t('events.subs.none')}</Note>
      ) : (
        <table className="hub-ev__subs">
          <thead>
            <tr>
              <th>{t('events.subs.consumer')}</th>
              <th>{t('events.subs.state')}</th>
              <th>{t('events.subs.lag')}</th>
              <th>{t('events.subs.error')}</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {data.subscriptions.map((s) => (
              <tr key={s.id}>
                <td>
                  <b>{s.consumer}</b>
                  {s.name ? ` · ${s.name}` : ''}
                </td>
                <td>
                  <Pill tone={s.state === 'active' ? undefined : s.state === 'paused' ? 'muted' : 'warn'}>
                    {t(`events.subs.st.${s.state}`)}
                  </Pill>
                </td>
                <td className="hub-ev__num">{lag(s, data.head)}</td>
                <td className="hub-ev__err">{s.lastError ?? ''}</td>
                <td className="hub-ev__acts">
                  <Button
                    size="xs"
                    appearance="ghost"
                    onClick={() => void act(api.events.pause(s.id, s.state !== 'paused', hub))}
                  >
                    {s.state === 'paused' ? t('events.resume') : t('events.pause')}
                  </Button>
                  <Button
                    size="xs"
                    appearance="ghost"
                    onClick={() => void act(api.events.replay(s.id, Math.max(0, s.cursor - 100), hub))}
                  >
                    {t('events.subs.replay')}
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
};

/**
 * The catalog of triggers: every kind of event the person's connections sent, how often, and what the hub
 * does with it. A kind is in the digest from its first event; the person turns a notice or the agent on for it, or
 * switches it off — then its events are not even journaled.
 */
const Triggers = () => {
  const api = useApi();
  const [list, setList] = useState<BusTrigger[] | null>(null);
  const [error, setError] = useState('');
  const load = useCallback(() => {
    api.events
      .triggers()
      .then((r) => setList(r.triggers))
      .catch((e: unknown) => setError(errorText(e)));
  }, [api]);
  useEffect(load, [load]);
  const set = (tr: BusTrigger, mode: TriggerMode) => {
    setList((l) => l?.map((x) => (x === tr ? { ...x, mode } : x)) ?? l);
    api.events
      .setTrigger({ source: tr.source, connection: tr.connection, type: tr.type, mode })
      .catch((e: unknown) => {
        setError(errorText(e));
        load();
      });
  };
  if (error) return <Msg tone="warn">{error}</Msg>;
  if (!list) return <ListSkeleton rows={3} />;
  if (list.length === 0) return <Note>{t('events.trig.none')}</Note>;
  return (
    <div className="hub-ev">
      <Note>{t('events.trig.lead')}</Note>
      <table className="hub-ev__subs">
        <thead>
          <tr>
            <th>{t('events.f.source')}</th>
            <th>{t('events.f.type')}</th>
            <th>{t('events.trig.seen')}</th>
            <th>{t('events.trig.last')}</th>
            <th>{t('events.trig.mode')}</th>
          </tr>
        </thead>
        <tbody>
          {list.map((tr) => (
            <tr key={`${tr.source}|${tr.connection ?? ''}|${tr.type}`}>
              <td>
                <b>{tr.source}</b>
                {tr.connection ? ` · ${tr.connection}` : ''}
              </td>
              <td className="hub-ev__type">{tr.type}</td>
              <td className="hub-ev__num">{tr.seen}</td>
              <td className="hub-ev__time">{time(tr.last)}</td>
              <td>
                <select
                  className="hub-ev__scope"
                  value={tr.mode}
                  aria-label={t('events.trig.mode')}
                  onChange={(e) => set(tr, e.target.value as TriggerMode)}
                >
                  {TRIGGER_MODES.map((m) => (
                    <option key={m} value={m}>
                      {t(`events.trig.m.${m}`)}
                    </option>
                  ))}
                </select>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
};
