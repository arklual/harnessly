// The Settings pages that are about the agent CLIs on the machine: one CLI's own page, «Models and keys», and
// «Skills and MCP». They read the CLIs' files through the runtime and write one change at a time; a row shows the old
// value until the machine has taken the new one.
import { useState } from 'react';
import {
  useAgentSettings,
  useCatalog,
  type IAgentSettingsHandle,
  type Loaded,
} from '@/next/bindings/settings-data';
import AccountsSection, { type AccountCard } from '@/next/features/settings/accounts-section';
import AgentSection from '@/next/features/settings/agent-section';
import { EnvGroup, RulesGroup } from '@/next/features/settings/rules-groups';
import {
  EMPTY_RULES,
  skillOn,
  skillRule,
  withRule,
  withoutRule,
  type RuleKind,
} from '@/next/features/settings/rules';
import { Note } from '@/next/features/settings/rows';
import SkillsSection, {
  type ExtFilter,
  type ExtList,
  type ExtTab,
  type IExtension,
} from '@/next/features/settings/skills-section';
import { t } from '@/next/i18n';
import type {
  AgentModel,
  AgentOptionKey,
  AgentOptionValue,
  AgentSettings,
  Provider,
} from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import usePendingList from '@/next/ui/use-pending-list';
import { Skeleton } from '@/next/ui/skeleton';
import Swap from '@/next/ui/swap';

export type Pending = ReturnType<typeof usePendingList>;
export const PROVIDER_LIST = ['claude', 'codex'] as const satisfies readonly Provider[];

/** The settings of both CLIs; each is read only while a page that needs it is open. */
export const useAgentPages = (machine: string, needs: Readonly<Record<Provider, boolean>>) => ({
  claude: useAgentSettings(machine, 'claude', needs.claude),
  codex: useAgentSettings(machine, 'codex', needs.codex),
});
export type AgentPages = ReturnType<typeof useAgentPages>;

const Failed = ({ text, onRetry }: { text: string; onRetry: () => void }) => (
  <Note>
    <p>{text}</p>
    <Button size="sm" icon="rotate-ccw" onClick={onRetry}>
      {t('next.settings.retry')}
    </Button>
  </Note>
);

const busyOf = (pending: Pending['pending'], prefix: string): ReadonlySet<string> =>
  new Set(
    [...pending.keys()]
      .filter((key) => key.startsWith(`${prefix}/`))
      .map((key) => key.slice(prefix.length + 1)),
  );

interface IAgentPageProps {
  provider: Provider;
  handle: IAgentSettingsHandle;
  models: readonly AgentModel[];
  pending: Pending;
}

export const AgentPage = (props: IAgentPageProps) => {
  const { state } = props.handle;
  if (state.k === 'failed') return <Failed text={state.text} onRetry={props.handle.retry} />;
  return (
    <Swap ready={state.k === 'ready'} skeleton={<Skeleton className="hx-set__sk" />}>
      {state.k === 'ready' && <AgentLoaded {...props} settings={state.value} />}
    </Swap>
  );
};

const AgentLoaded = ({
  provider,
  handle,
  models,
  pending,
  settings,
}: IAgentPageProps & { settings: AgentSettings }) => {
  const { run, pending: waiting } = pending;
  const key = (name: string) => `${provider}/${name}`;
  const onOption = (option: AgentOptionKey, value: AgentOptionValue) =>
    void run(key(option), t('next.settings.failed'), () =>
      handle.patch({ options: [{ key: option, value }] }),
    );
  const rulesBusy = waiting.has(key('rules'));
  const envBusy = waiting.has(key('env'));

  const extra =
    provider === 'claude' && settings.rules ? (
      <>
        <RulesGroup
          rules={settings.rules}
          servers={settings.mcp.map((server) => server.name)}
          busy={rulesBusy}
          onAdd={(kind: RuleKind, rule) =>
            rulesBusy
              ? Promise.resolve(false)
              : run(key('rules'), t('next.settings.failed'), () =>
                  handle.patch((base) => ({ rules: withRule(base.rules ?? EMPTY_RULES, kind, rule) })),
                )
          }
          onRemove={(kind: RuleKind, rule) =>
            rulesBusy
              ? Promise.resolve(false)
              : run(key('rules'), t('next.settings.failed'), () =>
                  handle.patch((base) => ({ rules: withoutRule(base.rules ?? EMPTY_RULES, kind, rule) })),
                )
          }
        />
        <EnvGroup
          env={settings.env}
          busy={envBusy}
          onAdd={(name, value) =>
            run(key('env'), t('next.settings.failed'), () => handle.patch({ envSet: [{ key: name, value }] }))
          }
          onRemove={(name) =>
            run(key('env'), t('next.settings.failed'), () => handle.patch({ envUnset: [name] }))
          }
        />
      </>
    ) : undefined;

  return (
    <AgentSection
      settings={settings}
      models={models}
      busy={busyOf(waiting, provider)}
      onOption={onOption}
      extra={extra}
    />
  );
};

interface IAccountsPageProps {
  pages: AgentPages;
  pending: Pending;
}

const cardOf = (provider: Provider, state: Loaded<AgentSettings>): AccountCard => {
  if (state.k === 'ready') return { provider, k: 'ready', settings: state.value };
  if (state.k === 'failed') return { provider, k: 'failed', text: state.text };
  return { provider, k: 'loading' };
};

export const AccountsPage = ({ pages, pending }: IAccountsPageProps) => {
  const [open, setOpen] = useState<Provider | null>('claude');
  const { run, pending: waiting } = pending;
  const cards = PROVIDER_LIST.map((provider) => cardOf(provider, pages[provider].state));
  const busy = new Set(PROVIDER_LIST.filter((provider) => waiting.has(`${provider}/baseUrl`)));
  return (
    <AccountsSection
      cards={cards}
      open={open}
      busy={busy}
      onToggle={(provider) => setOpen((now) => (now === provider ? null : provider))}
      onBaseUrl={(provider, url) =>
        run(`${provider}/baseUrl`, t('next.settings.failed'), () =>
          pages[provider].patch({ options: [{ key: 'baseUrl', value: url }] }),
        )
      }
      onRetry={(provider) => pages[provider].retry()}
    />
  );
};

/** The commands a CLI lists that the person's own files make: skills, not the CLI's built-ins. */
const SKILL_SOURCES: readonly string[] = ['skill', 'plugin', 'user', 'project'];

interface ISkillsPageProps {
  machine: string;
  pages: AgentPages;
  pending: Pending;
}

export const SkillsPage = ({ machine, pages, pending }: ISkillsPageProps) => {
  const [provider, setProvider] = useState<Provider>('claude');
  const [tab, setTab] = useState<ExtTab>('skills');
  const [filter, setFilter] = useState<ExtFilter>('all');
  const catalog = useCatalog(machine, provider, tab === 'skills');
  const handle = pages[provider];
  const { run, pending: waiting } = pending;
  const prefix = `${provider}/${tab}`;

  let list: ExtList = { k: 'loading' };
  if (tab === 'mcp') {
    if (handle.state.k === 'ready') {
      list = {
        k: 'ready',
        items: handle.state.value.mcp.map((server): IExtension => ({
          id: server.name,
          name: server.name,
          source: t(`next.settings.skills.mcpKind.${server.kind}`),
          description: server.target,
          on: server.enabled,
          canToggle: true,
        })),
      };
    } else if (handle.state.k === 'failed') list = { k: 'failed', text: handle.state.text };
  } else if (catalog.state.k === 'ready') {
    const rules = provider === 'claude' && handle.state.k === 'ready' ? handle.state.value.rules : null;
    list = {
      k: 'ready',
      items: catalog.state.value
        .filter((item) => SKILL_SOURCES.includes(item.source))
        .map((item): IExtension => ({
          id: item.name,
          name: item.name,
          source: item.sourceLabel,
          description: item.description,
          on: rules ? skillOn(rules, item.name) : true,
          canToggle: rules !== null,
        })),
    };
  } else if (catalog.state.k === 'failed') list = { k: 'failed', text: catalog.state.text };

  const toggle = (id: string, on: boolean) => {
    const failure = t('next.settings.failed');
    if (tab === 'mcp') {
      void run(`${prefix}/${id}`, failure, () => handle.patch({ mcp: { name: id, enabled: on } }));
      return;
    }
    const rule = skillRule(id);
    void run(`${prefix}/${id}`, failure, () =>
      handle.patch((base) => {
        const rules = base.rules ?? EMPTY_RULES;
        const deny = rules.deny.filter((item) => item !== rule);
        return { rules: { ...rules, deny: on ? deny : [...deny, rule] } };
      }),
    );
  };
  const retry = tab === 'mcp' ? handle.retry : catalog.retry;

  return (
    <SkillsSection
      provider={provider}
      tab={tab}
      filter={filter}
      list={list}
      busy={busyOf(waiting, prefix)}
      onProvider={setProvider}
      onTab={setTab}
      onFilter={setFilter}
      onToggle={toggle}
      onRetry={retry}
    />
  );
};
