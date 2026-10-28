// /settings: the person — name, how they sign in (linked providers, passkeys), the look, sign out.
import { useState } from 'react';

import { Button, setThemeMode, TextField, useTheme, type ThemeMode } from '@kit';

import { isApiError, oauthStartUrl } from '../lib/api';
import { useApi, useEnv } from '../lib/env';
import { errorText } from '../lib/errors';
import { hhmm } from '../lib/format';
import { currentLang, t, type Lang } from '../lib/i18n';
import { Link } from '../lib/router';
import { useMe, useSession } from '../lib/session';
import { PlainPage } from '../ui/frame';
import { Fingerprint, LogOut, Moon, Monitor, Plus, Sun, Trash2 } from '../ui/icons';
import { Msg, Note, Segmented } from '../ui/parts';
import { SpaceMenu, useSignOut } from './home';

const PROVIDER_NAMES: Record<string, string> = { github: 'GitHub', yandex: 'Яндекс' };

export const AccountScreen = () => {
  const api = useApi();
  const env = useEnv();
  const me = useMe();
  const { config, reload } = useSession();
  const signOut = useSignOut();
  const { mode } = useTheme();
  const [name, setName] = useState(me.user.name);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [passkeyName, setPasskeyName] = useState(t('account.passkey.defaultName'));

  const run = async (key: string, job: () => Promise<unknown>) => {
    setBusy(key);
    setError(null);
    try {
      await job();
      await reload();
    } catch (e) {
      setError(isApiError(e, 'conflict') ? t('err.lastMethod') : errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const providers = [
    ...(config.auth.github ? ['github'] : []),
    ...(config.auth.yandex ? ['yandex'] : []),
    ...config.auth.oidc.map((p) => `oidc-${p.id}`),
  ];
  const nameOf = (id: string) =>
    PROVIDER_NAMES[id] ?? config.auth.oidc.find((p) => `oidc-${p.id}` === id)?.name ?? id;
  const linked = new Set(me.identities.map((i) => i.provider));
  const canPasskey = config.auth.passkey && env.passkeys.supported();

  const addPasskey = () =>
    run('passkey', async () => {
      const begin = await api.me.passkeyBegin();
      const credential = await env.passkeys.create(begin.publicKey);
      if (credential === null) return;
      await api.me.passkeyFinish(
        begin.challengeId,
        credential,
        passkeyName.trim() || t('account.passkey.defaultName'),
      );
    });

  return (
    <PlainPage title={t('account.title')} side={<SpaceMenu />}>
      <p className="hub-note">
        <Link to="/" className="hub-link">
          {t('account.back')}
        </Link>
      </p>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('account.profile')}</h2>
        <form
          className="hub-inputrow"
          data-labeled
          onSubmit={(e) => {
            e.preventDefault();
            void run('name', async () => {
              await api.me.rename(name.trim());
              setSaved(true);
            });
          }}
        >
          <TextField
            size="m"
            label={t('account.name')}
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setSaved(false);
            }}
          />
          <Button type="submit" loading={busy === 'name'} disabled={name.trim() === me.user.name}>
            {saved ? t('common.saved') : t('common.save')}
          </Button>
        </form>
        <Note>{me.user.email}</Note>
      </section>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('account.methods')}</h2>
        <div className="hub-list">
          <div className="hub-item">
            <div className="hub-item__head">
              <span className="hub-t">
                <span className="hub-t__n">{t('account.email')}</span>
                <span className="hub-item__s">{me.user.email}</span>
              </span>
            </div>
          </div>
          {me.identities.map((i) => (
            <div key={i.provider} className="hub-item">
              <div className="hub-item__head">
                <span className="hub-t">
                  <span className="hub-t__n">{nameOf(i.provider)}</span>
                  <span className="hub-item__s">{i.email}</span>
                </span>
                <Button
                  size="xs"
                  appearance="ghost"
                  loading={busy === i.provider}
                  onClick={() => void run(i.provider, () => api.me.removeIdentity(i.provider))}
                >
                  {t('account.unlink')}
                </Button>
              </div>
            </div>
          ))}
        </div>
        <div className="hub-row">
          {providers
            .filter((p) => !linked.has(p))
            .map((p) => (
              <Button
                key={p}
                size="s"
                icon={<Plus size={14} />}
                onClick={() => env.assign(oauthStartUrl(p, 'link', '/settings'))}
              >
                {t('account.link', { name: nameOf(p) })}
              </Button>
            ))}
        </div>
      </section>

      {canPasskey && (
        <section className="hub-section">
          <h2 className="hub-section__t">{t('account.passkeys')}</h2>
          {me.passkeys.length === 0 ? (
            <Note>{t('account.passkeys.none')}</Note>
          ) : (
            <div className="hub-list">
              {me.passkeys.map((k) => (
                <div key={k.id} className="hub-item">
                  <div className="hub-item__head">
                    <span className="hub-ic">
                      <Fingerprint size={18} />
                    </span>
                    <span className="hub-t">
                      <span className="hub-t__n">{k.name}</span>
                      <span className="hub-item__s">
                        {k.lastUsedAt
                          ? t('account.passkey.used', { time: hhmm(k.lastUsedAt) })
                          : t('account.passkey.unused')}
                      </span>
                    </span>
                    <Button
                      appearance="ghost"
                      size="xs"
                      className="k-btn--icon"
                      aria-label={t('account.passkey.remove')}
                      title={t('account.passkey.remove')}
                      loading={busy === k.id}
                      onClick={() => void run(k.id, () => api.me.removePasskey(k.id))}
                    >
                      <Trash2 size={14} />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          )}
          <form
            className="hub-inputrow"
            onSubmit={(e) => {
              e.preventDefault();
              void addPasskey();
            }}
          >
            <TextField
              size="m"
              aria-label={t('account.passkey.name')}
              value={passkeyName}
              onChange={(e) => setPasskeyName(e.target.value)}
            />
            <Button type="submit" icon={<Plus size={14} />} loading={busy === 'passkey'}>
              {t('account.passkey.add')}
            </Button>
          </form>
        </section>
      )}

      <section className="hub-section">
        <h2 className="hub-section__t">{t('account.look')}</h2>
        <div className="hub-row" data-between>
          <span className="hub-label">{t('account.theme')}</span>
          <Segmented<ThemeMode>
            value={mode}
            label={t('account.theme')}
            onChange={setThemeMode}
            options={[
              { value: 'light', label: <Sun size={14} /> },
              { value: 'dark', label: <Moon size={14} /> },
              { value: 'system', label: <Monitor size={14} /> },
            ]}
          />
        </div>
        <div className="hub-row" data-between>
          <span className="hub-label">{t('account.language')}</span>
          <Segmented<Lang>
            value={currentLang()}
            label={t('account.language')}
            onChange={(lang) => {
              window.location.assign(`${window.location.pathname}?lang=${lang}`);
            }}
            options={[
              { value: 'ru', label: 'Русский' },
              { value: 'en', label: 'English' },
            ]}
          />
        </div>
      </section>

      {error && <Msg tone="danger">{error}</Msg>}
      <div>
        <Button icon={<LogOut size={14} />} onClick={signOut}>
          {t('menu.signOut')}
        </Button>
      </div>
    </PlainPage>
  );
};
