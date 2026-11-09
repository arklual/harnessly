// /network: the closed network's policy of the whole installation — internal domains, the company's root certificates, the
// proxy and its exceptions, and who may sign apps. Only an administrator of the installation; the machines take the saved
// policy by themselves within minutes (docs/self-host/closed-network.md).
import { useEffect, useState } from 'react';

import { Button, TextArea, TextField } from '@kit';

import { isApiError } from '../lib/api';
import { useApi } from '../lib/env';
import { errorText } from '../lib/errors';
import { t, type Key } from '../lib/i18n';
import {
  checkForm,
  problemsOf,
  toForm,
  toPolicy,
  type NetworkField,
  type NetworkForm,
  type NetworkProblem,
} from '../lib/network';
import { Link } from '../lib/router';
import { useMe } from '../lib/session';
import { PlainPage } from '../ui/frame';
import { Plus, Trash2 } from '../ui/icons';
import { ListSkeleton, Msg, Note, Segmented } from '../ui/parts';
import { SpaceMenu } from './home';

const problemText = (p: NetworkProblem): string =>
  p.text ?? (p.key ? t(p.key as Key, { value: p.value ?? '' }) : '');

const messageOf = (ps: NetworkProblem[], field: NetworkField): string | undefined => {
  const own = ps.filter((p) => p.field === field).map(problemText);
  return own.length > 0 ? own.join(' ') : undefined;
};

export const NetworkScreen = () => {
  const me = useMe();
  return (
    <PlainPage title={t('network.title')} side={<SpaceMenu />}>
      <p className="hub-note">
        <Link to="/settings" className="hub-link">
          {t('account.back')}
        </Link>
      </p>
      {me.user.isAdmin ? <NetworkForms /> : <Msg tone="warn">{t('network.adminOnly')}</Msg>}
    </PlainPage>
  );
};

const NetworkForms = () => {
  const api = useApi();
  const [form, setForm] = useState<NetworkForm | null>(null);
  const [saved, setSaved] = useState<NetworkForm | null>(null);
  const [problems, setProblems] = useState<NetworkProblem[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);

  useEffect(() => {
    let alive = true;
    api.admin.network.get().then(
      (p) => {
        if (!alive) return;
        const f = toForm(p);
        setForm(f);
        setSaved(f);
      },
      (e: unknown) => {
        if (alive) setError(errorText(e));
      },
    );
    return () => {
      alive = false;
    };
  }, [api]);

  if (form === null || saved === null) {
    return error ? <Msg tone="danger">{error}</Msg> : <ListSkeleton rows={4} />;
  }

  const change = (patch: Partial<NetworkForm>) => {
    setForm({ ...form, ...patch });
    setDone(false);
  };
  const dirty = JSON.stringify(form) !== JSON.stringify(saved);

  const save = async () => {
    const found = checkForm(form);
    setProblems(found);
    setError(null);
    if (found.length > 0) return;
    setBusy(true);
    try {
      const back = toForm(await api.admin.network.put(toPolicy(form)));
      setForm(back);
      setSaved(back);
      setDone(true);
    } catch (e) {
      if (isApiError(e, 'invalid_request') && problemsOf(e.details).length > 0)
        setProblems(problemsOf(e.details));
      else setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const setPublisher = (i: number, patch: Partial<NetworkForm['publishers'][number]>) =>
    change({ publishers: form.publishers.map((p, j) => (j === i ? { ...p, ...patch } : p)) });

  return (
    <form
      className="hub-net"
      onSubmit={(e) => {
        e.preventDefault();
        void save();
      }}
    >
      <Note>{t('network.lead')}</Note>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('network.hosts.title')}</h2>
        <TextArea
          label={t('network.hosts.label')}
          hint={t('network.hosts.hint')}
          error={messageOf(problems, 'internalHosts')}
          rows={4}
          spellCheck={false}
          value={form.internalHosts}
          placeholder={'crm.bank.local\n*.bank.local'}
          onChange={(e) => change({ internalHosts: e.target.value })}
        />
      </section>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('network.ca.title')}</h2>
        <TextArea
          label={t('network.ca.label')}
          hint={t('network.ca.hint')}
          error={messageOf(problems, 'caPem')}
          rows={6}
          spellCheck={false}
          className="hub-net__pem"
          value={form.caPem}
          placeholder="-----BEGIN CERTIFICATE-----"
          onChange={(e) => change({ caPem: e.target.value })}
        />
      </section>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('network.proxy.title')}</h2>
        <TextField
          size="m"
          label={t('network.proxy.label')}
          hint={t('network.proxy.hint')}
          error={messageOf(problems, 'proxy')}
          value={form.proxy}
          placeholder="http://proxy.bank.local:3128"
          onChange={(e) => change({ proxy: e.target.value })}
        />
        <TextArea
          label={t('network.noProxy.label')}
          hint={t('network.noProxy.hint')}
          error={messageOf(problems, 'noProxy')}
          rows={3}
          spellCheck={false}
          value={form.noProxy}
          placeholder={'.bank.local\n10.0.0.0/8'}
          onChange={(e) => change({ noProxy: e.target.value })}
        />
      </section>

      <section className="hub-section">
        <h2 className="hub-section__t">{t('network.sign.title')}</h2>
        <Note>{t('network.sign.lead')}</Note>
        <Segmented
          label={t('network.sign.title')}
          value={form.requireSigned ? 'on' : 'off'}
          options={[
            { value: 'off', label: t('network.sign.off') },
            { value: 'on', label: t('network.sign.on') },
          ]}
          onChange={(v) => change({ requireSigned: v === 'on' })}
        />
        {messageOf(problems, 'requireSigned') && (
          <Msg tone="danger">{messageOf(problems, 'requireSigned')}</Msg>
        )}
        <div className="hub-net__pubs">
          {form.publishers.map((p, i) => (
            <div key={i} className="hub-net__pub">
              <TextField
                size="m"
                label={t('network.pub.name')}
                value={p.name}
                onChange={(e) => setPublisher(i, { name: e.target.value })}
              />
              <TextField
                size="m"
                label={t('network.pub.key')}
                value={p.key}
                spellCheck={false}
                onChange={(e) => setPublisher(i, { key: e.target.value })}
              />
              <Button
                size="xs"
                appearance="ghost"
                aria-label={t('network.pub.remove')}
                onClick={() => change({ publishers: form.publishers.filter((_, j) => j !== i) })}
              >
                <Trash2 size={14} />
              </Button>
            </div>
          ))}
        </div>
        {messageOf(problems, 'publishers') && <Msg tone="danger">{messageOf(problems, 'publishers')}</Msg>}
        <div className="hub-row">
          <Button
            size="s"
            appearance="ghost"
            onClick={() => change({ publishers: [...form.publishers, { name: '', key: '' }] })}
          >
            <Plus size={14} /> {t('network.pub.add')}
          </Button>
        </div>
        <Note>{t('network.pub.hint')}</Note>
      </section>

      {error && <Msg tone="danger">{error}</Msg>}
      {done && <Msg tone="success">{t('network.saved')}</Msg>}
      <div className="hub-row">
        <Button type="submit" loading={busy} disabled={!dirty}>
          {t('common.save')}
        </Button>
        <Button
          appearance="ghost"
          disabled={!dirty || busy}
          onClick={() => {
            setForm(saved);
            setProblems([]);
          }}
        >
          {t('network.reset')}
        </Button>
      </div>
    </form>
  );
};
