// The Settings screen (the design's «Настройки»): the person's and the machine's settings in eight sections. Each page
// works on what is really there — the person's settings record, the CLIs' own files on the machine, the fleet's list,
// the apps the machine reads — and says plainly what it cannot change from here.
import { useEffect, useState } from 'react';
import {
  AgentPage,
  AccountsPage,
  PROVIDER_LIST,
  SkillsPage,
  useAgentPages,
} from '@/next/bindings/settings-agents';
import { AppsPage, BetaPage, GeneralPage, MachinesPage } from '@/next/bindings/settings-general';
import { NoticesPage } from '@/next/bindings/settings-notices';
import { PeersPage } from '@/next/bindings/settings-peers';
import SettingsShell from '@/next/features/settings/settings-shell';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE, type SettingsSection } from '@/next/routes/routes';
import { useMachine } from '@/next/stores/machines';
import usePendingList from '@/next/ui/use-pending-list';

/** The «x minutes ago» of the machines list moves on with this clock. */
const CLOCK_MS = 60_000;

const useClock = () => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), CLOCK_MS);
    return () => clearInterval(timer);
  }, []);
  return now;
};

interface ISettingsScreenProps {
  /** The machine whose files and apps the pages are about: the one the person has open. */
  machine: MachineId;
  /** The section in the address; null: the list (a wide window shows the first section beside it). */
  section: SettingsSection | null;
  /** The name the sidebar shows for the person. */
  name: string;
  onOpenDrawer: () => void;
}

const SettingsScreen = ({ machine, section, name, onOpenDrawer }: ISettingsScreenProps) => {
  const active = section ?? 'general';
  const view = useMachine(machine);
  const now = useClock();
  const pending = usePendingList();
  const pages = useAgentPages(machine, {
    claude: active === 'claude' || active === 'accounts' || active === 'skills',
    codex: active === 'codex' || active === 'accounts' || active === 'skills',
  });

  const machineName = view?.machine.name ?? machine;
  const modelsOf = (provider: (typeof PROVIDER_LIST)[number]) =>
    view?.machine.agents.find((agent) => agent.provider === provider)?.models ?? [];
  const path = (provider: (typeof PROVIDER_LIST)[number]) => {
    const state = pages[provider].state;
    if (state.k === 'ready') return state.value.path;
    return provider === 'claude' ? '~/.claude/settings.json' : '~/.codex/config.toml';
  };

  let lead = t(`next.settings.lead.${active}`);
  if (active === 'claude' || active === 'codex')
    lead = t(`next.settings.lead.${active}`, { machine: machineName, path: path(active) });

  let body = null;
  if (active === 'general') body = <GeneralPage machine={machine} name={name} pending={pending} />;
  else if (active === 'notifications') body = <NoticesPage />;
  else if (active === 'accounts') body = <AccountsPage machine={machine} pages={pages} pending={pending} />;
  else if (active === 'claude' || active === 'codex')
    body = (
      <AgentPage
        key={active}
        provider={active}
        handle={pages[active]}
        models={modelsOf(active)}
        pending={pending}
      />
    );
  else if (active === 'skills') body = <SkillsPage machine={machine} pages={pages} pending={pending} />;
  else if (active === 'machines')
    body = (
      <>
        <MachinesPage now={now} pending={pending} />
        <PeersPage machine={machine} now={now} pending={pending} />
      </>
    );
  else if (active === 'beta') body = <BetaPage />;
  else body = <AppsPage machine={machine} />;

  return (
    <SettingsShell
      section={active}
      pageOpen={section !== null}
      lead={lead}
      onPick={(next) => goTo({ kind: ROUTE.settings, section: next })}
      onBack={() => goTo({ kind: ROUTE.settings, section: null })}
      onOpenDrawer={onOpenDrawer}
    >
      {body}
    </SettingsShell>
  );
};

export default SettingsScreen;
