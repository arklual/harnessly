// «Доступы»: Git hosts, an SSH key for Git, and free-form secrets, put on every machine that is online. Every value
// is sealed in the browser to each machine's own key (docs/hub/api.md, "Sealed envelope"): the hub relays ciphertext.
import { type ReactNode, useEffect, useState } from 'react';

import { Button, TextField } from '@kit';

import { GIT_ENV, envNameProblem, ownEnvNames } from '../lib/format';
import { useApi, useEnv } from '../lib/env';
import { errorText } from '../lib/errors';
import type { Live } from '../lib/hooks';
import { plural, t } from '../lib/i18n';
import { SealError } from '../lib/seal';
import type { Machine } from '../lib/types';
import { StepFrame } from '../ui/frame';
import { CircleCheck, GitBranch, GitPullRequest, KeyRound, Lock, Trash2 } from '../ui/icons';
import { CopyBox, Label, LinkButton, Msg, Note, Pending } from '../ui/parts';
import { useGoStep, useSpace, useStepNav } from './space';

type ItemId = 'github' | 'gitlab' | 'sshkey' | 'env';

interface Failure {
  machine: string;
  text: string;
}

/** Seals `value` to each machine and puts it there as `name`; answers the machines that refused. */
const useDeliver = (live: Live) => {
  const api = useApi();
  const env = useEnv();
  return async (machines: Machine[], pairs: { name: string; value: string }[]): Promise<Failure[]> => {
    const failures: Failure[] = [];
    for (const m of machines) {
      let current = m;
      for (const p of pairs) {
        try {
          const envelope = await env.seal(m.encKey, m.id, `env:${p.name}`, p.value);
          const { env: names } = await api.env.deliver(m.id, p.name, envelope);
          current = { ...current, env: names };
          live.push({ type: 'event', event: { type: 'machine', data: { machine: current } } });
        } catch (e) {
          failures.push({
            machine: m.name,
            text: e instanceof SealError ? t('access.cannotSeal') : errorText(e),
          });
          break;
        }
      }
    }
    return failures;
  };
};

export const AccessScreen = () => {
  const api = useApi();
  const env = useEnv();
  const { space, live } = useSpace();
  const toStep = useStepNav();
  const goStep = useGoStep(`/s/${space.slug}/setup/access`);
  const deliver = useDeliver(live);
  const machines = live.state.machines;
  const online = (machines ?? []).filter((m) => m.status === 'online');
  const targets = online.filter((m) => m.policy?.env !== false);
  const blocked = online.filter((m) => m.policy?.env === false);

  const [open, setOpen] = useState<ItemId | null>(null);
  const [busy, setBusy] = useState<ItemId | null>(null);
  const [failures, setFailures] = useState<Failure[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [sealable, setSealable] = useState<boolean | null>(null);
  const [form, setForm] = useState({ token: '', name: '', value: '' });
  const [nameError, setNameError] = useState<string | null>(null);
  const [gitKeys, setGitKeys] = useState<{ machine: Machine; key: string }[]>([]);
  const [removing, setRemoving] = useState<string | null>(null);
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    let alive = true;
    void env.canSeal().then((ok) => {
      if (alive) setSealable(ok);
    });
    return () => {
      alive = false;
    };
  }, [env]);

  const has = (name: string) => targets.filter((m) => m.env.includes(name)).length;
  const hostState = (token: string) => {
    const n = has(token);
    if (targets.length > 0 && n === targets.length) return 'on' as const;
    return n > 0 ? ('part' as const) : ('off' as const);
  };
  const secrets = [...new Set(online.flatMap((m) => m.env))].filter((n) => !ownEnvNames.includes(n)).sort();

  const toggle = (id: ItemId) => {
    setOpen((cur) => (cur === id ? null : id));
    setForm({ token: '', name: '', value: '' });
    setNameError(null);
    setFailures([]);
    setError(null);
  };

  const close = (done: Failure[]) => {
    setFailures(done);
    if (done.length === 0) setOpen(null);
  };

  const saveGit = async (id: 'github' | 'gitlab') => {
    const token = form.token.trim();
    if (!token) return;
    const pairs =
      id === 'github'
        ? GIT_ENV.github.map((name) => ({ name, value: token }))
        : [{ name: 'GITLAB_TOKEN', value: token }];
    setBusy(id);
    setError(null);
    try {
      close(await deliver(targets, pairs));
    } finally {
      setForm((f) => ({ ...f, token: '' }));
      setBusy(null);
    }
  };

  const saveEnv = async () => {
    const name = form.name.trim();
    const problem = envNameProblem(name);
    if (problem) {
      setNameError(t(problem === 'format' ? 'access.env.badName' : 'access.env.reserved'));
      return;
    }
    if (!form.value) return;
    setNameError(null);
    setBusy('env');
    try {
      close(await deliver(targets, [{ name, value: form.value }]));
    } finally {
      setForm((f) => ({ ...f, name: '', value: '' }));
      setBusy(null);
    }
  };

  const makeKeys = async () => {
    setBusy('sshkey');
    setError(null);
    const made: { machine: Machine; key: string }[] = [];
    const bad: Failure[] = [];
    for (const m of targets) {
      try {
        made.push({ machine: m, key: (await api.env.gitKey(m.id)).publicKey });
      } catch (e) {
        bad.push({ machine: m.name, text: errorText(e) });
      }
    }
    setGitKeys(made);
    setFailures(bad);
    setBusy(null);
  };

  const removeSecret = async (name: string) => {
    setRemoving(name);
    setError(null);
    try {
      for (const m of online.filter((x) => x.env.includes(name))) {
        const { env: names } = await api.env.remove(m.id, name);
        live.push({ type: 'event', event: { type: 'machine', data: { machine: { ...m, env: names } } } });
      }
    } catch (e) {
      setError(errorText(e));
    } finally {
      setRemoving(null);
    }
  };

  const next = async () => {
    setLeaving(true);
    try {
      await toStep('done');
    } catch (e) {
      setError(errorText(e));
      setLeaving(false);
    }
  };

  const item = (
    id: ItemId,
    icon: ReactNode,
    name: string,
    sub: string,
    cta: string,
    status: { on: boolean; label: string },
    body: ReactNode,
    save: () => void,
    canSave: boolean,
  ) => (
    <div className="hub-item">
      <div className="hub-item__head">
        <span className="hub-ic">{icon}</span>
        <span className="hub-t">
          <span className="hub-t__n">{name}</span>
          <span className="hub-item__s">{sub}</span>
        </span>
        {status.on && open !== id && (
          <>
            <span className="hub-item__st">{status.label}</span>
            <CircleCheck size={16} className="hub-ok" />
          </>
        )}
        {open !== id && (
          <Button size="xs" disabled={targets.length === 0} onClick={() => toggle(id)}>
            {status.on ? t('access.more') : cta}
          </Button>
        )}
      </div>
      {open === id && (
        <div className="hub-item__body">
          {sealable === false && id !== 'sshkey' && <Msg tone="warn">{t('access.cannotSeal')}</Msg>}
          {body}
          <div className="hub-row" data-end>
            <LinkButton onClick={() => toggle(id)}>{t('common.cancel')}</LinkButton>
            <Button
              size="s"
              appearance="primary"
              loading={busy === id}
              disabled={!canSave || (id !== 'sshkey' && sealable === false)}
              onClick={save}
            >
              {id === 'sshkey' ? t('access.create') : t('common.save')}
            </Button>
          </div>
        </div>
      )}
    </div>
  );

  const gitStatus = (token: string) => {
    const s = hostState(token);
    return {
      on: s !== 'off',
      label:
        s === 'part' ? t('access.partial', { n: has(token), total: targets.length }) : t('access.connected'),
    };
  };

  return (
    <StepFrame
      step={4}
      icon={<KeyRound size={22} />}
      title={t('access.title')}
      lead={t('access.lead')}
      goStep={goStep}
      foot={
        <>
          <button type="button" className="hub-link" onClick={() => goStep(3)}>
            {t('common.back')}
          </button>
          <span className="hub-spacer" />
          <button type="button" className="hub-link" onClick={() => void next()}>
            {t('common.skip')}
          </button>
          <Button appearance="primary" size="l" loading={leaving} onClick={() => void next()}>
            {t('common.next')}
          </Button>
        </>
      }
    >
      {machines === null ? (
        <Pending error={live.error} onRetry={live.reload} rows={4} />
      ) : (
        <>
          {online.length === 0 && <Msg tone="warn">{t('access.noMachines')}</Msg>}
          {blocked.length > 0 && (
            <Msg tone="warn">{t('access.policyOff', { names: blocked.map((m) => m.name).join(', ') })}</Msg>
          )}
          <div className="hub-list">
            {item(
              'github',
              <GitBranch size={18} />,
              'GitHub',
              t('access.github.sub'),
              t('access.connect'),
              gitStatus('GITHUB_TOKEN'),
              <TextField
                size="m"
                type="password"
                className="hub-mono"
                label={t('access.token')}
                placeholder="github_pat_…"
                autoComplete="off"
                value={form.token}
                onChange={(e) => setForm({ ...form, token: e.target.value })}
              />,
              () => void saveGit('github'),
              form.token.trim() !== '',
            )}
            {item(
              'gitlab',
              <GitPullRequest size={18} />,
              'GitLab',
              t('access.gitlab.sub'),
              t('access.connect'),
              gitStatus('GITLAB_TOKEN'),
              <>
                <Note>{t('access.gitlab.note')}</Note>
                <TextField
                  size="m"
                  type="password"
                  className="hub-mono"
                  label={t('access.token')}
                  placeholder="glpat-…"
                  autoComplete="off"
                  value={form.token}
                  onChange={(e) => setForm({ ...form, token: e.target.value })}
                />
              </>,
              () => void saveGit('gitlab'),
              form.token.trim() !== '',
            )}
            {item(
              'sshkey',
              <KeyRound size={18} />,
              t('access.sshkey'),
              t('access.sshkey.sub'),
              t('access.create'),
              { on: gitKeys.length > 0, label: t('access.created') },
              <Note>{t('access.sshkey.note')}</Note>,
              () => void makeKeys(),
              true,
            )}
            {gitKeys.length > 0 && (
              <div className="hub-card">
                <Note>{t('access.sshkey.add')}</Note>
                {gitKeys.map(({ machine, key }) => (
                  <div key={machine.id} className="hub-stack">
                    {gitKeys.length > 1 && <Label>{machine.name}</Label>}
                    <CopyBox text={key} label={t('access.sshkey.copy')} wrap />
                  </div>
                ))}
              </div>
            )}
            {item(
              'env',
              <Lock size={18} />,
              t('access.env'),
              t('access.env.sub'),
              t('access.add'),
              { on: secrets.length > 0, label: plural(secrets.length, 'access.vars') },
              <div className="hub-grid2">
                <TextField
                  size="m"
                  className="hub-mono"
                  label={t('access.env.name')}
                  placeholder="SENTRY_TOKEN"
                  autoComplete="off"
                  spellCheck={false}
                  value={form.name}
                  error={nameError ?? undefined}
                  onChange={(e) => setForm({ ...form, name: e.target.value.toUpperCase() })}
                />
                <TextField
                  size="m"
                  type="password"
                  className="hub-mono"
                  label={t('access.env.value')}
                  placeholder="••••••••"
                  autoComplete="off"
                  value={form.value}
                  onChange={(e) => setForm({ ...form, value: e.target.value })}
                />
              </div>,
              () => void saveEnv(),
              form.name.trim() !== '' && form.value !== '',
            )}
          </div>
          {secrets.length > 0 && (
            <div className="hub-stack">
              <span className="hub-label">{t('access.env.list')}</span>
              {secrets.map((name) => (
                <div key={name} className="hub-row" data-between>
                  <span className="hub-mono">{name}</span>
                  <Button
                    appearance="ghost"
                    size="xs"
                    className="k-btn--icon"
                    aria-label={t('access.env.remove', { name })}
                    title={t('access.env.remove', { name })}
                    loading={removing === name}
                    onClick={() => void removeSecret(name)}
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>
              ))}
            </div>
          )}
          {failures.map((f) => (
            <Msg key={f.machine} tone="danger">
              {t('access.failedOn', { machine: f.machine, text: f.text })}
            </Msg>
          ))}
          {error && <Msg tone="danger">{error}</Msg>}
        </>
      )}
    </StepFrame>
  );
};
