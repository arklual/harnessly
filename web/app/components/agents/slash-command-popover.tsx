import { useMemo } from 'react';
import {
  BUTTON_SIZE,
  BUTTON_VARIANT,
  CHIP_TONE,
  TwButton,
  TwKbd,
  TwListbox,
  TwSwitch,
  type ITwOption,
} from '@/components/agents/ui';
import type { IUseSlashCommands } from '@/hooks/use-slash-commands';
import type { ICommandItem } from '@/lib/hl-command-catalog/types';
import { executionBadge } from '@/lib/hl-command-filter';
import { PROVIDER_TITLES } from '@/lib/hl-launch-options';
import type { TAgentProvider } from '@/types/hl-launch';
import { t } from '@/lib/i18n';

const SOURCE_TONE = {
  quick: CHIP_TONE.accent,
  builtin: CHIP_TONE.neutral,
  project: CHIP_TONE.success,
  user: CHIP_TONE.success,
  skill: CHIP_TONE.neutral,
  plugin: CHIP_TONE.neutral,
} as const satisfies Record<ICommandItem['source'], (typeof CHIP_TONE)[keyof typeof CHIP_TONE]>;

const itemMeta = (item: ICommandItem) => {
  const badge = executionBadge(item);
  return (
    <span className="tw-slash-meta">
      {badge && (
        <span className="tw-slash-exec" data-execution={item.execution}>
          {badge}
        </span>
      )}
      <span className="tw-slash-source" data-source={item.source} data-tone={SOURCE_TONE[item.source]}>
        {item.plugin ?? item.sourceLabel}
      </span>
    </span>
  );
};

interface ISlashCommandPopoverProps {
  slash: IUseSlashCommands;
  provider: TAgentProvider;
  /** Welcome composer: commands that may start a conversation. */
  welcome?: boolean;
}

/**
 * The list above a composer, inside its container (no portal): a listbox driven by the editor through
 * aria-activedescendant, so focus never leaves the text.
 */
const SlashCommandPopover = ({ slash, provider, welcome = false }: ISlashCommandPopoverProps) => {
  const { ranked, catalog, query } = slash;
  const options = useMemo<ITwOption[] | null>(() => {
    if (!ranked) return catalog.error ? [] : null;
    return ranked.items.map((item) => ({
      value: item.id,
      // A plugin command shows its own name; the plugin goes to the muted source label on the right.
      label: `${item.trigger}${item.plugin && item.name.startsWith(`${item.plugin}:`) ? item.name.slice(item.plugin.length + 1) : item.name}`,
      description: [item.argumentHint, item.description].filter(Boolean).join(' · ') || undefined,
      meta: itemMeta(item),
    }));
  }, [catalog.error, ranked]);

  if (!slash.open) return null;
  const skills = query?.kind === 'list' && query.trigger === '$';
  const total = catalog.catalog?.items.filter((item) => item.trigger === (skills ? '$' : '/')).length ?? null;
  const title = skills
    ? t('agents.slashCommandPopover.skills', { provider: PROVIDER_TITLES[provider] })
    : t('agents.slashCommandPopover.commands', { provider: PROVIDER_TITLES[provider] });
  const empty = welcome
    ? t('agents.slashCommandPopover.emptyWelcome')
    : t('agents.slashCommandPopover.empty');

  return (
    <div className="tw-slash-popover" onMouseDown={(event) => event.preventDefault()}>
      <div className="tw-slash-head">
        <span className="tw-slash-title" id={`${slash.listId}-title`}>
          {title}
          {total !== null && <span className="tw-slash-count"> · {total}</span>}
        </span>
        <span className="tw-slash-keys" aria-hidden="true">
          <TwKbd>Tab</TwKbd> {t('agents.slashCommandPopover.complete')}
        </span>
        {!skills && !welcome && (
          <TwSwitch
            className="tw-slash-all"
            checked={slash.showHidden}
            onCheckedChange={slash.setShowHidden}
            label={t('agents.slashCommandPopover.all')}
          />
        )}
      </div>
      {catalog.error && !catalog.catalog ? (
        <div className="tw-slash-error" role="alert">
          <span>{catalog.error}</span>
          <TwButton size={BUTTON_SIZE.sm} variant={BUTTON_VARIANT.secondary} onClick={catalog.retry}>
            {t('agents.slashCommandPopover.retry')}
          </TwButton>
        </div>
      ) : (
        <TwListbox
          id={slash.listId}
          options={options}
          value={null}
          aria-labelledby={`${slash.listId}-title`}
          activeValue={slash.activeValue}
          onActiveValueChange={slash.setActiveValue}
          focusable={false}
          emptyText={empty}
          loadingText={t('agents.slashCommandPopover.loading')}
          className="tw-slash-list"
          onChange={(value) => {
            const item = slash.itemByValue(value);
            if (item) slash.pick(item, 'pointer');
          }}
        />
      )}
      {ranked && (ranked.more > 0 || (ranked.hiddenMatches > 0 && !slash.showHidden)) && (
        <p className="tw-slash-foot">
          {ranked.more > 0 && t('agents.slashCommandPopover.more', { count: ranked.more })}
          {ranked.more > 0 && ranked.hiddenMatches > 0 && !slash.showHidden && ' · '}
          {ranked.hiddenMatches > 0 &&
            !slash.showHidden &&
            t('agents.slashCommandPopover.hidden', { count: ranked.hiddenMatches })}
        </p>
      )}
      {catalog.catalog?.warnings.map((warning) => (
        <p key={warning} className="tw-slash-foot tw-slash-warning">
          {warning}
        </p>
      ))}
    </div>
  );
};

export default SlashCommandPopover;
