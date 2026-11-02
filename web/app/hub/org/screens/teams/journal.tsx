import { useCallback, useEffect, useState } from 'react';

import { Button, Spinner } from '@kit';

import { orgErrorText } from '../../api/errors';
import type { AuditEntry } from '../../api/teams';
import { useOrg } from '../../context';
import { ago, describe } from './format';

/** The journal of changes: who changed what and when; the newest first, more on request. */
export const Journal = ({ slug, rev, now }: { slug: string; rev: number; now: number }) => {
  const { api, shell } = useOrg();
  const org = shell.org.slug;
  // null: not loaded yet (not an empty journal).
  const [events, setEvents] = useState<AuditEntry[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [more, setMore] = useState(false);

  useEffect(() => {
    let alive = true;
    api.teamsAudit(org, slug, null).then(
      (p) => {
        if (!alive) return;
        setEvents(p.events);
        setNext(p.next);
        setError(null);
      },
      (e: unknown) => alive && setError(e),
    );
    return () => {
      alive = false;
    };
  }, [api, org, slug, rev]);

  const loadMore = useCallback(async () => {
    setMore(true);
    try {
      const p = await api.teamsAudit(org, slug, next);
      setEvents((cur) => [...(cur ?? []), ...p.events]);
      setNext(p.next);
    } catch (e) {
      setError(e);
    } finally {
      setMore(false);
    }
  }, [api, org, slug, next]);

  return (
    <section className="ht-journal" aria-labelledby="ht-journal-h">
      <h2 id="ht-journal-h" className="ht-journal__h">
        Журнал изменений
      </h2>
      <p className="ht-journal__sub">Кто что менял: роли, команды и лимиты.</p>
      {error != null ? (
        <p className="ht-err">{orgErrorText(error)}</p>
      ) : events === null ? (
        <div className="ht-journal__wait" role="status" aria-label="Загрузка">
          <Spinner size="s" />
        </div>
      ) : events.length === 0 ? (
        <p className="ht-help">Пока никто ничего не менял.</p>
      ) : (
        <ul className="ht-journal__list">
          {events.map((e) => {
            const d = describe(e);
            return (
              <li key={e.id}>
                <span className="ht-journal__line">
                  <b>{d.who}</b> {d.what}
                </span>
                <time dateTime={e.at}>{ago(e.at, now)}</time>
              </li>
            );
          })}
        </ul>
      )}
      {next !== null && (
        <Button size="s" loading={more} onClick={() => void loadMore()}>
          Показать ещё
        </Button>
      )}
    </section>
  );
};
