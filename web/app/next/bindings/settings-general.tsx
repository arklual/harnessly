// The Settings pages that are about the person and the fleet: «General», «Machines», «Apps».
import { useAppsFeed, useSignIn } from '@/next/bindings/settings-data';
import type { Pending } from '@/next/bindings/settings-agents';
import AppsSection from '@/next/features/settings/apps-section';
import BetaSection from '@/next/features/settings/beta-section';
import GeneralSection from '@/next/features/settings/general-section';
import UpdateGroup from '@/next/features/settings/update-group';
import ChecksGroup from '@/next/features/settings/checks-group';
import {
  browserChecks,
  fixMachineCheck,
  loadMachineChecks,
  type IMachineCheck,
} from '@/next/features/settings/machine-checks';
import { applyMachineUpdate, loadUpdateInfo, type TUpdateInfo } from '@/next/features/update/machine-update';
import { machineRows } from '@/next/features/settings/machines';
import MachinesSection from '@/next/features/settings/machines-section';
import { loadRuntimeSettings, saveRuntimeSettings, signOut } from '@/next/features/settings/api';
import { useEffect, useState } from 'react';
import { locale, t } from '@/next/i18n';
import { pageText } from '@/next/bindings/outcome';
import {
  DEFAULT_DELEGATION,
  type DelegationSettings,
  type MachineId,
  type RuntimeSettings,
} from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { bootNext, shutdownNext } from '@/next/boot';
import { appConfig } from '@/platform/config';
import { hasFeature, useMachines } from '@/next/stores/machines';
import { useDeadline } from '@/next/ui/use-deadline';
import {
  DEFAULT_SETTINGS,
  patchSettings,
  useMe,
  type PatchResult,
  type PersonSettings,
} from '@/next/stores/me';
import { toast } from '@/next/ui/toast';
import { setTopTabs, useTopTabs } from '@/next/stores/tabs';
import { setBeta, useBeta } from '@/next/stores/beta';
import { setPaneAuto, usePaneAuto } from '@/next/stores/pane-auto';
import { setFeedKind, useFeedKinds } from '@/next/stores/feed-kinds';
import { useTheme } from '@/platform/theme';

/** The words for a change of the person's settings that did not go through. */
const patchError = (res: Exclude<PatchResult, { r: 'accepted' }>): string =>
  res.r === 'refused' ? t(`next.refuse.${res.code}`) : t('next.failure.network');

/** A change of the person's settings: resolves when the server took it, rejects with the reason when it did not. */
const savePerson = async (patch: Partial<PersonSettings>): Promise<void> => {
  const res = await patchSettings(patch);
  if (res.r !== 'accepted') throw new Error(patchError(res));
};

/** The runtime's own settings of the machine: null until it says, and on a runtime that has none of them. */
const useRuntimeSettings = (machine: MachineId) => {
  const supported = hasFeature(machine, 'terminal-stop');
  // Kept with the machine it came from: another machine's value is not shown while this one's is read.
  const [read, setRead] = useState<{ machine: MachineId; value: RuntimeSettings } | null>(null);
  useEffect(() => {
    if (!supported) return;
    let live = true;
    void loadRuntimeSettings(machine).then((res) => {
      if (live && 'ok' in res) setRead({ machine, value: res.value });
    });
    return () => {
      live = false;
    };
  }, [machine, supported]);
  const value = supported && read?.machine === machine ? read.value : null;
  // Pessimistic: the row moves when the runtime has kept the new value. It is asked for the whole as it was read with
  // the one change; the settings it does not name are the runtime's to keep.
  const save = async (patch: Partial<RuntimeSettings>): Promise<void> => {
    if (!value) return;
    const res = await saveRuntimeSettings(machine, { ...value, ...patch });
    if (!('ok' in res)) throw new Error(pageText(res));
    setRead({ machine, value: res.value });
  };
  return { value, save };
};

/** «Обновления»: null until the runtime says, and on a runtime with nothing to tell. */
const useUpdateInfo = (machine: MachineId) => {
  const [read, setRead] = useState<{ machine: MachineId; value: TUpdateInfo } | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    void loadUpdateInfo(machine).then((value) => {
      if (live && value) setRead({ machine, value });
    });
    return () => {
      live = false;
    };
  }, [machine]);
  const value = read?.machine === machine ? read.value : null;
  const apply = async () => {
    setBusy(true);
    const next = await applyMachineUpdate(machine);
    setBusy(false);
    if (!next) {
      toast(t('next.settings.update.noAnswer'), { tone: 'error' });
      return;
    }
    const before = value?.kind === 'hub' ? value.machine : null;
    setRead({ machine, value: { kind: 'hub', machine: next } });
    // Programs were replaced: the runtime restarts by itself and the page comes back to it.
    if (before && before.checked && !before.upToDate && next.upToDate && !next.error)
      toast(t('next.settings.update.done'));
  };
  const copy = () => {
    if (value?.kind !== 'hub') return;
    void navigator.clipboard.writeText(value.machine.command).then(
      () => toast(t('next.settings.update.copied')),
      () => toast(t('next.shellData.noClipboard'), { tone: 'error' }),
    );
  };
  return { value, busy, apply, copy };
};

/** «Проверка машины»: the machine's checks and this browser's, read when the page opens; empty until they come. */
const useMachineChecks = (machine: MachineId) => {
  const [read, setRead] = useState<{ machine: MachineId; value: IMachineCheck[] } | null>(null);
  const [browser, setBrowser] = useState<IMachineCheck[]>([]);
  const [fixing, setFixing] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    void loadMachineChecks(machine).then((value) => {
      if (live && value) setRead({ machine, value });
    });
    void browserChecks().then((value) => {
      if (live) setBrowser(value);
    });
    return () => {
      live = false;
    };
  }, [machine]);
  const checks = [...(read?.machine === machine ? read.value : []), ...browser];
  // Pessimistic: the row goes when the machine says the check passes now.
  const fix = async (id: string) => {
    setFixing(id);
    const next = await fixMachineCheck(machine, id);
    setFixing(null);
    if (!next) {
      toast(t('next.settings.checks.fixFailed'), { tone: 'error' });
      return;
    }
    setRead({ machine, value: next });
    if (next.find((c) => c.id === id)?.ok) toast(t('next.settings.checks.fixed'));
  };
  const copy = (command: string) => {
    void navigator.clipboard.writeText(command).then(
      () => toast(t('next.settings.checks.copied')),
      () => toast(t('next.shellData.noClipboard'), { tone: 'error' }),
    );
  };
  return { checks, fixing, fix, copy };
};

interface IGeneralPageProps {
  machine: MachineId;
  /** The name the sidebar shows for the person. */
  name: string;
  pending: Pending;
}

export const GeneralPage = ({ machine, name, pending }: IGeneralPageProps) => {
  const me = useMe();
  const { theme, setTheme } = useTheme();
  const password = useSignIn(machine);
  const topTabs = useTopTabs();
  const paneAuto = usePaneAuto();
  const feedKinds = useFeedKinds();
  const runtime = useRuntimeSettings(machine);
  const update = useUpdateInfo(machine);
  const machineChecks = useMachineChecks(machine);
  const settings = me.k === 'ready' ? me.me.settings : DEFAULT_SETTINGS;
  const email = me.k === 'ready' ? me.me.email : null;
  const { run, pending: waiting } = pending;
  const failure = t('next.settings.failed');
  const hubUrl = appConfig().hubUrl;

  const hint = email ?? (password ? t('next.settings.general.byPassword') : null);
  return (
    <GeneralSection
      theme={theme}
      textSize={settings.textSize}
      enterSends={settings.enterSends}
      topTabs={topTabs}
      paneAuto={paneAuto}
      feedKinds={feedKinds}
      language={locale()}
      account={{ name, hint, canSignOut: password || hubUrl !== null }}
      idleStop={runtime.value?.idleStop ?? null}
      limitContinue={runtime.value?.limitContinue ?? null}
      restartContinue={runtime.value?.restartContinue ?? null}
      delegation={runtime.value ? (runtime.value.delegation ?? DEFAULT_DELEGATION) : null}
      busy={new Set(waiting.keys())}
      onTheme={setTheme}
      onTextSize={(textSize) => void run('textSize', failure, () => savePerson({ textSize }))}
      onEnterSends={(enterSends) => void run('enterSends', failure, () => savePerson({ enterSends }))}
      onTopTabs={setTopTabs}
      onPaneAuto={setPaneAuto}
      onFeedKind={setFeedKind}
      onIdleStop={(idleStop) => void run('idleStop', failure, () => runtime.save({ idleStop }))}
      onLimitContinue={(on) =>
        void run('limitContinue', failure, () => runtime.save({ limitContinue: on ? 'on' : 'off' }))
      }
      onRestartContinue={(on) =>
        void run('restartContinue', failure, () => runtime.save({ restartContinue: on ? 'on' : 'off' }))
      }
      onDelegation={(delegation: DelegationSettings) =>
        void run('delegation', failure, () => runtime.save({ delegation }))
      }
      updates={
        update.value ? (
          <UpdateGroup
            info={update.value}
            busy={update.busy}
            onUpdate={() => void update.apply()}
            onCopy={update.copy}
          />
        ) : null
      }
      checks={
        <ChecksGroup
          checks={machineChecks.checks}
          fixing={machineChecks.fixing}
          onCopy={machineChecks.copy}
          onFix={(id) => void machineChecks.fix(id)}
        />
      }
      onSignOut={() => {
        void signOut(machine).then((res) => {
          if ('ok' in res) {
            // Behind a hub the session is the hub's: signing out lands on its sign-in page.
            if (hubUrl) window.location.assign(`${hubUrl}/signin`);
            else window.location.reload();
          } else toast(t('next.settings.general.signOutFailed'), { tone: 'error' });
        });
      }}
    />
  );
};

interface IMachinesPageProps {
  now: number;
  pending: Pending;
}

export const MachinesPage = ({ now, pending }: IMachinesPageProps) => {
  const me = useMe();
  const machines = useMachines();
  const { run, pending: waiting } = pending;
  const defaultMachine = me.k === 'ready' ? me.me.settings.defaultMachine : null;
  const rows = machines === null ? null : machineRows(machines, defaultMachine, now);
  // The fleet comes with the start; a start that does not finish says so with a retry instead of «Загрузка» for ever.
  const wait = useDeadline(rows === null);
  const failed = wait.expired
    ? {
        reason: t('next.load.noAnswer'),
        onRetry: () => shutdownNext().then(() => bootNext()),
      }
    : null;
  return (
    <MachinesSection
      rows={rows}
      failed={failed}
      busy={new Set(waiting.keys())}
      onMakeMain={(id) => void run(id, t('next.settings.failed'), () => savePerson({ defaultMachine: id }))}
    />
  );
};

export const AppsPage = ({ machine }: { machine: MachineId }) => {
  const feed = useAppsFeed(machine, true);
  let error: string | null = null;
  if (!hasFeature(machine, 'apps-hub')) error = t('next.apps.unsupported');
  else if (feed.state.k === 'failed') error = feed.state.text;
  return (
    <AppsSection
      feed={feed.state.k === 'ready' ? feed.state.value : null}
      error={error}
      onOpen={() => goTo({ kind: ROUTE.apps })}
      onRetry={feed.retry}
    />
  );
};

export const BetaPage = () => <BetaSection flags={useBeta()} onChange={setBeta} />;
