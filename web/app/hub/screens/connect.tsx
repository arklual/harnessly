// /connect?code=ABCD-EFGH: the person confirms that one of their own apps (a launcher, a script) may read and write their
// chats; the app waits for the answer and gets a personal token (docs/hub/client-api.md).
import { useEffect, useState } from 'react';

import { Button, TextField } from '@kit';

import { useApi } from '../lib/env';
import { errorText } from '../lib/errors';
import { t } from '../lib/i18n';
import { navigate, useQuery } from '../lib/router';
import type { DeviceRequest } from '../lib/types';
import { PlainPage } from '../ui/frame';
import { ListSkeleton, Msg, Note } from '../ui/parts';
import { SpaceMenu } from './home';

type Phase = 'loading' | 'ask' | 'allowed' | 'denied' | 'error';

/** The user code as the hub keeps it: upper case, with its dash. */
export const normalCode = (raw: string): string => {
  const s = raw.toUpperCase().replace(/[^A-Z0-9]/g, '');
  return s.length === 8 ? `${s.slice(0, 4)}-${s.slice(4)}` : s;
};

export const ConnectScreen = () => {
  const api = useApi();
  const query = useQuery();
  const code = normalCode(query.get('code') ?? '');
  const [typed, setTyped] = useState('');
  const [request, setRequest] = useState<DeviceRequest | null>(null);
  const [phase, setPhase] = useState<Phase>('loading');
  const [error, setError] = useState<string | null>(null);
  const [prompt, setPrompt] = useState(true);
  const [busy, setBusy] = useState<'allow' | 'deny' | null>(null);

  useEffect(() => {
    if (!code) return;
    let live = true;
    api.device
      .show(code)
      .then((r) => {
        if (!live) return;
        setRequest(r);
        setPrompt(r.scopes.includes('prompt'));
        setPhase('ask');
      })
      .catch((e: unknown) => {
        if (!live) return;
        setError(errorText(e));
        setPhase('error');
      });
    return () => {
      live = false;
    };
  }, [api, code]);

  const answer = async (allow: boolean) => {
    setBusy(allow ? 'allow' : 'deny');
    setError(null);
    try {
      await api.device.approve(code, allow, prompt ? ['read', 'prompt'] : ['read']);
      setPhase(allow ? 'allowed' : 'denied');
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  if (!code) {
    return (
      <PlainPage title={t('connect.title')} side={<SpaceMenu />}>
        <Note>{t('connect.noCode')}</Note>
        <form
          className="hub-inputrow"
          onSubmit={(e) => {
            e.preventDefault();
            if (typed.trim())
              navigate(`/connect?code=${encodeURIComponent(normalCode(typed))}`, { replace: true });
          }}
        >
          <TextField
            size="m"
            label={t('connect.code')}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
          />
          <Button type="submit" disabled={!typed.trim()}>
            {t('connect.find')}
          </Button>
        </form>
      </PlainPage>
    );
  }

  return (
    <PlainPage title={t('connect.title')} side={<SpaceMenu />}>
      {phase === 'loading' && <ListSkeleton rows={2} />}
      {phase === 'error' && error && <Msg tone="danger">{error}</Msg>}
      {phase === 'allowed' && <Msg tone="success">{t('connect.allowed')}</Msg>}
      {phase === 'denied' && <Msg tone="info">{t('connect.denied')}</Msg>}
      {phase === 'ask' && request && (
        <section className="hub-section">
          <h2 className="hub-section__t">
            {request.device
              ? t('connect.ask', { client: request.client, device: request.device })
              : t('connect.askNoDevice', { client: request.client })}
          </h2>
          <Note>{t('connect.check')}</Note>
          <p className="hub-connect__code" data-testid="connect-code">
            {code}
          </p>
          <div className="hub-list">
            <label className="hub-item">
              <span className="hub-item__head">
                <input type="checkbox" checked disabled />
                <span className="hub-t__n">{t('connect.read')}</span>
              </span>
            </label>
            {request.scopes.includes('prompt') && (
              <label className="hub-item">
                <span className="hub-item__head">
                  <input type="checkbox" checked={prompt} onChange={(e) => setPrompt(e.target.checked)} />
                  <span className="hub-t__n">{t('connect.prompt')}</span>
                </span>
              </label>
            )}
          </div>
          {request.ip && <Note>{t('connect.from', { ip: request.ip })}</Note>}
          {error && <Msg tone="danger">{error}</Msg>}
          <div className="hub-row">
            <Button
              appearance="primary"
              loading={busy === 'allow'}
              disabled={busy !== null}
              onClick={() => void answer(true)}
            >
              {t('connect.allow')}
            </Button>
            <Button loading={busy === 'deny'} disabled={busy !== null} onClick={() => void answer(false)}>
              {t('connect.deny')}
            </Button>
          </div>
        </section>
      )}
    </PlainPage>
  );
};
