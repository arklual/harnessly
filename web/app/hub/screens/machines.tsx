// «Машины»: where the agents will work. A machine joins one of three ways (this one, a command, SSH); the way is
// chosen, then the isolation profile (before the command, because the one-time code carries it), then the machine
// shows up by itself when it dials in.
import { useEffect, useMemo, useState } from 'react';

import { Button, Spinner, TextField } from '@kit';

import { isApiError } from '../lib/api';
import { useApi } from '../lib/env';
import { errorText } from '../lib/errors';
import { hhmm } from '../lib/format';
import { useNow } from '../lib/hooks';
import { t, type Key } from '../lib/i18n';
import { useSession } from '../lib/session';
import { hostKeyFingerprint, sshFailure } from '../lib/ssh';
import type { Job, Machine, MachineCode, Profile } from '../lib/types';
import { StepFrame } from '../ui/frame';
import {
  Cpu,
  CircleCheck,
  Folder,
  HardDrive,
  Layers,
  KeyRound,
  Plug,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  Terminal,
  User,
} from '../ui/icons';
import { MachineRow } from '../ui/machine-row';
import {
  Card,
  Checks,
  Choice,
  CopyBox,
  type CheckRow,
  Label,
  Pending,
  Msg,
  Note,
  StepLine,
  Sure,
  Way,
  Waiting,
  type Tip,
} from '../ui/parts';
import { useGoStep, useSpace, useStepNav } from './space';

type WayId = 'local' | 'command' | 'ssh' | 'teleport' | 'ours';

const tip = (id: WayId): Tip => ({
  title: t(`machines.tip.${id}.t` as Key),
  lines: [1, 2, 3].map((n) => t(`machines.tip.${id}.${n}` as Key)),
});

const sshKeyNote = (owner: boolean, state: 'loading' | 'ready' | 'off' | 'failed'): Key => {
  if (!owner) return 'machines.ssh.ownersOnly';
  if (state === 'loading') return 'machines.ssh.keyLoading';
  return state === 'off' ? 'machines.ssh.noKey' : 'machines.ssh.keyFailed';
};

const sshFailureText = (failure: ReturnType<typeof sshFailure> | null): Key => {
  switch (failure) {
    case 'host_key_mismatch':
      return 'machines.ssh.mismatch';
    case 'ssh_blocked':
      return 'machines.ssh.blocked';
    case 'ssh_unreachable':
      return 'machines.ssh.unreachable';
    case 'ssh_auth':
      return 'machines.ssh.auth';
    default:
      return 'machines.ssh.failed';
  }
};

export const SSH_STEPS = ['ssh', 'preflight', 'install', 'online'] as const;

const stepDetail = (s: Job['steps'][number]): string | null => {
  if (s.id === 'ssh' && s.detail) return t('machines.ssh.fingerprint', { fp: s.detail });
  return s.state === 'fail' ? s.detail : null;
};

/** The progress rows of a job: the contract's step ids, the mockup's labels. */
export const jobRows = (job: Job, labels: (id: string) => string): CheckRow[] =>
  job.steps.map((s) => ({
    id: s.id,
    label: labels(s.id),
    state: s.state,
    detail: stepDetail(s),
  }));

export const MachinesScreen = () => {
  const api = useApi();
  const { config, me } = useSession();
  const { space, live, owner } = useSpace();
  const toStep = useStepNav();
  const goStep = useGoStep(`/s/${space.slug}/setup/machines`);
  const machines = live.state.machines;

  const localOff = !config.connect.local.available || !me?.user.isAdmin;
  const cloud = config.edition === 'cloud';
  const localOffSub = (() => {
    if (cloud) return t('machines.way.local.cloud');
    return config.connect.local.available ? t('machines.way.local.admin') : t('machines.way.local.none');
  })();
  const ways = useMemo(
    () => [
      {
        id: 'local' as const,
        icon: <HardDrive size={20} />,
        title: t('machines.way.local'),
        sub: t('machines.way.local.sub'),
        off: localOff,
        offSub: localOffSub,
      },
      {
        id: 'command' as const,
        icon: <Terminal size={20} />,
        title: t('machines.way.command'),
        sub: t('machines.way.command.sub'),
        off: false,
        offSub: '',
      },
      {
        id: 'ssh' as const,
        icon: <KeyRound size={20} />,
        title: t('machines.way.ssh'),
        sub: t('machines.way.ssh.sub'),
        off: !config.connect.ssh.available,
        offSub: t('machines.way.ssh.off'),
      },
      {
        id: 'teleport' as const,
        icon: <ShieldCheck size={20} />,
        title: t('machines.way.teleport'),
        sub: t('machines.way.teleport.sub'),
        off: true,
        offSub: t('machines.way.teleport.off'),
      },
      {
        id: 'ours' as const,
        icon: <Cpu size={20} />,
        title: t('machines.way.ours'),
        sub: t('machines.way.ours.sub'),
        off: true,
        offSub: cloud ? t('machines.way.ours.cloud') : t('machines.way.ours.self'),
      },
    ],
    [localOff, localOffSub, cloud, config.connect.ssh.available],
  );
  const hero = ways.find((w) => !w.off && (w.id === 'local' || w.id === 'command')) ?? ways[1];
  const [picked, setPicked] = useState<WayId | null>(null);
  const way: WayId = picked ?? hero?.id ?? 'command';
  const rows = ways.filter((w) => w !== hero).sort((a, b) => Number(a.off) - Number(b.off));

  const [profile, setProfile] = useState<Profile>('dir');
  const [sure, setSure] = useState(false);
  const [issued, setIssued] = useState<{ code: MachineCode; known: string[] } | null>(null);
  const [sshJob, setSshJob] = useState<{ id: string; known: string[] } | null>(null);
  const [busy, setBusy] = useState<null | 'code' | 'local' | 'ssh' | 'next'>(null);
  const [error, setError] = useState<string | null>(null);
  const [ssh, setSsh] = useState({ host: '', port: '22', user: '' });
  // The space's own key (GET …/machines/ssh, owners only): null until it is read.
  const [sshKey, setSshKey] = useState<{ key: string | null; state: 'loading' | 'ready' | 'off' | 'failed' }>(
    {
      key: null,
      state: 'loading',
    },
  );

  const ready = (machines ?? []).filter((m) => m.status === 'online');
  const choose = (id: WayId) => {
    setPicked(id);
    setIssued(null);
    setSshJob(null);
    setError(null);
  };
  const pickProfile = (p: Profile) => {
    // The code carries the profile: another choice means another code.
    setProfile(p);
    setSure(false);
    setIssued(null);
  };
  const canIssue = profile !== 'full' || sure;

  const issue = async () => {
    setBusy('code');
    setError(null);
    try {
      const code = await api.machines.issueCode(space.id, profile, profile === 'full');
      setIssued({ code, known: (machines ?? []).map((m) => m.id) });
    } catch (e) {
      setError(
        isApiError(e, 'provider_off') && profile === 'container' ? t('machines.containerOff') : errorText(e),
      );
    } finally {
      setBusy(null);
    }
  };

  const claimLocal = async () => {
    setBusy('local');
    setError(null);
    try {
      const { machine } = await api.machines.claimLocal(space.id);
      live.push({ type: 'event', event: { type: 'machine', data: { machine } } });
    } catch (e) {
      setError(isApiError(e, 'not_found') ? t('machines.localGone') : errorText(e));
    } finally {
      setBusy(null);
    }
  };

  useEffect(() => {
    if (way !== 'ssh' || !owner || !config.connect.ssh.available) return;
    let alive = true;
    api.machines
      .sshKey(space.id)
      .then(({ publicKey }) => alive && setSshKey({ key: publicKey, state: 'ready' }))
      .catch(
        (e: unknown) =>
          alive && setSshKey({ key: null, state: isApiError(e, 'provider_off') ? 'off' : 'failed' }),
      );
    return () => {
      alive = false;
    };
  }, [api, way, owner, space.id, config.connect.ssh.available]);

  // The first request only shakes hands and the job fails with the server's fingerprint; the owner compares it with the
  // server's own and the request goes again with it.
  const startSsh = async (hostKeyFingerprint?: string) => {
    const port = Number(ssh.port);
    if (!ssh.host.trim() || !ssh.user.trim() || !Number.isInteger(port) || port < 1 || port > 65535) {
      setError(t('machines.ssh.bad'));
      return;
    }
    setBusy('ssh');
    setError(null);
    try {
      const { jobId } = await api.machines.ssh(space.id, {
        host: ssh.host.trim(),
        port,
        user: ssh.user.trim(),
        profile,
        confirmFull: profile === 'full',
        ...(hostKeyFingerprint ? { hostKeyFingerprint } : {}),
      });
      setSshJob({ id: jobId, known: (machines ?? []).map((m) => m.id) });
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(null);
    }
  };

  const job = sshJob ? live.state.jobs[sshJob.id]?.job : undefined;
  const failure = job ? sshFailure(job) : null;
  const fingerprint = job ? hostKeyFingerprint(job) : null;
  const arrived = (known: string[]) => (machines ?? []).filter((m) => !known.includes(m.id));
  const cmdMachine = issued ? arrived(issued.known)[0] : undefined;
  const sshMachine = sshJob ? arrived(sshJob.known)[0] : undefined;
  const connectedNow =
    (way === 'command' && cmdMachine?.status === 'online') ||
    (way === 'ssh' && sshMachine?.status === 'online') ||
    (way === 'local' && ready.some((m) => m.via === 'local'));

  const another = () => {
    setPicked(null);
    setIssued(null);
    setSshJob(null);
    setSure(false);
    setError(null);
  };

  const next = async () => {
    setBusy('next');
    try {
      await toStep('agents');
    } catch (e) {
      setError(errorText(e));
      setBusy(null);
    }
  };

  return (
    <StepFrame
      step={2}
      icon={<Server size={22} />}
      title={t('machines.title')}
      lead={t('machines.lead')}
      goStep={goStep}
      foot={
        <>
          <button type="button" className="hub-link" onClick={() => goStep(1)}>
            {t('common.back')}
          </button>
          <span className="hub-spacer" />
          <Button
            appearance="primary"
            size="l"
            aria-disabled={ready.length === 0 || undefined}
            loading={busy === 'next'}
            onClick={() => ready.length > 0 && void next()}
          >
            {t('common.next')}
          </Button>
        </>
      }
    >
      {machines === null ? (
        <Pending error={live.error} onRetry={live.reload} rows={2} />
      ) : (
        machines.length > 0 && (
          <div className="hub-list" aria-label={t('machines.connected')}>
            {machines.map((m) => (
              <ConnectedMachine key={m.id} machine={m} />
            ))}
          </div>
        )
      )}

      {hero && (
        <Way
          hero
          icon={hero.icon}
          title={hero.title}
          sub={hero.sub}
          on={way === hero.id}
          tag={hero.id === 'local' ? t('machines.found') : undefined}
          tip={tip(hero.id)}
          onPick={() => choose(hero.id)}
        />
      )}
      <div className="hub-opts" role="group" aria-label={t('machines.others.aria')}>
        <span className="hub-opts__h">{t('machines.others')}</span>
        {rows.map((w) => (
          <Way
            key={w.id}
            icon={w.icon}
            title={w.title}
            sub={w.off ? w.offSub : w.sub}
            on={way === w.id && !w.off}
            off={w.off}
            tip={tip(w.id)}
            onPick={() => choose(w.id)}
          />
        ))}
      </div>

      {way === 'local' && (
        <Card>
          <div className="hub-item__head">
            <span className="hub-ic">
              <HardDrive size={18} />
            </span>
            <span className="hub-t">
              <span className="hub-t__n">{t('machines.way.local')}</span>
              <span className="hub-item__s">{t('machines.local.sub')}</span>
            </span>
            {connectedNow ? (
              <CircleCheck size={16} className="hub-ok" />
            ) : (
              <Button
                size="s"
                appearance="primary"
                loading={busy === 'local'}
                onClick={() => void claimLocal()}
              >
                {t('machines.connect')}
              </Button>
            )}
          </div>
          <Note>{t('machines.local.note')}</Note>
        </Card>
      )}

      {(way === 'command' || way === 'ssh') && (
        <ProfileCard
          profile={profile}
          sure={sure}
          container={config.profiles.container}
          onProfile={pickProfile}
          onSure={() => {
            setSure((v) => !v);
            setIssued(null);
          }}
        />
      )}

      {way === 'command' && (
        <CommandCard
          issued={issued?.code ?? null}
          machine={cmdMachine}
          canIssue={canIssue}
          busy={busy === 'code'}
          onIssue={() => void issue()}
        />
      )}

      {way === 'ssh' && (
        <Card>
          <div className="hub-grid2">
            <TextField
              size="m"
              label={t('machines.ssh.host')}
              className="hub-mono"
              placeholder="203.0.113.10"
              autoComplete="off"
              spellCheck={false}
              value={ssh.host}
              disabled={busy === 'ssh' || sshJob !== null}
              onChange={(e) => setSsh({ ...ssh, host: e.target.value })}
            />
            <TextField
              size="m"
              label={t('machines.ssh.user')}
              className="hub-mono"
              placeholder="ubuntu"
              autoComplete="off"
              spellCheck={false}
              value={ssh.user}
              disabled={busy === 'ssh' || sshJob !== null}
              onChange={(e) => setSsh({ ...ssh, user: e.target.value })}
            />
            <TextField
              size="m"
              label={t('machines.ssh.port')}
              className="hub-mono"
              inputMode="numeric"
              autoComplete="off"
              value={ssh.port}
              disabled={busy === 'ssh' || sshJob !== null}
              onChange={(e) => setSsh({ ...ssh, port: e.target.value.replace(/\D/g, '').slice(0, 5) })}
            />
          </div>
          <div className="hub-stack">
            <Label>{t('machines.ssh.addKey')}</Label>
            {sshKey.key ? (
              <CopyBox text={sshKey.key} label={t('machines.ssh.copyKey')} wrap />
            ) : (
              <Msg tone="warn">{t(sshKeyNote(owner, sshKey.state))}</Msg>
            )}
          </div>
          {!sshJob && (
            <Button
              appearance="primary"
              icon={<Plug size={16} />}
              loading={busy === 'ssh'}
              aria-disabled={!canIssue || !sshKey.key || undefined}
              onClick={() => canIssue && sshKey.key && void startSsh()}
            >
              {t('machines.ssh.go')}
            </Button>
          )}
          {sshJob && !job && <Waiting>{t('machines.ssh.starting')}</Waiting>}
          {job && <Checks rows={jobRows(job, (id) => t(`job.ssh.${id}` as Key))} />}
          {job?.state === 'failed' &&
            (failure === 'host_key_unconfirmed' && fingerprint ? (
              <div className="hub-stack">
                <Label>{t('machines.ssh.confirm.t')}</Label>
                <CopyBox text={fingerprint} label={t('machines.ssh.confirm.copy')} />
                <Note>{t('machines.ssh.confirm.note')}</Note>
                <div className="hub-row">
                  <Button
                    appearance="primary"
                    icon={<ShieldCheck size={16} />}
                    loading={busy === 'ssh'}
                    onClick={() => {
                      setSshJob(null);
                      void startSsh(fingerprint);
                    }}
                  >
                    {t('machines.ssh.confirm.go')}
                  </Button>
                  <Button size="s" onClick={() => setSshJob(null)}>
                    {t('common.cancel')}
                  </Button>
                </div>
              </div>
            ) : (
              <>
                <Msg tone="danger">{t(sshFailureText(failure))}</Msg>
                {failure !== 'ssh_blocked' && (
                  <Button size="s" icon={<RefreshCw size={14} />} onClick={() => setSshJob(null)}>
                    {t('common.retryAgain')}
                  </Button>
                )}
              </>
            ))}
        </Card>
      )}

      {error && <Msg tone="danger">{error}</Msg>}

      {connectedNow && (
        <button type="button" className="hub-add" onClick={another}>
          <Plus size={16} />
          {t('machines.addMore')}
        </button>
      )}
    </StepFrame>
  );
};

/** A machine of the list with its preflight line and the button to run the check again. */
const ConnectedMachine = ({ machine }: { machine: Machine }) => {
  const api = useApi();
  const { live } = useSpace();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const recheck = async () => {
    setBusy(true);
    setError(null);
    try {
      const { preflight } = await api.machines.preflight(machine.id);
      live.push({ type: 'event', event: { type: 'machine', data: { machine: { ...machine, preflight } } } });
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <MachineRow
        machine={machine}
        details
        actions={
          machine.status === 'online' && (
            <Button
              appearance="ghost"
              size="xs"
              className="k-btn--icon"
              aria-label={t('machines.recheck')}
              title={t('machines.recheck')}
              loading={busy}
              onClick={() => void recheck()}
            >
              <RefreshCw size={14} />
            </Button>
          )
        }
      />
      {error && <Msg tone="danger">{error}</Msg>}
    </>
  );
};

/** «Доступ агентов к машине»: the isolation profile; the full one needs an explicit «Понимаю и подтверждаю». */
const ProfileCard = ({
  profile,
  sure,
  container,
  onProfile,
  onSure,
}: {
  profile: Profile;
  sure: boolean;
  container: boolean;
  onProfile: (p: Profile) => void;
  onSure: () => void;
}) => (
  <Card>
    <div className="hub-stack">
      <span className="hub-label">{t('machines.iso.title')}</span>
      <Note>{t('machines.iso.later')}</Note>
    </div>
    <Choice
      icon={<Folder size={18} />}
      title={t('machines.iso.dir')}
      sub={t('machines.iso.dir.sub')}
      tag={t('machines.iso.recommended')}
      on={profile === 'dir'}
      onPick={() => onProfile('dir')}
    />
    {container && (
      <Choice
        icon={<Layers size={18} />}
        title={t('machines.iso.container')}
        sub={t('machines.iso.container.sub')}
        on={profile === 'container'}
        onPick={() => onProfile('container')}
      />
    )}
    <Choice
      icon={<User size={18} />}
      title={t('machines.iso.full')}
      sub={t('machines.iso.full.sub')}
      on={profile === 'full'}
      onPick={() => onProfile('full')}
    />
    {profile === 'full' && (
      <>
        <Note>{t('machines.iso.fullWarn')}</Note>
        <Sure on={sure} onToggle={onSure}>
          {t('machines.iso.sure')}
        </Sure>
      </>
    )}
  </Card>
);

/** One command to run on the machine; the machine appears below by itself when it dials in. */
const CommandCard = ({
  issued,
  machine,
  canIssue,
  busy,
  onIssue,
}: {
  issued: MachineCode | null;
  machine: Machine | undefined;
  canIssue: boolean;
  busy: boolean;
  onIssue: () => void;
}) => {
  const now = useNow(1000, issued !== null);
  if (!issued) {
    return (
      <Card>
        <Note>{t('machines.cmd.before')}</Note>
        <Button
          appearance="primary"
          icon={<Terminal size={16} />}
          loading={busy}
          aria-disabled={!canIssue || undefined}
          onClick={() => canIssue && onIssue()}
        >
          {t('machines.cmd.get')}
        </Button>
      </Card>
    );
  }
  const expired = new Date(issued.expiresAt).getTime() <= now;
  return (
    <Card>
      {machine ? (
        <Msg tone="success">{t('machines.cmd.used', { name: machine.name })}</Msg>
      ) : (
        <>
          <StepLine n={1}>{t('machines.cmd.run', { until: hhmm(issued.expiresAt) })}</StepLine>
          <CopyBox text={issued.command} label={t('machines.cmd.copy')} />
        </>
      )}
      <StepLine n={machine ? 1 : 2}>{t('machines.cmd.appears')}</StepLine>
      {expired && !machine ? (
        <Msg
          tone="warn"
          action={
            <button type="button" className="hub-link" onClick={onIssue}>
              {t('machines.cmd.renew')}
            </button>
          }
        >
          {t('machines.cmd.expired')}
        </Msg>
      ) : (
        <div className="hub-conn" data-ok={machine?.status === 'online' || undefined}>
          {!machine && <Waiting>{t('machines.cmd.waiting')}</Waiting>}
          {machine?.status === 'pending' && (
            <>
              <Spinner size="s" />
              <span>{t('machines.cmd.joining', { name: machine.name })}</span>
            </>
          )}
          {machine?.status === 'offline' && <span>{t('machines.cmd.lost', { name: machine.name })}</span>}
          {machine?.status === 'online' && (
            <>
              <CircleCheck size={16} />
              <span>
                <b>{machine.name}</b> {t('machines.cmd.online')}
              </span>
            </>
          )}
        </div>
      )}
    </Card>
  );
};
