// «Уведомления»: which events of a chat the person is not looking at come as a notice at the top of the window, whether
// it chimes, and whether a tab in the background shows a notification of the system. This browser's, as the theme is;
// the browser's permission is asked from the switch, never on load.
import { Group, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import { NOTICE_KINDS, type INotifySettings, type TNoticeKind } from '@/next/stores/notices';
import { SYSTEM_PERMISSION, type TSystemPermission } from '@/next/features/notifications/ping';

export interface INoticesSectionProps {
  settings: INotifySettings;
  /** What the browser says to notifications of the system. */
  permission: TSystemPermission;
  onKind: (kind: TNoticeKind, on: boolean) => void;
  onSound: (on: boolean) => void;
  onSystem: (on: boolean) => void;
}

const SYSTEM_HINT: Record<TSystemPermission, string> = {
  [SYSTEM_PERMISSION.granted]: 'next.notices.settings.system.hint',
  [SYSTEM_PERMISSION.ask]: 'next.notices.settings.system.hint',
  [SYSTEM_PERMISSION.denied]: 'next.notices.settings.system.denied',
  [SYSTEM_PERMISSION.none]: 'next.notices.settings.system.none',
};

const NoticesSection = ({ settings, permission, onKind, onSound, onSystem }: INoticesSectionProps) => (
  <>
    <Group title={t('next.notices.settings.events')} foot={t('next.notices.settings.events.foot')}>
      {NOTICE_KINDS.map((kind) => (
        <SwitchRow
          key={kind}
          label={t(`next.notices.settings.kind.${kind}`)}
          hint={t(`next.notices.settings.kind.${kind}.hint`)}
          on={settings.kinds[kind]}
          onChange={(on) => onKind(kind, on)}
        />
      ))}
    </Group>
    <Group title={t('next.notices.settings.how')}>
      <SwitchRow
        label={t('next.notices.settings.sound')}
        hint={t('next.notices.settings.sound.hint')}
        on={settings.sound}
        onChange={onSound}
      />
      <SwitchRow
        label={t('next.notices.settings.system')}
        hint={t(SYSTEM_HINT[permission])}
        on={settings.system && permission === SYSTEM_PERMISSION.granted}
        onChange={onSystem}
      />
    </Group>
  </>
);

export default NoticesSection;
