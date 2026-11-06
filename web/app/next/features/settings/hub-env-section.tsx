// «From the organization»: the skills and MCP servers the hub handed this machine, each with the layer that
// gave it, whether it is pinned, how the machine agent applied it, and the error when it did not. A pinned item has no
// switch; the switch of the others is the person's own and works only where the runtime takes it.
import type { HubEnvStatus, IHubEnvItem } from '@/next/features/hub-env/api';
import { Spin } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import Switch from '@/next/ui/switch';

export interface IHubEnvSectionProps {
  items: readonly IHubEnvItem[];
  canSwitch: boolean;
  /** The ids of the rows whose switch waits for the answer. */
  busy: ReadonlySet<string>;
  /** The ids the person switched off for themselves. */
  off: ReadonlySet<string>;
  onSwitch: (id: string, off: boolean) => void;
}

const STATUS_ICON: Record<HubEnvStatus, string> = { ok: 'check', error: 'circle-alert', pending: 'clock' };

const HubEnvSection = ({ items, canSwitch, busy, off, onSwitch }: IHubEnvSectionProps) => {
  if (items.length === 0) return null;
  return (
    <>
      <div className="hx-set__group">
        <div className="hx-set__group-title">{t('next.settings.hubEnv.title')}</div>
        <div className="hx-set__card" data-testid="hub-env">
          {items.map((item) => (
            <div
              className="hx-ext"
              key={item.itemId}
              data-on={!off.has(item.itemId)}
              data-busy={busy.has(item.itemId) || undefined}
            >
              <span className="hx-ext__ic">
                <Icon name={item.kind === 'mcp' ? 'plug' : 'sparkles'} size="sm" />
              </span>
              <div className="hx-ext__text">
                <div className="hx-ext__name">
                  {item.name}
                  <span className="hx-ext__src">{t(`next.settings.hubEnv.layer.${item.layer}`)}</span>
                  {item.pinned && <span className="hx-ext__src">{t('next.settings.hubEnv.pinned')}</span>}
                </div>
                <div className="hx-ext__desc hx-hubenv__status" data-status={item.status}>
                  <Icon name={STATUS_ICON[item.status]} size="sm" />
                  <span>
                    {t(`next.settings.hubEnv.status.${item.status}`)}
                    {item.status === 'error' && item.message ? `: ${item.message}` : ''}
                  </span>
                </div>
              </div>
              <Spin />
              {!item.pinned && canSwitch && (
                <Switch
                  label={item.name}
                  checked={!off.has(item.itemId)}
                  onChange={(on) => onSwitch(item.itemId, !on)}
                />
              )}
            </div>
          ))}
        </div>
        <div className="hx-set__foot">
          {t(canSwitch ? 'next.settings.hubEnv.foot' : 'next.settings.hubEnv.footFixed')}
        </div>
      </div>
    </>
  );
};

export default HubEnvSection;
