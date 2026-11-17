// The machines' requests for a Teleport identity (tbot) that wait for the person (docs/hub/tbot-join.md).
// The hub answers 404 while the feature is off: then the section shows nothing.
import { useCallback, useEffect, useState } from 'react';

import { http } from '../../../org/api/live/http';
import { orgErrorText } from '../../../org/api/errors';
import { Btn } from '../../ui';

interface TbotJoin {
  id: string;
  machineId: string;
  machineName: string;
  host?: string;
  bot: string;
  token: string;
  fingerprint: string;
  state: 'pending' | 'approved';
  createdAt: string;
}

const when = (iso: string): string =>
  new Date(iso).toLocaleString('ru-RU', {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });

export const TbotJoins = () => {
  const [list, setList] = useState<TbotJoin[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    http.get<{ requests: TbotJoin[] }>('/api/me/tbot-joins').then(
      (r) => setList(r.requests ?? []),
      () => setList([]),
    );
  }, []);
  useEffect(load, [load]);

  const act = async (j: TbotJoin, action: 'approve' | 'decline' | 'revoke') => {
    setBusy(`${j.id}:${action}`);
    setError(null);
    try {
      await http.post(`/api/me/tbot-joins/${encodeURIComponent(j.id)}/${action}`);
    } catch (e) {
      setError(orgErrorText(e));
    } finally {
      setBusy(null);
      load();
    }
  };

  if (list.length === 0 && !error) return null;
  return (
    <>
      {error && (
        <div className="mx-err" role="alert">
          <span>{error}</span>
        </div>
      )}
      {list.map((j) => {
        const name = j.machineName || j.host || j.machineId;
        return (
          <div className="mx-join" key={j.id} role={j.state === 'pending' ? 'alert' : undefined}>
            <span>
              {j.state === 'pending'
                ? `Новая машина ${name} просит доступ как ${j.bot}`
                : `Машина ${name} входит в кластер как ${j.bot}`}
              {` · ключ ${j.fingerprint} · ${when(j.createdAt)}`}
            </span>
            {j.state === 'pending' ? (
              <>
                <Btn
                  variant="primary"
                  size="sm"
                  busy={busy === `${j.id}:approve`}
                  disabled={busy !== null}
                  onClick={() => void act(j, 'approve')}
                >
                  Разрешить
                </Btn>
                <Btn
                  variant="outline"
                  size="sm"
                  busy={busy === `${j.id}:decline`}
                  disabled={busy !== null}
                  onClick={() => void act(j, 'decline')}
                >
                  Отказать
                </Btn>
              </>
            ) : (
              <Btn
                variant="outline"
                size="sm"
                busy={busy === `${j.id}:revoke`}
                disabled={busy !== null}
                onClick={() => void act(j, 'revoke')}
              >
                Отозвать
              </Btn>
            )}
          </div>
        );
      })}
    </>
  );
};
