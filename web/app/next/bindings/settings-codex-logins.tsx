// The other sign-ins of the Codex CLI on the Codex page of Settings: the list as the machine keeps it, and for each the
// login that runs in its own home. A change goes to the machine first; the list shown is the one it answers with.
import { useCallback, useEffect, useState } from 'react';
import { refusalOf } from '@/next/bindings/settings-changes';
import { useMachineLimits } from '@/next/bindings/use-limits';
import { useLoginFlow } from '@/next/bindings/settings-credentials';
import LoginPanel from '@/next/features/accounts/login-panel';
import {
  addCodexLogin,
  loadCodexLogins,
  removeCodexLogin,
  renameCodexLogin,
  setCodexLoginMain,
  type ICodexLogin,
  type ICodexLogins,
} from '@/next/features/settings/codex-logins-api';
import CodexLoginsGroup, { CodexLoginCard } from '@/next/features/settings/codex-logins-group';
import LimitsLine from '@/next/features/limits/limits-line';
import { profileLimitsOf, type MachineLimits } from '@/next/features/limits/model';
import { isRefused, type TAnswerOutcome } from '@/next/features/settings/platform-answer';
import { t } from '@/next/i18n';
import type { AgentAccount, MachineId } from '@/next/protocol/v2';
import { noteCodexMain } from '@/next/stores/codex-main';

const accountOf = (login: ICodexLogin): AgentAccount =>
  login.account === ''
    ? { signedIn: false, method: null, plan: null, who: null }
    : { signedIn: true, method: 'subscription', plan: null, who: login.account };

interface ICodexLoginRowProps {
  machine: MachineId;
  login: ICodexLogin;
  /** The machine's limits; null while it has not said. */
  limits: MachineLimits | null;
  reload: () => Promise<void>;
}

const CodexLoginRow = ({ machine, login, limits, reload }: ICodexLoginRowProps) => {
  const onChanged = useCallback(() => void reload(), [reload]);
  const flow = useLoginFlow(machine, 'codex', login.id, onChanged);
  const change = async <T,>(res: Promise<TAnswerOutcome<T>>) => {
    const got = await res;
    const refused = refusalOf(got);
    if (refused !== null) return refused;
    if (!('ok' in got) || isRefused(got.value)) return t('next.settings.failed');
    await reload();
    return null;
  };
  return (
    <CodexLoginCard
      login={login}
      onRename={(name) => change(renameCodexLogin(machine, login.id, name))}
      onRemove={() => change(removeCodexLogin(machine, login.id))}
      onMain={(main) => change(setCodexLoginMain(machine, login.id, main))}
      onLogout={flow.onLogout}
      left={
        limits !== null && login.account !== '' ? (
          <LimitsLine limits={profileLimitsOf(limits, login.profile)} />
        ) : undefined
      }
    >
      <LoginPanel
        via="subscription"
        account={accountOf(login)}
        login={flow.login}
        onLogin={flow.onLogin}
        onSubmitCode={flow.onSubmitCode}
        onCancel={flow.onCancelLogin}
        onLogout={flow.onLogout}
      />
    </CodexLoginCard>
  );
};

export const CodexLogins = ({ machine }: { machine: MachineId }) => {
  const limits = useMachineLimits(machine);
  // null: not read yet (or the machine has no such list: an older runtime). Nothing is drawn until it is read.
  const [box, setBox] = useState<{ machine: MachineId; got: ICodexLogins } | null>(null);

  const reload = useCallback(async () => {
    const res = await loadCodexLogins(machine);
    if ('ok' in res && !isRefused(res.value)) {
      setBox({ machine, got: res.value });
      noteCodexMain(machine, res.value.logins.find((l) => l.main)?.profile ?? null);
    }
  }, [machine]);

  useEffect(() => {
    let live = true;
    void loadCodexLogins(machine).then((res) => {
      if (live && 'ok' in res && !isRefused(res.value)) {
        setBox({ machine, got: res.value });
        noteCodexMain(machine, res.value.logins.find((l) => l.main)?.profile ?? null);
      }
    });
    return () => {
      live = false;
    };
  }, [machine]);

  const got = box !== null && box.machine === machine ? box.got : null;
  return (
    <CodexLoginsGroup
      logins={got === null ? null : got.logins}
      full={got !== null && got.logins.length >= got.max}
      onAdd={async (name) => {
        const res = await addCodexLogin(machine, name);
        const refused = refusalOf(res);
        if (refused !== null) return refused;
        if (!('ok' in res) || isRefused(res.value)) return t('next.settings.failed');
        await reload();
        return null;
      }}
      renderLogin={(login) => (
        <CodexLoginRow key={login.id} machine={machine} login={login} limits={limits} reload={reload} />
      )}
    />
  );
};
