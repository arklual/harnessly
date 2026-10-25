// «Machines»: where the agents run. New chats open on the main one. The list is the fleet's: a machine is added where
// the fleet is kept, not on this page.
import type { IMachineRow } from '@/next/features/settings/machines';
import { Group, SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';

export interface IMachinesSectionProps {
  rows: readonly IMachineRow[] | null;
  /** The list did not come: why, and how to ask again. */
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  busy: ReadonlySet<string>;
  onMakeMain: (id: string) => void;
}

const MachinesSection = ({ rows, failed = null, busy, onMakeMain }: IMachinesSectionProps) => (
  <Group foot={t('next.settings.machines.foot')}>
    {rows === null && failed === null && <div className="hx-set__empty">{t('next.settings.loading')}</div>}
    {rows === null && failed !== null && (
      <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
    )}
    {rows?.map((row) => (
      <div className="hx-mrow" key={row.id} data-health={row.online ? 'ok' : 'off'} data-main={row.main}>
        <span className="hx-mrow__ic">
          <Icon name="server" size="sm" />
          <span className="hx-mrow__dot" />
        </span>
        <div className="hx-mrow__text">
          <div className="hx-mrow__name">
            {row.name}
            {row.host !== row.name && <span className="hx-mrow__host">{row.host}</span>}
          </div>
          <div className="hx-mrow__sub">{[row.state, row.agents].filter(Boolean).join(' · ')}</div>
        </div>
        {row.main && <span className="hx-mrow__main">{t('next.settings.machines.main')}</span>}
        {row.canMain && (
          <SetButton tone="ghost" busy={busy.has(row.id)} onClick={() => onMakeMain(row.id)}>
            {t('next.settings.machines.makeMain')}
          </SetButton>
        )}
      </div>
    ))}
  </Group>
);

export default MachinesSection;
