// The page of one agent CLI (Claude Code, Codex): the options of its settings file as the design groups them. Each row
// shows what the file says (or what the CLI does while it says nothing) and changes it on the machine, then waits.
import type { ReactNode } from 'react';
import { choicesOf, groupsOf, ROW_KIND, shownValue, type IOptionRow } from '@/next/features/settings/options';
import { Group, MenuRow, SegRow, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { AgentModel, AgentOptionKey, AgentOptionValue, AgentSettings } from '@/next/protocol/v2';

export interface IAgentSectionProps {
  settings: AgentSettings;
  models: readonly AgentModel[];
  /** The keys of the rows waiting for the machine. */
  busy: ReadonlySet<string>;
  onOption: (key: AgentOptionKey, value: AgentOptionValue) => void;
  /** The rules and the environment, for Claude Code. */
  extra?: ReactNode;
}

interface IOptionRowViewProps {
  row: IOptionRow;
  settings: AgentSettings;
  models: readonly AgentModel[];
  busy: boolean;
  onOption: IAgentSectionProps['onOption'];
}

const OptionRowView = ({ row, settings, models, busy, onOption }: IOptionRowViewProps) => {
  const label = t(`next.settings.opt.${settings.provider}.${row.key}`);
  const hintKey = `next.settings.opt.${settings.provider}.${row.key}.hint`;
  const hint = t(hintKey) === hintKey ? null : t(hintKey);
  const value = shownValue(row, settings);
  if (row.kind === ROW_KIND.switch)
    return (
      <SwitchRow
        label={label}
        hint={hint}
        on={value === true}
        busy={busy}
        onChange={(on) => onOption(row.key, on)}
      />
    );
  const choices = choicesOf(settings.provider, row, models, value);
  if (row.kind === ROW_KIND.seg)
    return (
      <SegRow
        label={label}
        hint={hint}
        value={typeof value === 'boolean' ? null : value}
        choices={
          choices.filter((choice) => choice.value !== null) as { value: string | number; label: string }[]
        }
        busy={busy}
        onPick={(next) => onOption(row.key, next)}
      />
    );
  return (
    <MenuRow
      label={label}
      hint={hint}
      value={typeof value === 'boolean' ? null : value}
      choices={choices}
      busy={busy}
      onPick={(next) => onOption(row.key, next)}
    />
  );
};

const AgentSection = ({ settings, models, busy, onOption, extra }: IAgentSectionProps) => {
  const groups = groupsOf(settings.provider);
  return (
    <>
      {groups.map((group) => {
        const footKey = `next.settings.foot.${settings.provider}.${group.id}`;
        return (
          <Group
            key={group.id}
            title={t(`next.settings.group.${settings.provider}.${group.id}`)}
            foot={t(footKey) === footKey ? undefined : t(footKey)}
          >
            {group.rows.map((row) => (
              <OptionRowView
                key={row.key}
                row={row}
                settings={settings}
                models={models}
                busy={busy.has(row.key)}
                onOption={onOption}
              />
            ))}
          </Group>
        );
      })}
      {extra}
    </>
  );
};

export default AgentSection;
