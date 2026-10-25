// «Skills and MCP»: what the agent can reach on the machine, one list for the skills the CLI finds and one for the MCP
// servers it has set up, each with a switch where the CLI's file can turn it off. There are no counts of use or of
// tokens here: nothing on the machine measures them per skill.
import { Spin } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { Provider } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { Button } from '@/next/ui/button';
import { Skeleton } from '@/next/ui/skeleton';
import Swap from '@/next/ui/swap';
import Switch from '@/next/ui/switch';

export const EXT_TABS = ['skills', 'mcp'] as const;
export type ExtTab = (typeof EXT_TABS)[number];
export const EXT_FILTERS = ['all', 'on', 'off'] as const;
export type ExtFilter = (typeof EXT_FILTERS)[number];
export const EXT_PROVIDERS = ['claude', 'codex'] as const;

/** One skill or server of the list. */
export interface IExtension {
  /** The name the CLI's file knows it by; the key of a toggle. */
  id: string;
  name: string;
  /** Where it comes from: a plugin, the person's own, the project. */
  source: string | null;
  description: string;
  on: boolean;
  /** The file can turn it off; false: the row only lists it. */
  canToggle: boolean;
}

export type ExtList =
  { k: 'loading' } | { k: 'failed'; text: string } | { k: 'ready'; items: readonly IExtension[] };

export interface ISkillsSectionProps {
  provider: Provider;
  tab: ExtTab;
  filter: ExtFilter;
  list: ExtList;
  /** The ids of the rows waiting for the machine. */
  busy: ReadonlySet<string>;
  onProvider: (provider: Provider) => void;
  onTab: (tab: ExtTab) => void;
  onFilter: (filter: ExtFilter) => void;
  onToggle: (id: string, on: boolean) => void;
  onRetry: () => void;
}

const visible = (items: readonly IExtension[], filter: ExtFilter) =>
  items.filter((item) => filter === 'all' || (filter === 'on') === item.on);

const SkillsSection = ({
  provider,
  tab,
  filter,
  list,
  busy,
  onProvider,
  onTab,
  onFilter,
  onToggle,
  onRetry,
}: ISkillsSectionProps) => {
  const items = list.k === 'ready' ? visible(list.items, filter) : [];
  const empty = list.k === 'ready' && items.length === 0;
  const emptyKey = list.k === 'ready' && list.items.length > 0 ? 'filtered' : `${tab}.${provider}`;
  return (
    <>
      <div className="hx-set__tools">
        <div className="hx-set__seg" role="radiogroup" aria-label={t('next.settings.skills.agent')}>
          {EXT_PROVIDERS.map((item) => (
            <button
              key={item}
              type="button"
              className="hx-set__seg-item"
              role="radio"
              aria-checked={item === provider}
              onClick={() => onProvider(item)}
            >
              {t(`next.settings.accounts.name.${item}`)}
            </button>
          ))}
        </div>
        <div className="hx-set__seg" role="tablist">
          {EXT_TABS.map((item) => (
            <button
              key={item}
              type="button"
              className="hx-set__seg-item"
              role="tab"
              aria-selected={item === tab}
              aria-checked={item === tab}
              onClick={() => onTab(item)}
            >
              {t(`next.settings.skills.tab.${item}`)}
            </button>
          ))}
        </div>
        <div className="hx-set__seg" role="radiogroup" aria-label={t('next.settings.skills.show')}>
          {EXT_FILTERS.map((item) => (
            <button
              key={item}
              type="button"
              className="hx-set__seg-item"
              role="radio"
              aria-checked={item === filter}
              onClick={() => onFilter(item)}
            >
              {t(`next.settings.skills.filter.${item}`)}
            </button>
          ))}
        </div>
      </div>
      <div className="hx-set__card">
        {list.k === 'failed' && (
          <div className="hx-set__empty">
            <p>{list.text}</p>
            <Button size="sm" icon="rotate-ccw" onClick={onRetry}>
              {t('next.settings.retry')}
            </Button>
          </div>
        )}
        {list.k !== 'failed' && (
          <Swap ready={list.k === 'ready'} skeleton={<Skeleton className="hx-set__sk" />}>
            {items.map((item) => (
              <div
                className="hx-ext"
                key={item.id}
                data-on={item.on}
                data-busy={busy.has(item.id) || undefined}
              >
                <span className="hx-ext__ic">
                  <Icon name={tab === 'mcp' ? 'plug' : 'sparkles'} size="sm" />
                </span>
                <div className="hx-ext__text">
                  <div className="hx-ext__name">
                    {item.name}
                    {item.source && <span className="hx-ext__src">{item.source}</span>}
                  </div>
                  <div className="hx-ext__desc">{item.description}</div>
                </div>
                <Spin />
                {item.canToggle && (
                  <Switch label={item.name} checked={item.on} onChange={(on) => onToggle(item.id, on)} />
                )}
              </div>
            ))}
            {empty && <div className="hx-set__empty">{t(`next.settings.skills.empty.${emptyKey}`)}</div>}
          </Swap>
        )}
      </div>
      <div className="hx-set__foot">{t(`next.settings.skills.foot.${tab}.${provider}`)}</div>
    </>
  );
};

export default SkillsSection;
