// The «Профиль» screen of the new hub, the board's `pProfile`: the person's own data, how they sign in, their access
// keys and the organizations they belong to. Data and actions are the old account screen's (screens/account.tsx).
// Its own chunk: it loads when the person opens the section.
import { useCallback, useEffect, useState, type ReactNode } from 'react';

import { setThemeMode, useTheme, type ThemeMode } from '@kit';

import { isApiError, oauthStartUrl } from '../../lib/api';
import { useApi, useEnv } from '../../lib/env';
import { errorText } from '../../lib/errors';
import { hhmm } from '../../lib/format';
import { currentLang, t, type Lang } from '../../lib/i18n';
import { navigate } from '../../lib/router';
import { useMe, useSession } from '../../lib/session';
import type { AppToken } from '../../lib/types';
import { OrgApiError, orgErrorText } from '../../org/api/errors';
import { useOrg } from '../../org/context';
import { useSignOut } from '../../screens/home';
import { Head } from '../shell';
import { Btn, initial } from '../ui';
import { useUrl } from './teams/url';

const TABS = [
  ['me', 'Профиль'],
  ['login', 'Способы входа'],
  ['tokens', 'Ключи доступа'],
  ['orgs', 'Мои организации'],
] as const;
type Tab = (typeof TABS)[number][0];

const PROVIDER_NAMES: Record<string, string> = { github: 'GitHub', yandex: 'Яндекс' };

const ORG_COLORS = ['#5b5bd6', '#2f9e6b', '#d97a1f', '#c2417a', '#3a7bd5', '#8968f4'];
const orgColor = (slug: string): string => {
  let h = 0;
  for (const ch of slug) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  return ORG_COLORS[h % ORG_COLORS.length];
};

const Row = ({ title, d, children }: { title: ReactNode; d?: ReactNode; children?: ReactNode }) => (
  <div className="set">
    <div>
      <div className="hd-t">{title}</div>
      {d != null && <div className="hd-d">{d}</div>}
    </div>
    <div>{children}</div>
  </div>
);

const Card = ({ children }: { children: ReactNode }) => (
  <div className="t-card viz-card dash-tile hd-card flat">{children}</div>
);

function Seg<V extends string>({
  value,
  label,
  options,
  onChange,
}: {
  value: V;
  label: string;
  options: [V, string][];
  onChange: (v: V) => void;
}) {
  return (
    <div className="hx-set__seg" role="radiogroup" aria-label={label}>
      {options.map(([v, text]) => (
        <button
          key={v}
          type="button"
          className="hx-set__seg-item"
          role="radio"
          aria-checked={value === v}
          onClick={() => onChange(v)}
        >
          {text}
        </button>
      ))}
    </div>
  );
}

const whenUsed = (iso: string): string =>
  new Date(iso).toLocaleString(currentLang(), {
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  });

const Screen = () => {
  const api = useApi();
  const env = useEnv();
  const me = useMe();
  const { config, reload } = useSession();
  const signOut = useSignOut();
  const { mode } = useTheme();
  const url = useUrl([]);
  const raw = url.get('tab');
  const tab: Tab = TABS.find(([id]) => id === raw)?.[0] ?? 'me';

  const [name, setName] = useState(me.user.name);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
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
  const linked = new Map(me.identities.map((i) => [i.provider, i]));
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

  const saveName = () =>
    void run('name', async () => {
      await api.me.rename(name.trim());
      setSaved(true);
    });

  const profile = (
    <Card>
      <Row title="Имя" d="Как вас видят коллеги.">
        <form
          className="row"
          style={{ gap: 8 }}
          onSubmit={(e) => {
            e.preventDefault();
            saveName();
          }}
        >
          <input
            className="inp"
            style={{ width: 260 }}
            aria-label="Имя"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              setSaved(false);
            }}
          />
          <Btn
            type="submit"
            size="sm"
            busy={busy === 'name'}
            disabled={!name.trim() || name.trim() === me.user.name}
          >
            {saved ? t('common.saved') : t('common.save')}
          </Btn>
        </form>
      </Row>
      <Row title="Почта" d="Для входа и уведомлений.">
        <span className="mut">{me.user.email}</span>
      </Row>
      <Row title="Тема" d="Тёмная или светлая.">
        <Seg<ThemeMode>
          value={mode}
          label="Тема"
          onChange={setThemeMode}
          options={[
            ['dark', 'Тёмная'],
            ['light', 'Светлая'],
            ['system', 'Как в системе'],
          ]}
        />
      </Row>
      <Row title="Язык" d="Язык интерфейса хаба и чатов.">
        <select
          className="inp"
          style={{ width: 200 }}
          aria-label="Язык"
          value={currentLang()}
          onChange={(e) => {
            window.location.assign(`${window.location.pathname}?lang=${e.target.value as Lang}`);
          }}
        >
          <option value="ru">Русский</option>
          <option value="en">English</option>
        </select>
      </Row>
    </Card>
  );

  const login = (
    <Card>
      <Row title="Почта" d={me.user.email}>
        <span className="pill ok">подключена</span>
      </Row>
      {providers.map((p) => {
        const id = linked.get(p);
        return (
          <Row key={p} title={nameOf(p)} d={id ? `Подключён · ${id.email}` : 'Не подключён'}>
            {id ? (
              <Btn
                size="sm"
                variant="ghost"
                busy={busy === p}
                onClick={() => void run(p, () => api.me.removeIdentity(p))}
              >
                {t('account.unlink')}
              </Btn>
            ) : (
              <Btn size="sm" onClick={() => env.assign(oauthStartUrl(p, 'link', '/settings'))}>
                Подключить
              </Btn>
            )}
          </Row>
        );
      })}
      {canPasskey && (
        <>
          <Row title="Ключи входа (passkey)" d={me.passkeys.length ? 'Вход отпечатком или Face ID.' : 'Нет'}>
            <form
              className="row"
              style={{ gap: 8 }}
              onSubmit={(e) => {
                e.preventDefault();
                void addPasskey();
              }}
            >
              <input
                className="inp"
                style={{ width: 200 }}
                aria-label={t('account.passkey.name')}
                value={passkeyName}
                onChange={(e) => setPasskeyName(e.target.value)}
              />
              <Btn type="submit" size="sm" icon="plus" busy={busy === 'passkey'}>
                Добавить ключ
              </Btn>
            </form>
          </Row>
          {me.passkeys.map((k) => (
            <Row
              key={k.id}
              title={k.name}
              d={
                k.lastUsedAt
                  ? t('account.passkey.used', { time: hhmm(k.lastUsedAt) })
                  : t('account.passkey.unused')
              }
            >
              <Btn
                size="sm"
                variant="ghost"
                busy={busy === k.id}
                aria-label={t('account.passkey.remove')}
                title={t('account.passkey.remove')}
                onClick={() => void run(k.id, () => api.me.removePasskey(k.id))}
              >
                Удалить
              </Btn>
            </Row>
          ))}
        </>
      )}
    </Card>
  );

  const body = tab === 'me' ? profile : tab === 'login' ? login : tab === 'tokens' ? <Keys /> : <Orgs />;

  return (
    <>
      <Head
        title="Профиль"
        lead="Ваши данные и способы входа. Это не настройки организации."
        scope={false}
        acts={
          <Btn variant="ghost" icon="log-out" onClick={signOut}>
            Выйти
          </Btn>
        }
      />
      <nav className="hx-set__nav hb-tabs hd-tabs">
        {TABS.map(([id, label]) => (
          <button
            key={id}
            type="button"
            className="hx-set__nav-item"
            aria-current={tab === id}
            onClick={() => url.put({ tab: id === 'me' ? null : id })}
          >
            <span>{label}</span>
          </button>
        ))}
      </nav>
      <div style={{ maxWidth: 1100 }}>
        {body}
        {error && (
          <p className="mut sm" style={{ marginTop: 8 }}>
            {error}
          </p>
        )}
      </div>
    </>
  );
};

/** The person's own apps connected through the client API; the list is there while the hub's switch is on. */
const Keys = () => {
  const api = useApi();
  const [state, setState] = useState<{ on: boolean; tokens: AppToken[] } | null | 'none'>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    () =>
      api.me
        .tokens()
        .then(setState)
        .catch(() => setState('none')),
    [api],
  );
  useEffect(() => {
    void load();
  }, [load]);

  const revoke = async (id: string) => {
    setBusy(id);
    setError(null);
    try {
      await api.me.revokeToken(id);
      await load();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  if (state === null) return <span className="hx-sk" style={{ display: 'block', height: 96 }} />;
  if (state === 'none' || !state.on)
    return <div className="mut">Ключи доступа на этом сервере выключены.</div>;
  return (
    <>
      <div className="mut" style={{ marginBottom: 12 }}>
        {t('apps.note')}
      </div>
      <Card>
        <table className="tbl">
          <thead>
            <tr>
              <th>Название</th>
              <th>Создан</th>
              <th>Последний раз</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {state.tokens.map((a) => (
              <tr key={a.id}>
                <td className="nm">{a.device ? `${a.client} · ${a.device}` : a.client}</td>
                <td className="mut">{whenUsed(a.createdAt)}</td>
                <td className="mut">{whenUsed(a.lastUsedAt)}</td>
                <td className="num">
                  <Btn size="sm" variant="ghost" busy={busy === a.id} onClick={() => void revoke(a.id)}>
                    {t('apps.revoke')}
                  </Btn>
                </td>
              </tr>
            ))}
            {state.tokens.length === 0 && (
              <tr>
                <td className="mut" colSpan={4}>
                  Пока ни одного ключа.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </Card>
      {error && (
        <p className="mut sm" style={{ marginTop: 8 }}>
          {error}
        </p>
      )}
    </>
  );
};

const Orgs = () => {
  const { shell, api } = useOrg();
  const cur = shell.org;
  const list = shell.orgs.length ? shell.orgs : [{ slug: cur.slug, name: cur.name, roleLabel: '' }];
  const [ask, setAsk] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const leave = async (slug: string) => {
    setBusy(true);
    setError(null);
    try {
      await api.leaveOrg(slug);
      const rest = list.find((o) => o.slug !== slug);
      window.location.assign(rest ? `/o/${rest.slug}` : '/');
    } catch (e) {
      setError(e instanceof OrgApiError && e.message ? e.message : orgErrorText(e));
      setBusy(false);
    }
  };
  return (
    <>
      <Card>
        <table className="tbl">
          <tbody>
            {list.map((o) => (
              <tr key={o.slug}>
                <td>
                  <div className="row">
                    <span className="hd-av" style={{ ['--c' as string]: orgColor(o.slug) }}>
                      {initial(o.name)}
                    </span>
                    <div>
                      <div className="nm">{o.name}</div>
                      {o.roleLabel && <div className="mut2 sm">{o.roleLabel}</div>}
                    </div>
                  </div>
                </td>
                <td className="num">
                  {o.slug === cur.slug ? (
                    <span className="pill acc">открыта</span>
                  ) : (
                    <Btn size="sm" onClick={() => navigate(`/o/${o.slug}`)}>
                      Перейти
                    </Btn>
                  )}{' '}
                  {ask === o.slug ? (
                    <>
                      <span className="mut2 sm">Ваши машины в ней отключатся.</span>{' '}
                      <Btn size="sm" variant="primary" busy={busy} onClick={() => void leave(o.slug)}>
                        Выйти
                      </Btn>{' '}
                      <Btn size="sm" variant="ghost" onClick={() => setAsk(null)}>
                        Отмена
                      </Btn>
                    </>
                  ) : (
                    <Btn
                      size="sm"
                      variant="ghost"
                      title="Выйти из организации"
                      onClick={() => {
                        setError(null);
                        setAsk(o.slug);
                      }}
                    >
                      Выйти
                    </Btn>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
      {error && (
        <p className="mut sm" style={{ marginTop: 8 }}>
          {error}
        </p>
      )}
    </>
  );
};

export default Screen;
