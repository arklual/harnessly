// «Агенты»: for each machine, which agents stand there and in what state; the missing ones are installed from
// here, then signed in (by link, in the console or with a key). When the provider's servers do not answer from the
// machine, the install stops at that step and offers «Повторить» or a proxy.
import { useEffect, useState } from 'react';

import { Button } from '@kit';

import { agentOf, isReady, PROVIDER_INFO, PROVIDERS, providerStepFailed, rowView } from '../lib/agent-view';
import { useApi } from '../lib/env';
import { errorText } from '../lib/errors';
import { checkAnswer, checkedAgo } from '../lib/format';
import { useNow, type Live } from '../lib/hooks';
import { t, type Key } from '../lib/i18n';
import { installJobOf } from '../lib/live';
import type { Job, Machine, Provider, ProviderCheck } from '../lib/types';
import { StepFrame } from '../ui/frame';
import { CircleCheck, Cpu, Download, LogIn, Server, TriangleAlert } from '../ui/icons';
import { Checks, ListSkeleton, LinkButton, Msg, Note, Pill } from '../ui/parts';
import { LoginPanel } from './agent-login';
import { ProxyBar, ProxyForm } from './agent-proxy';
import { jobRows } from './machines';
import { useGoStep, useSpace, useStepNav } from './space';

export const AgentMark = ({ provider, soon }: { provider: Provider; soon?: boolean }) => (
  <span className="hub-agent" data-agent={provider} data-soon={soon || undefined} aria-hidden="true">
    {PROVIDER_INFO[provider].name.charAt(0)}
  </span>
);

const policyAllows = (m: Machine, key: 'agentLogin' | 'keys' | 'install'): boolean =>
  m.policy?.[key] !== false;

export const AgentsScreen = () => {
  const { space, live } = useSpace();
  const toStep = useStepNav();
  const goStep = useGoStep(`/s/${space.slug}/setup/agents`);
  const machines = live.state.machines;
  const [pick, setPick] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const list = machines ?? [];
  const machine = list.find((m) => m.id === pick) ?? list.find((m) => m.status === 'online') ?? list[0];
  const anyReady = list.some((m) => m.agents.some(isReady));

  const next = async () => {
    setBusy(true);
    try {
      await toStep('access');
    } catch (e) {
      setError(errorText(e));
      setBusy(false);
    }
  };

  return (
    <StepFrame
      step={3}
      icon={<Cpu size={22} />}
      title={t('agents.title')}
      lead={t('agents.lead')}
      goStep={goStep}
      foot={
        <>
          <button type="button" className="hub-link" onClick={() => goStep(2)}>
            {t('common.back')}
          </button>
          <span className="hub-spacer" />
          {!anyReady && machines !== null && (
            <button type="button" className="hub-link" onClick={() => void next()}>
              {t('common.skip')}
            </button>
          )}
          <Button
            appearance="primary"
            size="l"
            aria-disabled={!anyReady || undefined}
            loading={busy}
            onClick={() => anyReady && void next()}
          >
            {t('common.next')}
          </Button>
        </>
      }
    >
      {machines === null &&
        (live.error ? <Msg tone="danger">{errorText(live.error)}</Msg> : <ListSkeleton rows={3} />)}
      {machines !== null && list.length === 0 && (
        <Msg tone="warn" action={<GoMachines />}>
          {t('agents.noMachines')}
        </Msg>
      )}
      {list.length > 1 && (
        <div className="hub-tabs" role="group" aria-label={t('agents.machine')}>
          {list.map((m) => (
            <button
              key={m.id}
              type="button"
              className="hub-tab"
              aria-pressed={m.id === machine?.id}
              onClick={() => setPick(m.id)}
            >
              <Server size={14} />
              {m.name}
            </button>
          ))}
        </div>
      )}
      {machine && <MachineAgents key={machine.id} machine={machine} live={live} />}
      {error && <Msg tone="danger">{error}</Msg>}
    </StepFrame>
  );
};

const GoMachines = () => {
  const { space } = useSpace();
  return (
    <a className="hub-link" href={`/s/${space.slug}/setup/machines`}>
      {t('agents.toMachines')}
    </a>
  );
};

/** The agents of one machine; asks the machine for fresh statuses once when opened. */
export const MachineAgents = ({ machine, live }: { machine: Machine; live: Live }) => {
  const api = useApi();
  const [loading, setLoading] = useState(machine.status === 'online');
  const [error, setError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (machine.status !== 'online') return;
    let alive = true;
    void api.agents
      .list(machine.id, true)
      .then(({ agents }) => {
        if (!alive) return;
        live.push({ type: 'event', event: { type: 'machine', data: { machine: { ...machine, agents } } } });
        setError(null);
        setLoading(false);
      })
      .catch((e: unknown) => {
        if (!alive) return;
        setError(errorText(e));
        setLoading(false);
      });
    return () => {
      alive = false;
    };
    // Asked once per opening (or retry), not on every change of the machine the answer itself causes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api, machine.id, machine.status, attempt]);

  if (machine.status !== 'online') {
    return <Msg tone="warn">{t('agents.offline', { name: machine.name })}</Msg>;
  }
  return (
    <>
      {error && (
        <Msg
          tone="danger"
          action={
            <LinkButton
              onClick={() => {
                setLoading(true);
                setAttempt((n) => n + 1);
              }}
            >
              {t('common.retry')}
            </LinkButton>
          }
        >
          {error}
        </Msg>
      )}
      {loading && machine.agents.length === 0 ? (
        <ListSkeleton rows={3} />
      ) : (
        <div className="hub-list">
          {PROVIDERS.map((p) => (
            <AgentRow key={p} machine={machine} provider={p} live={live} />
          ))}
        </div>
      )}
      <ProxyBar machine={machine} live={live} />
    </>
  );
};

type Panel = 'login' | 'none';

const installLabel = (id: string, provider: Provider, machine: Machine): string =>
  t(`job.agent.${id}` as Key, {
    vendor: PROVIDER_INFO[provider].vendor,
    name: PROVIDER_INFO[provider].name,
    machine: machine.name,
  });

const AgentRow = ({ machine, provider, live }: { machine: Machine; provider: Provider; live: Live }) => {
  const api = useApi();
  const info = PROVIDER_INFO[provider];
  const status = agentOf(machine, provider);
  const [started, setStarted] = useState<string | null>(null);
  const [panel, setPanel] = useState<Panel | null>(null);
  const [checked, setChecked] = useState<ProviderCheck | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const streamed = started
    ? (live.state.jobs[started]?.job ?? null)
    : installJobOf(live.state, machine.id, provider);
  const starting = started !== null && !live.state.jobs[started];
  const job: Job | null = streamed;
  const view = rowView(provider, status, job, starting);
  const soon = view.kind === 'soon';
  const providerFailed = view.kind === 'failed' && providerStepFailed(job);
  // After an install the sign-in opens by itself; the person's own choice (open or closed) wins over that.
  const autoLogin = view.kind === 'needsLogin' && job?.state === 'done';
  const shown: Panel = panel ?? (autoLogin ? 'login' : 'none');
  const installAllowed = policyAllows(machine, 'install');

  const install = async (from?: 'provider') => {
    setBusy(true);
    setError(null);
    setPanel(null);
    setChecked(null);
    try {
      const { jobId } = await api.agents.install(machine.id, provider, from);
      setStarted(jobId);
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const recheck = async () => {
    setBusy(true);
    setError(null);
    try {
      const { agents } = await api.agents.list(machine.id, true);
      live.push({ type: 'event', event: { type: 'machine', data: { machine: { ...machine, agents } } } });
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };

  const LABELS: Partial<Record<typeof view.kind, Key>> = {
    installing: 'agents.installing',
    failed: 'agents.noAccess',
    needsLogin: 'agents.needsLogin',
  };
  const labelKey = LABELS[view.kind];
  const label = labelKey ? t(labelKey) : '';
  const readySub = [t('agents.sub.ready'), status?.authMode ? t(`agents.mode.${status.authMode}` as Key) : '']
    .filter(Boolean)
    .join(' · ');
  const installedSub = `${t('agents.sub.installed')}${status?.version ? ` ${status.version}` : ''}`;
  const SUBS: Record<typeof view.kind, string> = {
    soon: t('agents.sub.soon'),
    ready: readySub,
    missing: t('agents.sub.missing'),
    installing: t('agents.sub.missing'),
    failed: t('agents.sub.missing'),
    needsLogin: installedSub,
  };
  const subText = SUBS[view.kind];
  const sub = `${info.vendor} · ${subText}`;

  return (
    <div className="hub-item">
      <div className="hub-item__head">
        <AgentMark provider={provider} soon={soon} />
        <span className="hub-t">
          <span className="hub-t__n">{info.name}</span>
          <span className="hub-item__s">{sub}</span>
        </span>
        {label && (
          <span className="hub-item__st" data-tone={view.kind === 'failed' ? 'warn' : undefined}>
            {label}
          </span>
        )}
        {soon && <Pill tone="muted">{t('agents.soon')}</Pill>}
        {(view.kind === 'missing' || view.kind === 'failed') && (
          <Button
            size="xs"
            icon={<Download size={14} />}
            loading={busy}
            disabled={!installAllowed}
            onClick={() => void install()}
          >
            {t('agents.install')}
          </Button>
        )}
        {view.kind === 'needsLogin' && shown !== 'login' && (
          <Button size="xs" icon={<LogIn size={14} />} onClick={() => setPanel('login')}>
            {t('agents.login')}
          </Button>
        )}
        {view.kind === 'needsLogin' && view.unknown && (
          <LinkButton onClick={() => void recheck()} disabled={busy}>
            {t('agents.recheck')}
          </LinkButton>
        )}
        {view.kind === 'ready' && <CircleCheck size={16} className="hub-ok" />}
      </div>
      {view.kind === 'ready' && status?.overriddenBy && (
        <div className="hub-item__sub" data-tone="warn">
          {t('agents.overridden', { name: status.overriddenBy })}
        </div>
      )}
      {view.kind === 'missing' && !installAllowed && (
        <div className="hub-item__sub" data-tone="warn">
          {t('agents.installPolicyOff')}
        </div>
      )}
      {error && <Msg tone="danger">{error}</Msg>}

      {(view.kind === 'installing' || view.kind === 'failed') && (job || starting) && (
        <div className="hub-item__body">
          {job ? (
            <Checks rows={jobRows(job, (id) => installLabel(id, provider, machine))} />
          ) : (
            <Note>{t('agents.install.starting')}</Note>
          )}
          {job?.state === 'failed' &&
            (providerFailed ? (
              <ProviderFail
                machine={machine}
                provider={provider}
                live={live}
                check={newest(checked, job.check)}
                onChecked={setChecked}
                onRetry={() => void install('provider')}
                busy={busy}
              />
            ) : (
              <Msg
                tone="danger"
                action={
                  <LinkButton onClick={() => void install()} disabled={busy}>
                    {t('common.retryAgain')}
                  </LinkButton>
                }
              >
                {job.error?.message || t('agents.install.failed')}
              </Msg>
            ))}
        </div>
      )}

      {shown === 'login' && view.kind === 'needsLogin' && (
        <LoginPanel
          machine={machine}
          provider={provider}
          live={live}
          allowed={{ login: policyAllows(machine, 'agentLogin'), keys: policyAllows(machine, 'keys') }}
        />
      )}
    </div>
  );
};

const newest = (a: ProviderCheck | null, b: ProviderCheck | null): ProviderCheck | null =>
  a && (!b || a.at >= b.at) ? a : b;

/** «Серверы … не отвечают»: what was tried, the answer, and the two ways on. */
const ProviderFail = ({
  machine,
  provider,
  live,
  check,
  onChecked,
  onRetry,
  busy,
}: {
  machine: Machine;
  provider: Provider;
  live: Live;
  check: ProviderCheck | null;
  onChecked: (c: ProviderCheck) => void;
  onRetry: () => void;
  busy: boolean;
}) => {
  const info = PROVIDER_INFO[provider];
  const now = useNow(30_000, check !== null);
  const [proxy, setProxy] = useState(false);
  const [more, setMore] = useState(false);
  return (
    <>
      <div className="hub-fail">
        <div className="hub-fail__h">
          <span className="hub-fail__ic">
            <TriangleAlert size={18} />
          </span>
          <span className="hub-fail__t">
            <b>{t('fail.title', { vendor: info.vendor })}</b>
            <span className="hub-fail__s">{t('fail.sub', { machine: machine.name })}</span>
          </span>
        </div>
        {check && (
          <dl className="hub-dl">
            <div>
              <dt>{t('fail.server')}</dt>
              <dd className="hub-mono">{check.host}</dd>
            </div>
            <div>
              <dt>{t('fail.answer')}</dt>
              <dd className="hub-mono">{checkAnswer(check)}</dd>
            </div>
            <div>
              <dt>{t('fail.checked')}</dt>
              <dd>
                {checkedAgo(check.at, now)}, {check.via === 'proxy' ? t('fail.viaProxy') : t('fail.direct')}
              </dd>
            </div>
          </dl>
        )}
        {check && check.hosts.length > 0 && (
          <>
            <LinkButton onClick={() => setMore((v) => !v)}>
              {more ? t('fail.less') : t('fail.more')}
            </LinkButton>
            {more && (
              <ul className="hub-hosts">
                {check.hosts.map((h) => (
                  <li key={h.host}>
                    <span className="hub-mono">{h.host}</span>
                    <span>
                      <span data-bad={!h.dns || undefined}>DNS {h.dns ? '✓' : '✗'}</span>{' '}
                      <span data-bad={!h.tcp || undefined}>TCP {h.tcp ? '✓' : '✗'}</span>{' '}
                      <span data-bad={!h.tls || undefined}>TLS {h.tls ? '✓' : '✗'}</span> <em>{h.ms} мс</em>
                      {h.error && <em> · {h.error}</em>}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </>
        )}
        <Note>{t('fail.note')}</Note>
      </div>
      {!proxy && (
        <div className="hub-row" data-end>
          <Button size="s" loading={busy} onClick={onRetry}>
            {t('fail.retry')}
          </Button>
          <Button size="s" appearance="primary" onClick={() => setProxy(true)}>
            {t('fail.setProxy')}
          </Button>
        </div>
      )}
      {proxy && (
        <ProxyForm
          machine={machine}
          live={live}
          scopes={provider === 'gemini' ? ['all'] : ['all', provider]}
          onCancel={() => setProxy(false)}
          onDone={(checks) => {
            const mine = checks.find((c) => c.provider === provider);
            if (!mine) return;
            if (mine.verdict === 'ok') {
              setProxy(false);
              onRetry();
            } else onChecked(mine);
          }}
        />
      )}
    </>
  );
};
