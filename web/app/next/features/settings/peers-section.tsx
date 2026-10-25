// «Связь между агентами» (Settings → Machines): whether this machine's agents take messages from other agents, who may
// write (the whole fleet, or only the list; each machine and person can be allowed or blocked), when a message reaches
// a working agent, and the last messages in and out with what came of them. The runtime enforces every switch here.
import { useState } from 'react';
import type { IPeerSettings, IPeersView } from '@/next/features/peers/api';
import {
  parseWho,
  peerLogRows,
  peerWhoRows,
  withRule,
  type IFleetMachine,
} from '@/next/features/settings/peers';
import { Group, Note, SegRow, SetButton, Spin, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import LoadFailed from '@/next/ui/load-failed';
import Switch from '@/next/ui/switch';
import '@/next/styles/peers.css';

/** How many of the audit's records the section shows. */
const LOG_SHOWN = 20;

const SCOPE_CHOICES = [
  { value: 'team', label: 'next.settings.peers.scopeTeam' },
  { value: 'list', label: 'next.settings.peers.scopeList' },
] as const;

const DELIVERY_CHOICES = [
  { value: 'step', label: 'next.settings.peers.deliveryStep' },
  { value: 'turn', label: 'next.settings.peers.deliveryTurn' },
] as const;

const LOG_ICON = { in: 'messages-square', out: 'arrow-up-right' } as const;

export interface IPeersSectionProps {
  view: IPeersView | null;
  failed?: { reason: string; onRetry: () => void | Promise<unknown> } | null;
  fleet: readonly IFleetMachine[];
  now: number;
  /** The keys of the changes on their way (accept, scope, delivery, `<kind>:<id>`, add). */
  busy: ReadonlySet<string>;
  /** Keeps `next` (made against the shown revision); `key` names the control that waits. */
  onChange: (key: string, next: IPeerSettings) => void;
}

const PeersSection = ({ view, failed = null, fleet, now, busy, onChange }: IPeersSectionProps) => {
  const [who, setWho] = useState('');
  if (view === null) {
    return (
      <Group title={t('next.settings.peers.title')}>
        {failed === null && <div className="hx-set__empty">{t('next.settings.loading')}</div>}
        {failed !== null && (
          <LoadFailed title={t('next.load.failed')} reason={failed.reason} onRetry={failed.onRetry} />
        )}
      </Group>
    );
  }
  const { settings } = view;
  const off = !settings.accept;
  const rows = peerWhoRows(view, fleet);
  const log = peerLogRows(view.audit.slice(0, LOG_SHOWN), now);
  const added = parseWho(who);
  return (
    <>
      <Group
        title={t('next.settings.peers.title')}
        foot={view.relay ? t('next.settings.peers.foot') : t('next.settings.peers.noRelay')}
      >
        <SwitchRow
          label={t('next.settings.peers.accept')}
          hint={t('next.settings.peers.acceptHint')}
          on={settings.accept}
          busy={busy.has('accept')}
          onChange={(accept) => onChange('accept', { ...settings, accept })}
        />
        <SegRow
          label={t('next.settings.peers.scope')}
          hint={t(`next.settings.peers.scopeHint.${settings.scope}`)}
          value={settings.scope}
          choices={SCOPE_CHOICES.map((c) => ({ value: c.value, label: t(c.label) }))}
          busy={busy.has('scope')}
          off={off}
          onPick={(scope) => onChange('scope', { ...settings, scope })}
        />
        <SegRow
          label={t('next.settings.peers.delivery')}
          hint={t(`next.settings.peers.deliveryHint.${settings.delivery}`)}
          value={settings.delivery}
          choices={DELIVERY_CHOICES.map((c) => ({ value: c.value, label: t(c.label) }))}
          busy={busy.has('delivery')}
          off={off}
          onPick={(delivery) => onChange('delivery', { ...settings, delivery })}
        />
      </Group>

      <Group title={t('next.settings.peers.whoTitle')} foot={t('next.settings.peers.whoFoot')}>
        {rows.map((row) => (
          <div
            className="hx-peers-set__who"
            key={row.key}
            data-busy={busy.has(row.key) || undefined}
            aria-busy={busy.has(row.key) || undefined}
          >
            <Icon name={row.kind === 'machine' ? 'server' : 'user'} size="sm" className="hx-peers-set__ic" />
            <div className="hx-peers-set__text">
              <span className="hx-peers-set__name">{row.name}</span>
              <span className="hx-peers-set__sub">{row.sub}</span>
            </div>
            <Spin />
            <Switch
              label={t('next.settings.peers.allowWho', { who: row.name })}
              checked={row.allowed}
              disabled={off}
              onChange={(allow) => onChange(row.key, withRule(settings, row, allow))}
            />
          </div>
        ))}
        <div className="hx-peers-set__add">
          <input
            className="hx-peers-set__input"
            value={who}
            placeholder={t('next.settings.peers.addPlaceholder')}
            aria-label={t('next.settings.peers.addPlaceholder')}
            disabled={off}
            onChange={(event) => setWho(event.target.value)}
          />
          <SetButton
            tone="ghost"
            icon="plus"
            busy={busy.has('add')}
            disabled={off || added === null}
            onClick={() => {
              if (!added) return;
              onChange('add', withRule(settings, added, true));
              setWho('');
            }}
          >
            {t('next.settings.peers.add')}
          </SetButton>
        </div>
      </Group>

      <Group title={t('next.settings.peers.logTitle')}>
        {log.length === 0 && <Note>{t('next.settings.peers.logEmpty')}</Note>}
        {log.map((row) => (
          <div className="hx-peers-log" key={row.id} data-state={row.refused ? 'refused' : 'delivered'}>
            <Icon name={row.refused ? 'ban' : LOG_ICON[row.dir]} size="sm" className="hx-peers-log__ic" />
            <div className="hx-peers-log__main">
              <div className="hx-peers-log__line">
                <span className="hx-peers-log__route">{row.route}</span>
                <span className="hx-peers-log__when">{row.when}</span>
              </div>
              <div className="hx-peers-log__text">{row.text}</div>
              {row.why && <div className="hx-peers-log__why">{row.why}</div>}
            </div>
          </div>
        ))}
      </Group>
    </>
  );
};

export default PeersSection;
