// The proxy for a machine's agents: the form (shared by the failure card and the machine's proxy line) and the line
// that shows what is set. The proxy is applied on top of the agents' launch by the machine; here it is only asked for.
import { useState } from 'react';

import { Button, TextField } from '@kit';

import { useApi } from '../lib/env';
import { errorText } from '../lib/errors';
import { isSocksUrl, validProxyUrl } from '../lib/format';
import { t } from '../lib/i18n';
import { PROVIDER_INFO } from '../lib/agent-view';
import type { Machine, Provider, ProviderCheck, ProxyEntry, ProxyScope } from '../lib/types';
import { Plug } from '../ui/icons';
import { LinkButton, Msg, Note, Segmented } from '../ui/parts';
import type { Live } from '../lib/hooks';

const scopeLabel = (scope: ProxyScope): string =>
  scope === 'all' ? t('proxy.scopeAll') : t('proxy.scopeOnly', { name: PROVIDER_INFO[scope].name });

/** Which agents a proxy of this scope reaches, and so which provider checks to run after saving it. */
export const providersOfScope = (scope: ProxyScope): Provider[] =>
  scope === 'all' ? ['claude', 'codex'] : [scope];

export const ProxyForm = ({
  machine,
  live,
  scopes,
  onDone,
  onCancel,
}: {
  machine: Machine;
  live: Live;
  /** The scopes to choose from; the first is the default. */
  scopes: ProxyScope[];
  /** The checks that ran with the saved proxy. */
  onDone: (checks: ProviderCheck[]) => void;
  onCancel: () => void;
}) => {
  const api = useApi();
  const [url, setUrl] = useState('');
  const [scope, setScope] = useState<ProxyScope>(scopes[0] ?? 'all');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const socks = isSocksUrl(url);

  const save = async () => {
    if (!validProxyUrl(url)) {
      setError(t('proxy.bad'));
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const { proxy } = await api.proxy.set(machine.id, scope, url.trim());
      live.push({ type: 'event', event: { type: 'machine', data: { machine: { ...machine, proxy } } } });
      const checks = await Promise.all(
        providersOfScope(scope).map((p) => api.agents.check(machine.id, p).then((r) => r.check)),
      );
      setUrl('');
      onDone(checks);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="hub-stack">
      <TextField
        size="m"
        label={t('proxy.address')}
        className="hub-mono"
        placeholder="http://логин:пароль@proxy.company.ru:3128"
        autoComplete="off"
        spellCheck={false}
        value={url}
        onChange={(e) => setUrl(e.target.value)}
        error={error ?? undefined}
      />
      {socks && (
        <div className="hub-bridge">
          <Plug size={16} />
          <div>
            <b>{t('proxy.socks.t')}</b>
            <span>{t('proxy.socks.s')}</span>
          </div>
        </div>
      )}
      {scopes.length > 1 && (
        <div className="hub-row hub-scope" data-between>
          <span className="hub-label">{t('proxy.apply')}</span>
          <Segmented
            value={scope}
            onChange={setScope}
            label={t('proxy.forWhom')}
            options={scopes.map((s) => ({ value: s, label: scopeLabel(s) }))}
          />
        </div>
      )}
      <Note>{t('proxy.warning')}</Note>
      <div className="hub-row" data-end>
        <LinkButton onClick={onCancel}>{t('common.cancel')}</LinkButton>
        <Button size="s" appearance="primary" loading={busy} onClick={() => void save()}>
          {t('proxy.saveCheck')}
        </Button>
      </div>
    </div>
  );
};

const entryText = (e: ProxyEntry): string =>
  `${e.scheme}://${e.host}${e.bridge ? ` · ${t('proxy.bridge', { addr: e.bridge })}` : ''}`;

/** What is set now, with a way to remove it; and a way to set one when nothing is. */
export const ProxyBar = ({ machine, live }: { machine: Machine; live: Live }) => {
  const api = useApi();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState<ProxyScope | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [checks, setChecks] = useState<ProviderCheck[]>([]);
  const set = (['all', 'claude', 'codex'] as const).flatMap((s) => {
    const e = machine.proxy[s];
    return e ? [{ scope: s, entry: e }] : [];
  });

  const clear = async (scope: ProxyScope) => {
    setBusy(scope);
    setError(null);
    try {
      const { proxy } = await api.proxy.clear(machine.id, scope);
      live.push({ type: 'event', event: { type: 'machine', data: { machine: { ...machine, proxy } } } });
      setChecks([]);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="hub-stack">
      <div className="hub-row" data-between>
        <span className="hub-label">{t('proxy.title')}</span>
        {!open && (
          <LinkButton onClick={() => setOpen(true)}>
            {set.length ? t('proxy.change') : t('proxy.set')}
          </LinkButton>
        )}
      </div>
      {set.length === 0 && !open && <Note>{t('proxy.direct')}</Note>}
      {set.map(({ scope, entry }) => (
        <div key={scope} className="hub-row" data-between>
          <span className="hub-note">
            <b>{scopeLabel(scope)}</b> · <span className="hub-mono">{entryText(entry)}</span>
          </span>
          <LinkButton danger disabled={busy !== null} onClick={() => void clear(scope)}>
            {t('proxy.remove')}
          </LinkButton>
        </div>
      ))}
      {open && (
        <ProxyForm
          machine={machine}
          live={live}
          scopes={['all', 'claude', 'codex']}
          onCancel={() => setOpen(false)}
          onDone={(done) => {
            setChecks(done);
            setOpen(false);
          }}
        />
      )}
      {checks.map((c) => (
        <Msg key={c.provider} tone={c.verdict === 'ok' ? 'success' : 'warn'}>
          {t(c.verdict === 'ok' ? 'proxy.checkOk' : 'proxy.checkBad', {
            name: PROVIDER_INFO[c.provider].name,
          })}
        </Msg>
      ))}
      {error && <Msg tone="danger">{error}</Msg>}
    </div>
  );
};
